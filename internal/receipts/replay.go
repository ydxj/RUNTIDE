package receipts

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/runtide/runtide/internal/config"
	"github.com/runtide/runtide/internal/domain"
	"github.com/runtide/runtide/internal/execution"
	"github.com/runtide/runtide/internal/files"
	"github.com/runtide/runtide/internal/redact"
	"github.com/runtide/runtide/internal/tools"
	"gopkg.in/yaml.v3"
)

// ReplayOptions configure reproduction.
type ReplayOptions struct {
	ProjectDir  string            // a local checkout that contains the recorded commit
	ProjectName string            // existing RUNTIDE project to attach the replay to (default: a new "replay-..." project)
	Env         map[string]string // values for environment variables the receipt withheld
	Approve     bool              // the user reviewed Plan.Steps and consents to running them
}

// ReplayPlan says what a replay would do and how faithful it can be.
type ReplayPlan struct {
	Class      string   `json:"class"` // locally assessed reproducibility
	Reasons    []string `json:"reasons"`
	Steps      []string `json:"steps"`
	NeedsEnv   []string `json:"needsEnv"`
	ToolIssues []string `json:"toolIssues"`
	Missing    []string `json:"missing"`
	Blockers   []string `json:"blockers"`
	Commit     string   `json:"commit,omitempty"`

	cfg *execution.Config
}

// PlanReplay inspects the receipt against this machine. It changes nothing.
func PlanReplay(r *Receipt, o ReplayOptions) *ReplayPlan {
	p := &ReplayPlan{Class: r.Reproduction.Class, Reasons: append([]string{}, r.Reproduction.Reasons...), Missing: append([]string{}, r.Reproduction.Missing...)}
	if r.Reproduction.Class == ReproNone || r.Reproduction.Class == ReproMetadataOnly {
		p.Blockers = append(p.Blockers, fmt.Sprintf("this receipt is classified %q: it has no replayable steps", r.Reproduction.Class))
		return p
	}
	if r.Execution.ConfigYAML == "" {
		p.Blockers = append(p.Blockers, "the receipt contains no workspace configuration")
		return p
	}
	cfg, err := execution.ParseConfig(r.Execution.ConfigYAML)
	if err != nil {
		p.Blockers = append(p.Blockers, err.Error())
		return p
	}
	p.cfg = cfg
	p.Steps = append(p.Steps, r.Reproduction.Replayable...)
	for _, line := range cfg.Commands() {
		p.Steps = append(p.Steps, "run "+line)
	}
	// withheld env values
	collect := func(m map[string]string) {
		for k, v := range m {
			if v == redact.Mask {
				if _, ok := o.Env[k]; !ok {
					p.NeedsEnv = append(p.NeedsEnv, k)
				}
			}
		}
	}
	collect(cfg.Env)
	for _, c := range cfg.Setup {
		collect(c.Env)
	}
	for _, c := range cfg.Start {
		collect(c.Env)
	}
	for _, s := range cfg.Services {
		collect(s.Env)
	}
	p.NeedsEnv = dedupeSorted(p.NeedsEnv)
	if len(p.NeedsEnv) > 0 {
		p.Blockers = append(p.Blockers, "secret environment variables were withheld from the receipt; provide values with --env: "+strings.Join(p.NeedsEnv, ", "))
	}
	// source
	if r.Reproduction.Git != nil && r.Reproduction.Git.Commit != "" {
		p.Commit = r.Reproduction.Git.Commit
	}
	switch {
	case o.ProjectDir == "" && o.ProjectName == "":
		p.Blockers = append(p.Blockers, "a project directory containing the recorded source is required (--project-dir)")
	case o.ProjectDir != "":
		if !files.IsGitRepo(o.ProjectDir) {
			p.Blockers = append(p.Blockers, o.ProjectDir+" is not a git repository")
		} else if p.Commit != "" {
			if out, err := exec.Command("git", "-C", o.ProjectDir, "cat-file", "-e", p.Commit+"^{commit}").CombinedOutput(); err != nil {
				p.Blockers = append(p.Blockers, fmt.Sprintf("commit %.10s is not present in %s: %s", p.Commit, o.ProjectDir, strings.TrimSpace(string(out))))
			}
		} else {
			p.Reasons = append(p.Reasons, "the receipt records no commit; the replay will use the current HEAD of the project, which may differ from the original source")
			p.Class = demote(p.Class)
		}
	}
	// tools
	have := map[string]string{}
	for _, t := range tools.Detect(context.Background()) {
		if t.Installed {
			have[t.ID] = t.Version
		}
	}
	for _, t := range r.Environment.Tools {
		v, ok := have[t.ID]
		switch {
		case !ok:
			p.ToolIssues = append(p.ToolIssues, fmt.Sprintf("%s %s is not installed here", t.Name, t.Version))
		case v != t.Version:
			p.ToolIssues = append(p.ToolIssues, fmt.Sprintf("%s: recorded %s, installed %s", t.Name, t.Version, v))
		}
	}
	if len(p.ToolIssues) > 0 {
		p.Class = demote(p.Class)
		p.Reasons = append(p.Reasons, fmt.Sprintf("%d tool(s) differ from the recorded environment", len(p.ToolIssues)))
	}
	if r.Environment.OS != "" && r.Environment.OS != "windows" {
		p.Blockers = append(p.Blockers, "the receipt was recorded on "+r.Environment.OS+"; this RUNTIDE replays Windows executions only")
	}
	return p
}

func demote(c string) string {
	if c == ReproFull {
		return ReproPartial
	}
	return c
}

func dedupeSorted(in []string) []string {
	m := map[string]bool{}
	var out []string
	for _, s := range in {
		if !m[s] {
			m[s] = true
			out = append(out, s)
		}
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// ReplayResult is the outcome of a replay.
type ReplayResult struct {
	Plan        *ReplayPlan `json:"plan"`
	ExecutionID string      `json:"executionId"`
	State       string      `json:"state"`
	ReceiptPath string      `json:"receiptPath"`
	Comparison  *Comparison `json:"comparison"`
}

// Replay re-runs the recorded steps in an isolated worktree, records the new
// execution, exports its receipt and compares it with the original. Without
// opts.Approve nothing runs.
func Replay(ctx context.Context, m *execution.Manager, r *Receipt, receiptPath string, o ReplayOptions) (*ReplayResult, error) {
	plan := PlanReplay(r, o)
	res := &ReplayResult{Plan: plan}
	if len(plan.Blockers) > 0 {
		return res, errors.New("cannot replay: " + strings.Join(plan.Blockers, "; "))
	}
	if !o.Approve {
		return res, errors.New("replaying executes the commands listed in the plan; approval is required")
	}
	cfg := plan.cfg
	apply := func(dst map[string]string) {
		for k, v := range dst {
			if nv, ok := o.Env[k]; ok && v == redact.Mask {
				dst[k] = nv
			}
		}
	}
	apply(cfg.Env)
	for i := range cfg.Setup {
		apply(cfg.Setup[i].Env)
	}
	for i := range cfg.Start {
		apply(cfg.Start[i].Env)
	}
	for i := range cfg.Services {
		apply(cfg.Services[i].Env)
	}
	cfg.Policy.AllowAdHoc = false
	iso, _ := cfg.IsolationLevel()
	if iso == domain.IsolationManaged {
		cfg.Isolation = "worktree" // never replay in the user's project directory
	}
	// project
	var proj *domain.Project
	var err error
	if o.ProjectName != "" {
		proj, err = m.St.GetProject(o.ProjectName)
		if err != nil {
			return res, fmt.Errorf("project %q: %w", o.ProjectName, err)
		}
	} else {
		dir, _ := filepath.Abs(o.ProjectDir)
		name := "replay-" + short(r.Manifest.ExecutionID)
		if proj, err = m.St.GetProject(name); err != nil {
			if proj, err = m.CreateProject(name, dir, "created for a receipt replay"); err != nil {
				return res, err
			}
		}
	}
	y, err := yamlString(cfg)
	if err != nil {
		return res, err
	}
	wsName := "replay-" + short(r.Manifest.ExecutionID) + "-" + fmt.Sprint(os.Getpid()%1000) + "-" + short(domain.NewID("x"))
	wp, err := m.PlanWorkspace(execution.WorkspaceOptions{Project: proj.Name, Name: wsName, Task: "Replay of " + r.Manifest.Task, ConfigYAML: y, Commit: plan.Commit, AcceptDirty: true})
	if err != nil {
		return res, err
	}
	ws, err := m.CreateWorkspace(wp, "Replay of "+r.Manifest.Task)
	if err != nil {
		return res, err
	}
	if err := m.ApproveWorkspace(ws.ID); err != nil {
		return res, err
	}
	x, err := m.NewExecution(ws.ID, "Replay of "+r.Manifest.ExecutionID)
	if err != nil {
		return res, err
	}
	res.ExecutionID = x.ID
	startErr := m.Start(ctx, x.ID)
	if startErr == nil {
		if cfg.Verify != nil {
			if _, _, err := m.Verify(ctx, x.ID, nil); err != nil {
				startErr = err
			}
		} else if err := m.Finish(x.ID); err != nil {
			startErr = err
		}
	}
	got, _ := m.St.GetExecution(x.ID)
	if got != nil {
		res.State = string(got.State)
	}
	dir := filepath.Join(execution.ReplayDir(m.Home))
	_ = os.MkdirAll(dir, 0o700)
	out := filepath.Join(dir, x.ID+Extension)
	if _, err := Export(m, x.ID, out, BuildOptions{}); err != nil {
		return res, fmt.Errorf("replay ran but its receipt could not be exported: %w", err)
	}
	res.ReceiptPath = out
	if nr, _, err := Load(out, VerifyOptions{}); err == nil {
		res.Comparison = Compare(r, nr, receiptPath, out)
	}
	if startErr != nil {
		return res, fmt.Errorf("replay execution did not complete cleanly: %w", startErr)
	}
	return res, nil
}

func short(id string) string {
	if i := strings.Index(id, "_"); i >= 0 {
		id = id[i+1:]
	}
	if len(id) > 8 {
		id = id[len(id)-8:]
	}
	return id
}

var _ = config.ValidName

func yamlString(c *execution.Config) (string, error) {
	b, err := yaml.Marshal(c)
	return string(b), err
}
