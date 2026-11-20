package receipts

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/runtide/runtide/internal/domain"
	"github.com/runtide/runtide/internal/execution"
	"github.com/runtide/runtide/internal/files"
	"github.com/runtide/runtide/internal/redact"
	"github.com/runtide/runtide/internal/store"
	"gopkg.in/yaml.v3"
)

// BuildOptions control what an exported receipt contains.
type BuildOptions struct {
	IncludeDiffs bool     // add text diffs of modified files (may contain code or secrets: off by default)
	Artifacts    []string // extra files (relative to the workspace) to embed; sensitive files are refused
	Sign         *KeyPair // optional ed25519 signing key
	Now          time.Time
}

const maxArtifactBytes = 8 << 20

// Export builds the receipt for an execution and writes it to path (which must not exist).
func Export(m *execution.Manager, execID, path string, opts BuildOptions) (*Manifest, error) {
	if _, err := os.Stat(path); err == nil {
		return nil, fmt.Errorf("%s already exists; choose another name", path)
	}
	data, mf, err := Build(m, execID, opts)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return nil, err
	}
	sum := sha256.Sum256(data)
	mj, _ := json.Marshal(mf)
	rec := &store.ReceiptRecord{ExecutionID: execID, ProjectName: mf.ProjectName, Path: path, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(data)), Status: mf.Status, ManifestJSON: string(mj)}
	if opts.Sign != nil {
		rec.SignedBy = opts.Sign.Fingerprint()
	}
	if err := m.St.AddReceipt(rec); err != nil {
		return nil, err
	}
	m.St.Audit("local", "receipt.export", execID, rec.SHA256[:12])
	return mf, nil
}

// Build assembles the archive in memory.
func Build(m *execution.Manager, execID string, opts BuildOptions) ([]byte, *Manifest, error) {
	d, err := m.Detail(execID)
	if err != nil {
		return nil, nil, err
	}
	if !d.Execution.State.Terminal() {
		return nil, nil, fmt.Errorf("execution is %s; export a receipt after it has ended (or cancel it first)", d.Execution.State)
	}
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	st := m.St
	red := redact.New()
	wsDir := ""
	if d.Workspace != nil {
		wsDir = d.Workspace.Dir
	}
	scrub := pathScrubber(wsDir, d.Workspace)

	files := map[string][]byte{}
	put := func(name string, v any) error {
		b, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		files[name] = append(b, '\n')
		return nil
	}

	// commands (cwd relative, args already redacted at record time)
	cmds := make([]domain.Command, len(d.Commands))
	masked := 0
	logFiles := map[string]string{}
	for i, c := range d.Commands {
		c.Cwd = scrub(c.Cwd)
		cmds[i] = c
		if c.Redacted {
			masked++
		}
		lines, err := st.ListOutput(c.ID, 0, 100000)
		if err != nil {
			return nil, nil, err
		}
		var b strings.Builder
		for _, l := range lines {
			fmt.Fprintf(&b, "[%s] %s\n", l.Stream, scrub(l.Line))
		}
		name := fmt.Sprintf("%s%03d-%s.log", dirLogs, c.Seq, sanitizeName(c.Name))
		files[name] = []byte(b.String())
		logFiles[c.ID] = name
	}
	evs, err := st.ListEvents(execID, store.EventFilter{Limit: 100000})
	if err != nil {
		return nil, nil, err
	}
	for i := range evs {
		evs[i].Data = scrubMap(evs[i].Data, scrub)
	}
	for i := range d.Verification {
		d.Verification[i].Output = red.Line(scrub(d.Verification[i].Output))
		d.Verification[i].Error = scrub(d.Verification[i].Error)
	}
	changes := d.FileChanges

	// workspace config with sensitive env values masked
	cfgYAML, cfg := maskedConfig(d.Workspace)
	execDoc := ExecutionDoc{Execution: d.Execution, ConfigYAML: cfgYAML, DurationMs: d.DurationMs, Isolation: d.Execution.Isolation.Name()}
	execDoc.Execution.Environment = nil
	if d.Project != nil {
		execDoc.Project = d.Project.Name
	}
	if d.Workspace != nil {
		execDoc.Workspace = d.Workspace.Name
	}
	var env execution.Environment
	_ = json.Unmarshal(d.Environment, &env)

	repro := classify(d, cfg, &env, masked)

	if err := firstErr(
		put(fExecution, execDoc), put(fEnvironment, env), put(fCommands, cmds), put(fEvents, evs),
		put(fProcesses, d.Processes), put(fServices, d.Services), put(fFileChanges, changes),
		put(fVerification, VerificationDoc{Summary: d.Summary, Results: d.Verification}), put(fReproduction, repro),
	); err != nil {
		return nil, nil, err
	}
	var warnings []string
	if opts.IncludeDiffs {
		diffs := map[string]string{}
		for _, c := range changes {
			if c.Change != "modified" || files_isSensitive(c.Path) {
				continue
			}
			before, err := st.GetBaseline(execID, c.Path)
			if err != nil || wsDir == "" {
				continue
			}
			after, err := os.ReadFile(filepath.Join(wsDir, filepath.FromSlash(c.Path)))
			if err != nil || !isText(after) || len(after) > 256<<10 {
				continue
			}
			diffs[c.Path] = red.Line(unified(c.Path, string(before), string(after)))
		}
		if err := put(fDiffs, diffs); err != nil {
			return nil, nil, err
		}
		warnings = append(warnings, "This receipt contains text diffs of modified files. Review them before sharing: they may contain proprietary code or secrets that pattern-based redaction missed.")
	}
	for _, a := range opts.Artifacts {
		rel := filepath.ToSlash(filepath.Clean(a))
		if wsDir == "" || strings.HasPrefix(rel, "..") || filepath.IsAbs(a) {
			return nil, nil, fmt.Errorf("artifact %q must be a relative path inside the workspace", a)
		}
		if files_isSensitive(rel) {
			return nil, nil, fmt.Errorf("artifact %q looks like it contains secrets and is refused", a)
		}
		b, err := os.ReadFile(filepath.Join(wsDir, filepath.FromSlash(rel)))
		if err != nil {
			return nil, nil, err
		}
		if len(b) > maxArtifactBytes {
			return nil, nil, fmt.Errorf("artifact %q is larger than %d MiB", a, maxArtifactBytes>>20)
		}
		files[dirArtifacts+rel] = b
	}
	sens := 0
	for _, e := range evs {
		if e.Type == domain.EvFilesChanged {
			if n, ok := e.Data["skippedSensitive"].(float64); ok {
				sens = int(n)
			}
		}
	}
	mf := &Manifest{
		Format: FormatName, FormatVersion: FormatVersion, RuntideVersion: m.Version, ExecutionID: d.Execution.ID, ProjectID: d.Execution.ProjectID,
		ProjectName: execDoc.Project, Task: d.Execution.Task, CreatedAt: opts.Now.UTC(), Status: string(d.Execution.State), Verification: d.Execution.Verification,
		Isolation: execDoc.Isolation, Reproducibility: repro.Class, Warnings: warnings,
		Redaction: Redaction{CommandsWithMaskedArgs: masked, SensitiveFilesExcluded: sens, DiffsIncluded: opts.IncludeDiffs, AbsolutePathsRemoved: true},
	}
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		sum := sha256.Sum256(files[n])
		mf.Files = append(mf.Files, FileEntry{Path: n, Size: int64(len(files[n])), SHA256: hex.EncodeToString(sum[:])})
	}
	mfJSON, err := json.MarshalIndent(mf, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	mfJSON = append(mfJSON, '\n')

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	write := func(name string, b []byte) error {
		h := &zip.FileHeader{Name: name, Method: zip.Deflate, Modified: opts.Now.UTC()}
		w, err := zw.CreateHeader(h)
		if err != nil {
			return err
		}
		_, err = w.Write(b)
		return err
	}
	if err := write(fManifest, mfJSON); err != nil {
		return nil, nil, err
	}
	for _, n := range names {
		if err := write(n, files[n]); err != nil {
			return nil, nil, err
		}
	}
	if opts.Sign != nil {
		sig, err := opts.Sign.SignManifest(mfJSON, opts.Now)
		if err != nil {
			return nil, nil, err
		}
		if err := write(fSignature, sig); err != nil {
			return nil, nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, nil, err
	}
	return buf.Bytes(), mf, nil
}

func firstErr(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}

func sanitizeName(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "command"
	}
	return b.String()
}

// pathScrubber replaces the local workspace directory (and the project root,
// which contains the user name) with stable placeholders.
func pathScrubber(wsDir string, ws *domain.Workspace) func(string) string {
	pairs := []string{}
	add := func(p, repl string) {
		if p == "" {
			return
		}
		pairs = append(pairs, p, repl, strings.ReplaceAll(p, `\`, "/"), repl, strings.ReplaceAll(p, `\`, `\\`), repl)
	}
	add(wsDir, "<workspace>")
	if ws != nil && ws.SourceDir != wsDir {
		add(ws.SourceDir, "<source>")
	}
	if h, err := os.UserHomeDir(); err == nil {
		add(h, "<home>")
	}
	r := strings.NewReplacer(pairs...)
	return func(s string) string {
		out := r.Replace(s)
		return strings.ReplaceAll(out, `<workspace>\`, "<workspace>/")
	}
}

func scrubMap(m map[string]any, scrub func(string) string) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		switch x := v.(type) {
		case string:
			out[k] = scrub(x)
		case []any:
			cp := make([]any, len(x))
			for i, e := range x {
				if s, ok := e.(string); ok {
					cp[i] = scrub(s)
				} else {
					cp[i] = e
				}
			}
			out[k] = cp
		case []string:
			cp := make([]string, len(x))
			for i, e := range x {
				cp[i] = scrub(e)
			}
			out[k] = cp
		default:
			out[k] = v
		}
	}
	return out
}

// maskedConfig returns the workspace configuration as JSON with the values of
// sensitive environment variables replaced (names stay).
func maskedConfig(ws *domain.Workspace) (string, *execution.Config) {
	if ws == nil {
		return "", nil
	}
	cfg, err := execution.ParseConfig(ws.ConfigYAML)
	if err != nil {
		return "", nil
	}
	mask := func(m map[string]string) {
		for k, v := range m {
			m[k] = redact.Value(k, v)
		}
	}
	mask(cfg.Env)
	for i := range cfg.Setup {
		mask(cfg.Setup[i].Env)
	}
	for i := range cfg.Start {
		mask(cfg.Start[i].Env)
	}
	for i := range cfg.Services {
		mask(cfg.Services[i].Env)
	}
	y, _ := yaml.Marshal(cfg)
	return string(y), cfg
}

func files_isSensitive(p string) bool { return files.SensitivePath(p) }

func isText(b []byte) bool { return files.IsText(b) }

func unified(path, a, b string) string {
	return files.UnifiedDiff("a/"+path, "b/"+path, a, b)
}

// classify decides how reproducible the execution is, and why.
func classify(d *execution.Detail, cfg *execution.Config, env *execution.Environment, masked int) Reproduction {
	r := Reproduction{Reasons: []string{}, Missing: []string{}, Replayable: []string{}}
	if env.Git.Repo || env.Git.Head != "" {
		r.Git = &GitPin{Commit: env.Git.Head, Branch: env.Git.Branch, Clean: !env.Git.Dirty}
	}
	if cfg != nil {
		for _, c := range cfg.Setup {
			r.Replayable = append(r.Replayable, "setup:"+c.Name)
		}
		for _, c := range cfg.Start {
			r.Replayable = append(r.Replayable, "start:"+c.Name)
		}
		for _, s := range cfg.Services {
			r.Replayable = append(r.Replayable, "service:"+s.Name)
		}
		if cfg.Verify != nil {
			for _, c := range cfg.Verify.Checks {
				r.Replayable = append(r.Replayable, "verify:"+c.Name)
			}
		}
	}
	ran := 0
	adhoc := 0
	for _, c := range d.Commands {
		ran++
		if c.Kind == "adhoc" {
			adhoc++
		}
	}
	state := d.Execution.State
	switch {
	case ran == 0 && state != domain.StateCompleted:
		r.Class = ReproNone
		r.Reasons = append(r.Reasons, "the execution ended before any command ran, so there is nothing to reproduce")
		return r
	case cfg == nil || len(r.Replayable) == 0:
		r.Class = ReproMetadataOnly
		r.Reasons = append(r.Reasons, "no replayable workspace configuration was recorded; only the observed commands and results are available")
		r.Missing = append(r.Missing, "workspace configuration with setup/start/service/verify steps")
		return r
	}
	partial := false
	why := func(s string) { partial = true; r.Reasons = append(r.Reasons, s) }
	if r.Git == nil {
		why("the project is not a git repository, so the exact source state cannot be re-created")
		r.Missing = append(r.Missing, "source commit")
	} else if !r.Git.Clean {
		why("the working tree had uncommitted changes; the commit alone does not capture the starting state")
		r.Missing = append(r.Missing, "uncommitted changes at start")
	}
	if adhoc > 0 {
		why(fmt.Sprintf("%d ad-hoc command(s) ran that are recorded but not replayed automatically", adhoc))
	}
	if masked > 0 {
		why(fmt.Sprintf("%d command(s) had secret arguments masked and cannot be replayed verbatim", masked))
		r.Missing = append(r.Missing, "masked command arguments")
	}
	if cfg.InheritEnv {
		why("the execution inherited the whole parent environment, which is not recorded")
		r.Missing = append(r.Missing, "parent environment")
	}
	if len(cfg.Env) > 0 {
		for k, v := range cfg.Env {
			if redact.IsSensitiveKey(k) && v != "" {
				why("secret environment variable values are not stored in receipts")
				r.Missing = append(r.Missing, "values of secret environment variables")
				break
			}
		}
	}
	if d.Execution.Isolation == domain.IsolationDocker || d.Execution.Isolation == domain.IsolationWSL {
		why("container image contents / distribution state are not captured; only their names")
		r.Missing = append(r.Missing, "image or distribution contents")
	}
	if len(env.Tools) == 0 {
		why("no tool versions were recorded")
	}
	r.Missing = dedupe(r.Missing)
	if partial {
		r.Class = ReproPartial
	} else {
		r.Class = ReproFull
		r.Reasons = append(r.Reasons, "configuration, source commit and tool versions are recorded; reproduction is exact only within the declared constraints (same OS, same tool versions, no external state)")
	}
	return r
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	out := in[:0]
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
