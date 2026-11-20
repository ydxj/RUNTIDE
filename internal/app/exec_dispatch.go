package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/runtide/runtide/internal/domain"
	"github.com/runtide/runtide/internal/execution"
	"github.com/runtide/runtide/internal/files"
	"github.com/runtide/runtide/internal/paths"
	"github.com/runtide/runtide/internal/receipts"
	"github.com/runtide/runtide/internal/store"
	"github.com/runtide/runtide/internal/verification"
)

func strictDecode(raw json.RawMessage, method string, v any) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("invalid parameters for %s: %w", method, err)
	}
	return nil
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

func slug(s string) string {
	s = strings.Trim(slugRe.ReplaceAllString(strings.ToLower(s), "-"), "-")
	if len(s) > 40 {
		s = s[:40]
	}
	if s == "" {
		s = "run"
	}
	return s
}

// KeyPath is where the receipt-signing key lives.
func (e *Engine) KeyPath() string { return filepath.Join(e.Home, "keys", "signing.json") }

func (e *Engine) verifyOpts() receipts.VerifyOptions {
	return receipts.VerifyOptions{TrustedKeys: e.Settings().TrustedKeys}
}

// receiptPath resolves a file path or a stored receipt id/hash to a file.
func (e *Engine) receiptPath(ref string) (string, error) {
	if ref == "" {
		return "", errors.New("receipt reference is required")
	}
	if st, err := os.Stat(ref); err == nil && !st.IsDir() {
		abs, _ := filepath.Abs(ref)
		return abs, nil
	}
	if r, err := e.Store.GetReceipt(ref); err == nil {
		return r.Path, nil
	}
	return "", fmt.Errorf("no receipt file or stored receipt %q", ref)
}

// dispatchExec handles the execution-platform methods. handled is false when
// the method does not belong to this group.
func (e *Engine) dispatchExec(ctx context.Context, method string, raw json.RawMessage) (res any, handled bool, err error) {
	p := func(v any) error { return strictDecode(raw, method, v) }
	if !strings.HasPrefix(method, "projects.") && !strings.HasPrefix(method, "ws.") && !strings.HasPrefix(method, "runs.") && !strings.HasPrefix(method, "receipts.") {
		return nil, false, nil
	}
	switch method {
	// ---- projects
	case "projects.list":
		ps, err := e.Store.ListProjects()
		if ps == nil {
			ps = []*domain.Project{}
		}
		return ps, true, err
	case "projects.create":
		var a struct{ Name, Root, Description string }
		if err := p(&a); err != nil {
			return nil, true, err
		}
		pr, err := e.Exec.CreateProject(a.Name, a.Root, a.Description)
		return pr, true, err

	// ---- task workspaces
	case "ws.list":
		var a struct {
			Project string `json:"project"`
		}
		if err := p(&a); err != nil {
			return nil, true, err
		}
		ws, err := e.Exec.Workspaces(a.Project)
		if ws == nil {
			ws = []*domain.Workspace{}
		}
		return ws, true, err
	case "ws.get":
		var a struct {
			Ref string `json:"ref"`
		}
		if err := p(&a); err != nil {
			return nil, true, err
		}
		w, err := e.Exec.Workspace(a.Ref)
		if err != nil {
			return nil, true, err
		}
		cfg, cerr := execution.ParseConfig(w.ConfigYAML)
		out := map[string]any{"workspace": w}
		if cerr == nil {
			out["commands"] = cfg.Commands()
		}
		return out, true, nil
	case "ws.plan", "ws.create":
		var a struct {
			Project     string `json:"project"`
			Name        string `json:"name"`
			Task        string `json:"task"`
			Isolation   string `json:"isolation"`
			ConfigYAML  string `json:"configYaml"`
			Dir         string `json:"dir"`
			AcceptDirty bool   `json:"acceptDirty"`
			Confirm     bool   `json:"confirm"`
		}
		if err := p(&a); err != nil {
			return nil, true, err
		}
		plan, err := e.Exec.PlanWorkspace(execution.WorkspaceOptions{Project: a.Project, Name: a.Name, Task: a.Task, Isolation: a.Isolation, ConfigYAML: a.ConfigYAML, Dir: a.Dir, AcceptDirty: a.AcceptDirty})
		if err != nil {
			return nil, true, err
		}
		if method == "ws.plan" {
			return plan, true, nil
		}
		if !a.Confirm {
			return nil, true, errors.New("creating a workspace requires confirmation of the plan")
		}
		w, err := e.Exec.CreateWorkspace(plan, a.Task)
		return w, true, err
	case "ws.approve", "ws.reopen", "ws.delete", "ws.removeDir":
		var a struct {
			Ref     string `json:"ref"`
			Force   bool   `json:"force"`
			Confirm bool   `json:"confirm"`
		}
		if err := p(&a); err != nil {
			return nil, true, err
		}
		w, err := e.Exec.Workspace(a.Ref)
		if err != nil {
			return nil, true, err
		}
		switch method {
		case "ws.approve":
			if !a.Confirm {
				return nil, true, errors.New("approving a workspace requires explicit confirmation")
			}
			return nil, true, e.Exec.ApproveWorkspace(w.ID)
		case "ws.reopen":
			return nil, true, e.Exec.ReopenWorkspace(w.ID)
		case "ws.delete":
			if !a.Confirm {
				return nil, true, errors.New("deleting a workspace requires confirmation")
			}
			e.Store.Audit("local", "workspace.delete", w.ID, w.Name)
			return nil, true, e.Store.DeleteWorkspace(w.ID)
		}
		if !a.Confirm {
			return nil, true, errors.New("removing a workspace directory requires confirmation")
		}
		return nil, true, e.Exec.RemoveWorkspaceDir(w.ID, a.Force)
	case "ws.configure":
		var a struct {
			Ref  string `json:"ref"`
			YAML string `json:"yaml"`
		}
		if err := p(&a); err != nil {
			return nil, true, err
		}
		w, err := e.Exec.Workspace(a.Ref)
		if err != nil {
			return nil, true, err
		}
		return nil, true, e.Exec.ReconfigureWorkspace(w.ID, a.YAML)

	// ---- executions
	case "runs.list":
		var a struct {
			Project   string   `json:"project"`
			Workspace string   `json:"workspace"`
			States    []string `json:"states"`
			Limit     int      `json:"limit"`
		}
		if err := p(&a); err != nil {
			return nil, true, err
		}
		f := store.ExecFilter{Limit: a.Limit}
		if a.Project != "" {
			pr, err := e.Store.GetProject(a.Project)
			if err != nil {
				return nil, true, err
			}
			f.ProjectID = pr.ID
		}
		if a.Workspace != "" {
			w, err := e.Exec.Workspace(a.Workspace)
			if err != nil {
				return nil, true, err
			}
			f.WorkspaceID = w.ID
		}
		for _, s := range a.States {
			f.States = append(f.States, domain.State(strings.ToUpper(s)))
		}
		xs, err := e.Store.ListExecutions(f)
		if xs == nil {
			xs = []*domain.Execution{}
		}
		return xs, true, err
	case "runs.create":
		var a struct{ Workspace, Task string }
		if err := p(&a); err != nil {
			return nil, true, err
		}
		x, err := e.Exec.NewExecution(a.Workspace, a.Task)
		return x, true, err
	case "runs.get":
		var a struct {
			ID string `json:"id"`
		}
		if err := p(&a); err != nil {
			return nil, true, err
		}
		d, err := e.Exec.Detail(a.ID)
		return d, true, err
	case "runs.start", "runs.finish", "runs.cancel", "runs.verify":
		var a struct {
			ID     string `json:"id"`
			Reason string `json:"reason"`
		}
		if err := p(&a); err != nil {
			return nil, true, err
		}
		x, err := e.Store.GetExecution(a.ID)
		if err != nil {
			return nil, true, err
		}
		switch method {
		case "runs.start":
			err = e.Exec.Start(ctx, x.ID)
			e.publishRun(x.ID)
			return nil, true, err
		case "runs.finish":
			return nil, true, e.Exec.Finish(x.ID)
		case "runs.cancel":
			if a.Reason == "" {
				a.Reason = "cancelled by user"
			}
			e.Store.Audit("local", "run.cancel", x.ID, a.Reason)
			return nil, true, e.Exec.Cancel(x.ID, a.Reason)
		}
		results, sum, err := e.Exec.Verify(ctx, x.ID, nil)
		return map[string]any{"results": results, "summary": sum}, true, err
	case "runs.verifyWith":
		var a struct {
			ID       string `json:"id"`
			Pipeline string `json:"pipeline"` // YAML
		}
		if err := p(&a); err != nil {
			return nil, true, err
		}
		pl, err := verification.Parse([]byte(a.Pipeline))
		if err != nil {
			return nil, true, err
		}
		results, sum, err := e.Exec.Verify(ctx, a.ID, pl)
		return map[string]any{"results": results, "summary": sum}, true, err
	case "runs.exec":
		var a struct {
			ID        string   `json:"id"`
			Name      string   `json:"name"`
			Command   string   `json:"command"`
			Args      []string `json:"args"`
			Approved  bool     `json:"approved"`
			TimeoutMs int64    `json:"timeoutMs"`
		}
		if err := p(&a); err != nil {
			return nil, true, err
		}
		x, err := e.Store.GetExecution(a.ID)
		if err != nil {
			return nil, true, err
		}
		e.Store.Audit("local", "run.exec", x.ID, a.Command)
		c, err := e.Exec.RunAdHoc(ctx, x.ID, a.Name, a.Command, a.Args, a.Approved, time.Duration(a.TimeoutMs)*time.Millisecond)
		return c, true, err
	case "runs.events":
		var a struct {
			ID       string   `json:"id"`
			Types    []string `json:"types"`
			Subject  string   `json:"subject"`
			Severity string   `json:"severity"`
			Search   string   `json:"search"`
			AfterSeq int64    `json:"afterSeq"`
			Limit    int      `json:"limit"`
		}
		if err := p(&a); err != nil {
			return nil, true, err
		}
		x, err := e.Store.GetExecution(a.ID)
		if err != nil {
			return nil, true, err
		}
		evs, err := e.Store.ListEvents(x.ID, store.EventFilter{Types: a.Types, Subject: a.Subject, Severity: a.Severity, Search: a.Search, AfterSeq: a.AfterSeq, Limit: a.Limit})
		return evs, true, err
	case "runs.output":
		var a struct {
			CommandID string `json:"commandId"`
			Offset    int    `json:"offset"`
			Limit     int    `json:"limit"`
		}
		if err := p(&a); err != nil {
			return nil, true, err
		}
		ls, err := e.Store.ListOutput(a.CommandID, a.Offset, a.Limit)
		return ls, true, err
	case "runs.diff":
		var a struct{ ID, Path string }
		if err := p(&a); err != nil {
			return nil, true, err
		}
		return e.fileDiff(a.ID, a.Path)
	case "runs.orphans":
		o, err := e.Exec.Orphans()
		if o == nil {
			o = []execution.Orphan{}
		}
		return o, true, err
	case "runs.cleanup":
		var a struct {
			ID      string `json:"id"`
			Confirm bool   `json:"confirm"`
		}
		if err := p(&a); err != nil {
			return nil, true, err
		}
		if !a.Confirm {
			return nil, true, errors.New("terminating orphaned processes requires confirmation")
		}
		k, s, err := e.Exec.CleanupOrphans(a.ID)
		return map[string]int{"terminated": k, "skipped": s}, true, err

	// ---- receipts
	case "receipts.list":
		rs, err := e.Store.ListReceipts()
		return rs, true, err
	case "receipts.export":
		var a struct {
			ID        string   `json:"id"`
			Path      string   `json:"path"`
			Diffs     bool     `json:"diffs"`
			Artifacts []string `json:"artifacts"`
			Sign      bool     `json:"sign"`
		}
		if err := p(&a); err != nil {
			return nil, true, err
		}
		x, err := e.Store.GetExecution(a.ID)
		if err != nil {
			return nil, true, err
		}
		out := a.Path
		if out == "" {
			dir := filepath.Join(e.Home, "receipts", "exports")
			if err := os.MkdirAll(dir, 0o700); err != nil {
				return nil, true, err
			}
			out = filepath.Join(dir, slug(x.Task)+"-"+x.ID[3:11]+receipts.Extension)
		} else if !filepath.IsAbs(out) {
			return nil, true, errors.New("export path must be absolute")
		}
		opts := receipts.BuildOptions{IncludeDiffs: a.Diffs, Artifacts: a.Artifacts}
		if a.Sign {
			kp, err := receipts.LoadKey(e.KeyPath())
			if err != nil {
				return nil, true, fmt.Errorf("no signing key (create one with `runtide receipt keygen`): %w", err)
			}
			opts.Sign = kp
		}
		mf, err := receipts.Export(e.Exec, x.ID, out, opts)
		if err != nil {
			return nil, true, err
		}
		return map[string]any{"path": out, "manifest": mf}, true, nil
	case "receipts.verify", "receipts.inspect", "receipts.report", "receipts.import":
		var a struct {
			Ref string `json:"ref"`
		}
		if err := p(&a); err != nil {
			return nil, true, err
		}
		path, err := e.receiptPath(a.Ref)
		if err != nil {
			return nil, true, err
		}
		switch method {
		case "receipts.verify":
			v, err := receipts.Verify(path, e.verifyOpts())
			return v, true, err
		case "receipts.import":
			rec, v, err := receipts.Import(e.Store, e.Home, path, e.verifyOpts())
			if err != nil {
				return map[string]any{"verification": v}, true, err
			}
			return map[string]any{"receipt": rec, "verification": v}, true, nil
		}
		r, v, err := receipts.Load(path, e.verifyOpts())
		if err != nil {
			return map[string]any{"verification": v}, true, err
		}
		if method == "receipts.report" {
			h, err := receipts.HTML(r, v)
			return h, true, err
		}
		return map[string]any{"receipt": r, "verification": v, "path": path}, true, nil
	case "receipts.log":
		var a struct{ Ref, File string }
		if err := p(&a); err != nil {
			return nil, true, err
		}
		path, err := e.receiptPath(a.Ref)
		if err != nil {
			return nil, true, err
		}
		s, err := receipts.ReadLog(path, a.File)
		return s, true, err
	case "receipts.compare":
		var a struct{ A, B string }
		if err := p(&a); err != nil {
			return nil, true, err
		}
		return e.compareRefs(a.A, a.B)
	case "receipts.plan", "receipts.replay":
		var a struct {
			Ref         string            `json:"ref"`
			ProjectDir  string            `json:"projectDir"`
			ProjectName string            `json:"projectName"`
			Env         map[string]string `json:"env"`
			Approve     bool              `json:"approve"`
		}
		if err := p(&a); err != nil {
			return nil, true, err
		}
		path, err := e.receiptPath(a.Ref)
		if err != nil {
			return nil, true, err
		}
		r, _, err := receipts.Load(path, e.verifyOpts())
		if err != nil {
			return nil, true, err
		}
		o := receipts.ReplayOptions{ProjectDir: a.ProjectDir, ProjectName: a.ProjectName, Env: a.Env, Approve: a.Approve}
		if method == "receipts.plan" {
			return receipts.PlanReplay(r, o), true, nil
		}
		e.Store.Audit("local", "receipt.replay", r.Manifest.ExecutionID, "")
		res, err := receipts.Replay(ctx, e.Exec, r, path, o)
		return res, true, err
	case "receipts.keygen":
		kp, err := receipts.GenerateKey(e.KeyPath())
		if err != nil {
			return nil, true, err
		}
		return map[string]string{"fingerprint": kp.Fingerprint(), "path": e.KeyPath()}, true, nil
	case "receipts.keyinfo":
		kp, err := receipts.LoadKey(e.KeyPath())
		if err != nil {
			return map[string]any{"exists": false}, true, nil
		}
		return map[string]any{"exists": true, "fingerprint": kp.Fingerprint(), "path": e.KeyPath()}, true, nil
	}
	return nil, true, fmt.Errorf("unknown method %q", method)
}

func (e *Engine) publishRun(id string) {
	if d, err := e.Exec.Detail(id); err == nil {
		e.Publish("run", d.Execution)
	}
}

func (e *Engine) fileDiff(execID, rel string) (any, bool, error) {
	x, err := e.Store.GetExecution(execID)
	if err != nil {
		return nil, true, err
	}
	if files.SensitivePath(rel) {
		return nil, true, errors.New("diffs of sensitive files are never produced")
	}
	ws, err := e.Store.GetWorkspaceRow(x.WorkspaceID)
	if err != nil {
		return nil, true, err
	}
	rel = filepath.ToSlash(filepath.Clean(rel))
	if strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return nil, true, errors.New("path must be relative to the workspace")
	}
	before, err := e.Store.GetBaseline(x.ID, rel)
	if err != nil {
		return nil, true, fmt.Errorf("no baseline for %s (new, binary, large or excluded files have none)", rel)
	}
	after, err := os.ReadFile(filepath.Join(ws.Dir, filepath.FromSlash(rel)))
	if err != nil {
		after = nil
	}
	if !files.IsText(after) && len(after) > 0 {
		return nil, true, errors.New("current file is binary")
	}
	return map[string]string{"path": rel, "diff": files.UnifiedDiff("a/"+rel, "b/"+rel, string(before), string(after))}, true, nil
}

// compareRefs compares two receipts or executions (ids are exported to temporary receipts).
func (e *Engine) compareRefs(a, b string) (any, bool, error) {
	resolve := func(ref string) (string, func(), error) {
		if path, err := e.receiptPath(ref); err == nil {
			return path, func() {}, nil
		}
		x, err := e.Store.GetExecution(ref)
		if err != nil {
			return "", nil, fmt.Errorf("%q is neither a receipt nor an execution", ref)
		}
		dir, err := os.MkdirTemp(paths.BackupDir(e.Home), "cmp-")
		if err != nil {
			return "", nil, err
		}
		out := filepath.Join(dir, x.ID+receipts.Extension)
		if _, err := receipts.Export(e.Exec, x.ID, out, receipts.BuildOptions{}); err != nil {
			_ = os.RemoveAll(dir)
			return "", nil, err
		}
		return out, func() { _ = os.RemoveAll(dir) }, nil
	}
	pa, ca, err := resolve(a)
	if err != nil {
		return nil, true, err
	}
	defer ca()
	pb, cb, err := resolve(b)
	if err != nil {
		return nil, true, err
	}
	defer cb()
	ra, _, err := receipts.Load(pa, e.verifyOpts())
	if err != nil {
		return nil, true, err
	}
	rb, _, err := receipts.Load(pb, e.verifyOpts())
	if err != nil {
		return nil, true, err
	}
	return receipts.Compare(ra, rb, pa, pb), true, nil
}
