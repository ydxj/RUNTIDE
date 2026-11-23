package execution

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/runtide/runtide/internal/capsule"
	"github.com/runtide/runtide/internal/config"
	"github.com/runtide/runtide/internal/domain"
	"github.com/runtide/runtide/internal/files"
	"github.com/runtide/runtide/internal/logs"
	"github.com/runtide/runtide/internal/store"
	"github.com/runtide/runtide/internal/supervisor"
	"github.com/runtide/runtide/internal/tools"
	"github.com/runtide/runtide/internal/verification"
)

// Errors callers can match.
var (
	ErrNotApproved = errors.New("workspace configuration has not been approved for execution")
	ErrBusy        = errors.New("workspace already has an active execution")
	ErrNotRunning  = errors.New("execution is not in the RUNNING state")
)

// Manager runs executions.
type Manager struct {
	St      *store.Store
	Rec     *Recorder
	Sup     *supervisor.Manager
	Hub     *logs.Hub
	Home    string // RUNTIDE data directory (worktrees live below it)
	Version string
	Tools   func(ctx context.Context) []tools.Tool // optional; defaults to tools.Detect

	mu     sync.Mutex
	active map[string]*activeExec // by execution id
}

// NewManager creates a manager.
func NewManager(st *store.Store, rec *Recorder, sup *supervisor.Manager, hub *logs.Hub, home, version string) *Manager {
	return &Manager{St: st, Rec: rec, Sup: sup, Hub: hub, Home: home, Version: version, active: map[string]*activeExec{}}
}

type svcRun struct {
	name, key string
	cr        *CmdRec
	mpID      int64
	cleanup   func() capsule.Spec
	stopSub   func()
}

type activeExec struct {
	id      string
	ws      *store.WorkspaceRow
	cfg     *Config
	iso     domain.Isolation
	dir     string
	ctx     context.Context
	cancel  context.CancelFunc
	before  *files.Snapshot
	mu      sync.Mutex
	running map[string]context.CancelFunc
	svcs    []*svcRun
	seq     int
	ended   bool
}

func (a *activeExec) nextSeq() int { a.mu.Lock(); defer a.mu.Unlock(); a.seq++; return a.seq }

func (m *Manager) get(id string) *activeExec {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.active[id]
}

// IsActive reports whether the execution is being driven by this manager.
func (m *Manager) IsActive(id string) bool { return m.get(id) != nil }

func (m *Manager) ev(execID, typ, sev, subject string, data map[string]any) {
	_, _ = m.Rec.Event(execID, typ, domain.SourceObserved, sev, subject, data)
}

// NewExecution creates an execution (state CREATED) in a READY workspace.
func (m *Manager) NewExecution(wsRef, task string) (*domain.Execution, error) {
	ws, err := m.St.FindWorkspace(wsRef)
	if err != nil {
		return nil, fmt.Errorf("workspace %q: %w", wsRef, err)
	}
	if ws.State != domain.StateReady {
		return nil, fmt.Errorf("workspace %s is %s; it must be READY (reopen it first if it already ran)", ws.Name, ws.State)
	}
	if task == "" {
		task = ws.Task
	}
	e := &domain.Execution{WorkspaceID: ws.ID, ProjectID: ws.ProjectID, Task: task, Isolation: ws.Isolation, RuntideVersion: m.Version}
	if err := m.St.CreateExecution(e); err != nil {
		return nil, err
	}
	m.ev(e.ID, domain.EvExecutionCreated, domain.SevInfo, "", map[string]any{"task": task, "workspace": ws.Name, "isolation": ws.Isolation.Name()})
	return e, nil
}

// Start prepares the workspace, runs setup and start commands and brings
// services up. On success the execution is RUNNING; on failure it is FAILED
// (services stopped, evidence kept) and the error says why.
func (m *Manager) Start(ctx context.Context, execID string) error {
	x, err := m.St.GetExecution(execID)
	if err != nil {
		return err
	}
	if x.State != domain.StateCreated {
		return fmt.Errorf("execution %s is %s; only CREATED executions can be started", x.ID, x.State)
	}
	ws, err := m.St.GetWorkspaceRow(x.WorkspaceID)
	if err != nil {
		return err
	}
	cfg, err := ParseConfig(ws.ConfigYAML)
	if err != nil {
		return err
	}
	if err := cfg.Validate(ws.Dir); err != nil {
		return err
	}
	hash, _ := cfg.Hash(ws.Dir)
	if cfg.HasExecutables() && ws.ApprovedHash != hash {
		return ErrNotApproved
	}
	iso, _ := cfg.IsolationLevel()
	actx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	a := &activeExec{id: x.ID, ws: ws, cfg: cfg, iso: iso, dir: ws.Dir, ctx: actx, cancel: cancel, running: map[string]context.CancelFunc{}}
	m.mu.Lock()
	for _, o := range m.active {
		if o.ws.ID == ws.ID {
			m.mu.Unlock()
			cancel()
			return ErrBusy
		}
	}
	m.active[x.ID] = a
	m.mu.Unlock()

	fail := func(err error) error {
		m.finalize(a, domain.StateFailed, err)
		return err
	}
	if err := m.St.TransitionExecution(x.ID, domain.StateConfiguring); err != nil {
		return fail(err)
	}
	if err := m.St.TransitionWorkspace(ws.ID, domain.StateRunning, ""); err != nil {
		return fail(err)
	}
	// --- prepare
	envJSON := m.environmentSnapshot(ctx, a)
	_ = m.St.SetExecutionEnvironment(x.ID, envJSON)
	snap, err := files.Take(files.Options{Root: ws.Dir, Exclude: cfg.Exclude})
	if err != nil {
		return fail(fmt.Errorf("could not snapshot workspace files: %w", err))
	}
	a.before = snap
	for p, content := range files.Baselines(ws.Dir, snap, 0, 0) {
		_ = m.St.PutBaseline(x.ID, p, content)
	}
	m.ev(x.ID, domain.EvWorkspacePrepared, domain.SevInfo, "", map[string]any{"dir": ws.Dir, "trackedFiles": len(snap.Files), "skipped": snap.Skipped, "gitBacked": snap.UsedGit})
	for _, cs := range cfg.Setup {
		if err := m.runSpec(a, "setup", cs); err != nil {
			return fail(err)
		}
	}
	if err := m.St.TransitionExecution(x.ID, domain.StateReady); err != nil {
		return fail(err)
	}
	if err := m.St.TransitionExecution(x.ID, domain.StateRunning); err != nil {
		return fail(err)
	}
	m.ev(x.ID, domain.EvStateChanged, domain.SevInfo, "", map[string]any{"to": string(domain.StateRunning)})
	for _, cs := range cfg.Start {
		if err := m.runSpec(a, "start", cs); err != nil {
			return fail(err)
		}
	}
	if err := m.startServices(a); err != nil {
		return fail(err)
	}
	return nil
}

func (m *Manager) runSpec(a *activeExec, kind string, cs CommandSpec) error {
	dir := a.dir
	if cs.Dir != "" {
		d, err := config.ResolveInside(a.dir, cs.Dir)
		if err != nil {
			return err
		}
		dir = d
	}
	timeout := time.Duration(cs.TimeoutSec) * time.Second
	c, _, res := m.runCommand(a.ctx, a, kind, cs.Name, cs.Command, cs.Args, dir, cs.Env, timeout)
	if res.Err != nil {
		return fmt.Errorf("%s %q could not run: %w", kind, cs.Name, res.Err)
	}
	if res.Cancelled {
		return fmt.Errorf("%s %q was cancelled", kind, cs.Name)
	}
	if res.TimedOut {
		return fmt.Errorf("%s %q timed out after %s", kind, cs.Name, timeout)
	}
	if res.ExitCode != 0 && !cs.AllowFailure {
		return fmt.Errorf("%s %q failed with exit code %d (command %s)", kind, cs.Name, res.ExitCode, c.ID)
	}
	return nil
}

// runCommand records and executes one command inside the workspace's isolation level.
func (m *Manager) runCommand(parent context.Context, a *activeExec, kind, name, exe string, args []string, dir string, env map[string]string, timeout time.Duration) (*domain.Command, string, capsule.Result) {
	seq := a.nextSeq()
	cmd := &domain.Command{ExecutionID: a.id, Kind: kind, Name: name, Executable: exe, Args: append([]string(nil), args...), Cwd: dir, Isolation: a.iso, StartedAt: time.Now()}
	merged := map[string]string{}
	for k, v := range a.cfg.Env {
		merged[k] = v
	}
	for k, v := range env {
		merged[k] = v
	}
	for k := range merged {
		cmd.EnvKeys = append(cmd.EnvKeys, k)
	}
	cr, err := m.Rec.StartCommand(cmd)
	if err != nil {
		return cmd, "", capsule.Result{ExitCode: -1, Err: err}
	}
	subject := name
	m.ev(a.id, domain.EvCommandStarted, domain.SevInfo, subject, map[string]any{"commandId": cmd.ID, "kind": kind, "executable": exe, "args": cmd.Args, "cwd": dir, "isolation": a.iso.Name()})
	spec := capsule.Spec{Executable: exe, Args: args, Dir: dir, Env: merged, InheritAll: a.cfg.InheritEnv, Timeout: timeout}
	wrapped, cleanup, err := capsule.Wrap(a.iso, spec, fmt.Sprintf("%s-%d", a.id, seq), a.cfg.Docker, a.cfg.WSL)
	var res capsule.Result
	if err != nil {
		res = capsule.Result{ExitCode: -1, Err: err}
	} else {
		ctx, cancel := context.WithCancel(parent)
		a.mu.Lock()
		a.running[cmd.ID] = cancel
		a.mu.Unlock()
		var mpID int64
		sink := &mpSink{CmdRec: cr, onStart: func(pid int, ns int64) {
			mpID, _ = m.St.AddManagedProcess(store.ManagedProcess{ExecutionID: a.id, CommandID: cmd.ID, PID: pid, StartTimeNs: ns, Exe: wrapped.Executable})
			m.ev(a.id, domain.EvProcessStarted, domain.SevInfo, subject, map[string]any{"commandId": cmd.ID, "pid": pid})
		}}
		res = capsule.Run(ctx, wrapped, sink)
		cancel()
		a.mu.Lock()
		delete(a.running, cmd.ID)
		a.mu.Unlock()
		if mpID != 0 {
			_ = m.St.EndManagedProcess(mpID)
			m.ev(a.id, domain.EvProcessTerminated, domain.SevInfo, subject, map[string]any{"commandId": cmd.ID, "pid": res.PID, "exitCode": res.ExitCode})
		}
		if cleanup != nil {
			c := cleanup()
			capsule.Run(context.Background(), c, nil)
		}
	}
	cr.Finish(res.ExitCode, res.Cancelled, res.TimedOut, res.Err)
	m.ev(a.id, domain.EvOutputCaptured, domain.SevInfo, subject, map[string]any{"commandId": cmd.ID, "stdoutBytes": cmd.StdoutBytes, "stderrBytes": cmd.StderrBytes, "truncated": cmd.Truncated})
	sev := domain.SevInfo
	if res.Err != nil || res.ExitCode != 0 || res.TimedOut || res.Cancelled {
		sev = domain.SevError
	}
	data := map[string]any{"commandId": cmd.ID, "exitCode": res.ExitCode, "cancelled": res.Cancelled, "timedOut": res.TimedOut,
		"durationMs": time.Since(cmd.StartedAt).Milliseconds(), "truncated": cmd.Truncated}
	if res.Err != nil {
		data["error"] = res.Err.Error()
	}
	m.ev(a.id, domain.EvCommandFinished, sev, subject, data)
	return cmd, cr.Tail(), res
}

type mpSink struct {
	*CmdRec
	onStart func(pid int, ns int64)
}

func (s *mpSink) Started(pid int, ns int64) {
	s.CmdRec.Started(pid, ns)
	s.onStart(pid, ns)
}

// RunAdHoc runs an arbitrary command in a RUNNING execution. approved must be
// true: callers obtain the user's (or the configured authorisation policy's)
// consent before calling. The workspace policy is always enforced.
func (m *Manager) RunAdHoc(ctx context.Context, execID, name, exe string, args []string, approved bool, timeout time.Duration) (*domain.Command, error) {
	a := m.get(execID)
	if a == nil {
		return nil, ErrNotRunning
	}
	x, err := m.St.GetExecution(execID)
	if err != nil {
		return nil, err
	}
	if x.State != domain.StateRunning {
		return nil, ErrNotRunning
	}
	if !approved {
		return nil, errors.New("running an ad-hoc command requires explicit approval")
	}
	if err := a.cfg.checkPolicy(exe, true); err != nil {
		return nil, err
	}
	if name == "" {
		name = "adhoc-" + filepath.Base(exe)
	}
	if !config.ValidName(name) {
		return nil, fmt.Errorf("invalid command name %q", name)
	}
	c, _, res := m.runCommand(ctx, a, "adhoc", name, exe, args, a.dir, nil, timeout)
	if res.Err != nil {
		return c, res.Err
	}
	return c, nil
}

func (m *Manager) startServices(a *activeExec) error {
	if len(a.cfg.Services) == 0 {
		return nil
	}
	ws := &config.Workspace{Name: "x", Services: a.cfg.Services}
	layers, err := ws.StartOrder()
	if err != nil {
		return err
	}
	for _, layer := range layers {
		for _, n := range layer {
			if err := m.startService(a, *ws.Service(n)); err != nil {
				return err
			}
		}
		for _, n := range layer {
			spec := ws.Service(n)
			key := svcKey(a.id, n)
			timeout := time.Duration(spec.StartTimeoutSec) * time.Second
			if err := m.Sup.WaitReady(a.ctx, key, timeout, 1500*time.Millisecond); err != nil {
				return fmt.Errorf("service %s: %w", n, err)
			}
		}
	}
	return nil
}

func svcKey(execID, name string) string { return "x/" + execID + "/" + name }

func (m *Manager) startService(a *activeExec, spec config.ServiceSpec) error {
	key := svcKey(a.id, spec.Name)
	env := map[string]string{}
	for k, v := range a.cfg.Env {
		env[k] = v
	}
	for k, v := range spec.Env {
		env[k] = v
	}
	spec.Env, spec.CleanEnv = env, !a.cfg.InheritEnv
	dir := a.dir
	if spec.WorkingDirectory != "" {
		d, err := config.ResolveInside(a.dir, spec.WorkingDirectory)
		if err != nil {
			return err
		}
		dir = d
	}
	logical := spec
	var cleanup func() capsule.Spec
	if a.iso == domain.IsolationDocker || a.iso == domain.IsolationWSL {
		w, c, err := capsule.Wrap(a.iso, capsule.Spec{Executable: spec.Command, Args: spec.Args, Dir: dir, Env: env}, a.id+"-"+spec.Name, a.cfg.Docker, a.cfg.WSL)
		if err != nil {
			return err
		}
		spec.Command, spec.Args, spec.Env, spec.WorkingDirectory, cleanup = w.Executable, w.Args, w.Env, "", c
	}
	cmd := &domain.Command{ExecutionID: a.id, Kind: "service", Name: spec.Name, Executable: logical.Command, Args: append([]string(nil), logical.Args...), Cwd: dir, Isolation: a.iso, StartedAt: time.Now()}
	for k := range env {
		cmd.EnvKeys = append(cmd.EnvKeys, k)
	}
	cr, err := m.Rec.StartCommand(cmd)
	if err != nil {
		return err
	}
	ch, cancelSub := m.Hub.Subscribe(key)
	go func() {
		for e := range ch {
			if e.Stream == "stdout" || e.Stream == "stderr" {
				cr.Line(e.Stream, e.Line)
			}
		}
	}()
	m.ev(a.id, domain.EvCommandStarted, domain.SevInfo, spec.Name, map[string]any{"commandId": cmd.ID, "kind": "service", "executable": logical.Command, "args": cmd.Args, "cwd": dir})
	root := a.dir
	if a.iso == domain.IsolationDocker || a.iso == domain.IsolationWSL {
		root = dir
	}
	if err := m.Sup.Start(key, spec, root); err != nil {
		cancelSub()
		cr.Finish(-1, false, false, err)
		return fmt.Errorf("service %s: %w", spec.Name, err)
	}
	sr := &svcRun{name: spec.Name, key: key, cr: cr, cleanup: cleanup, stopSub: cancelSub}
	if inf, ok := m.Sup.Get(key); ok && inf.PID > 0 {
		cr.Started(inf.PID, 0)
		sr.mpID, _ = m.St.AddManagedProcess(store.ManagedProcess{ExecutionID: a.id, CommandID: cmd.ID, PID: inf.PID, StartTimeNs: inf.StartedAt.UnixNano(), Exe: spec.Command})
		m.ev(a.id, domain.EvProcessStarted, domain.SevInfo, spec.Name, map[string]any{"commandId": cmd.ID, "pid": inf.PID})
	}
	a.mu.Lock()
	a.svcs = append(a.svcs, sr)
	a.mu.Unlock()
	_ = m.St.UpsertService(a.id, store.ServiceRecord{Name: spec.Name, CommandID: cmd.ID, Health: "starting", LastState: "running"})
	return nil
}

func (m *Manager) stopServices(a *activeExec) {
	a.mu.Lock()
	svcs := a.svcs
	a.svcs = nil
	a.mu.Unlock()
	// reverse start order so dependents stop first
	for i := len(svcs) - 1; i >= 0; i-- {
		s := svcs[i]
		_ = m.Sup.Stop(s.key)
		inf, _ := m.Sup.Get(s.key)
		time.Sleep(50 * time.Millisecond) // let the output pump drain
		s.stopSub()
		code := 0
		if inf.ExitCode != nil {
			code = *inf.ExitCode
		}
		s.cr.Finish(code, false, false, nil)
		if s.mpID != 0 {
			_ = m.St.EndManagedProcess(s.mpID)
			m.ev(a.id, domain.EvProcessTerminated, domain.SevInfo, s.name, map[string]any{"pid": inf.PID, "exitCode": code})
		}
		if s.cleanup != nil {
			capsule.Run(context.Background(), s.cleanup(), nil)
		}
		_ = m.St.UpsertService(a.id, store.ServiceRecord{Name: s.name, CommandID: s.cr.Cmd.ID, Health: string(inf.Health), HealthDetail: inf.HealthDetail, Restarts: inf.TotalRestart, LastState: "stopped"})
		_ = m.Sup.Forget(s.key)
	}
}

// HandleSupervisorEvent turns supervisor events of execution services into recorded events.
func (m *Manager) HandleSupervisorEvent(ev supervisor.Event) {
	if !strings.HasPrefix(ev.Key, "x/") {
		return
	}
	parts := strings.SplitN(ev.Key, "/", 3)
	if len(parts) != 3 {
		return
	}
	execID, name := parts[1], parts[2]
	a := m.get(execID)
	if a == nil {
		return
	}
	inf, _ := m.Sup.Get(ev.Key)
	rec := store.ServiceRecord{Name: name, Health: string(inf.Health), HealthDetail: inf.HealthDetail, Restarts: inf.TotalRestart, LastState: string(inf.State)}
	a.mu.Lock()
	for _, s := range a.svcs {
		if s.key == ev.Key {
			rec.CommandID = s.cr.Cmd.ID
		}
	}
	a.mu.Unlock()
	_ = m.St.UpsertService(execID, rec)
	switch ev.Kind {
	case "health":
		if strings.HasPrefix(ev.Detail, "healthy") {
			m.ev(execID, domain.EvServiceHealthy, domain.SevInfo, name, map[string]any{"detail": ev.Detail})
		} else {
			m.ev(execID, domain.EvServiceUnhealthy, domain.SevWarn, name, map[string]any{"detail": ev.Detail})
		}
	case "crashed", "gave-up":
		m.ev(execID, domain.EvServiceUnhealthy, domain.SevError, name, map[string]any{"detail": ev.Detail, "kind": ev.Kind})
	case "restarting":
		m.ev(execID, domain.EvNote, domain.SevWarn, name, map[string]any{"message": ev.Detail})
	}
}

// Verify runs the workspace's verification pipeline (or pipeline when non-nil)
// against a RUNNING execution, stores the evidence and finalises the execution
// as COMPLETED (all required checks passed) or FAILED.
func (m *Manager) Verify(ctx context.Context, execID string, pipeline *verification.Pipeline) ([]domain.VerificationResult, verification.Summary, error) {
	a := m.get(execID)
	if a == nil {
		return nil, verification.Summary{}, ErrNotRunning
	}
	p := pipeline
	if p == nil {
		p = a.cfg.Verify
	}
	if p == nil {
		p = m.projectDefaultPipeline(a.ws.ProjectID)
	}
	if p == nil {
		return nil, verification.Summary{}, errors.New("no verification defined: this workspace has no verify section and its project has no default profile (use --profile or define a verify section)")
	}
	if err := p.Validate(); err != nil {
		return nil, verification.Summary{}, err
	}
	if err := m.St.TransitionExecution(execID, domain.StateVerifying); err != nil {
		return nil, verification.Summary{}, err
	}
	_ = m.St.TransitionWorkspace(a.ws.ID, domain.StateVerifying, "")
	m.ev(execID, domain.EvStateChanged, domain.SevInfo, "", map[string]any{"to": string(domain.StateVerifying)})
	m.ev(execID, domain.EvTestStarted, domain.SevInfo, p.Name, map[string]any{"checks": len(p.Checks)})
	vctx, cancel := context.WithCancel(a.ctx)
	defer cancel()
	go func() {
		select {
		case <-ctx.Done():
			cancel()
		case <-vctx.Done():
		}
	}()
	results, sum := p.Run(vctx, &verifEnv{m: m, a: a})
	if _, err := m.St.SaveVerification(execID, results); err != nil {
		m.finalize(a, domain.StateFailed, fmt.Errorf("could not store verification results: %w", err))
		return results, sum, err
	}
	for _, r := range results {
		sev := domain.SevInfo
		if r.Status == domain.VFailed || r.Status == domain.VTimedOut {
			sev = domain.SevError
		}
		typ := domain.EvVerificationPassed
		if r.Status != domain.VPassed {
			typ = domain.EvVerificationFailed
		}
		if r.Status == domain.VNotApplicable || r.Status == domain.VSkipped {
			sev = domain.SevWarn
		}
		m.ev(execID, typ, sev, r.Name, map[string]any{"status": r.Status, "durationMs": r.DurationMs, "error": r.Error, "evidence": r.Evidence})
	}
	m.ev(execID, domain.EvTestCompleted, domain.SevInfo, p.Name, map[string]any{"overall": sum.Overall, "counts": sum.Counts})
	_ = m.St.SetExecutionVerification(execID, sum.Overall)
	switch {
	case ctx.Err() != nil || a.ctx.Err() != nil:
		m.finalize(a, domain.StateCancelled, errors.New("verification was cancelled"))
	case sum.Overall == "passed":
		m.finalize(a, domain.StateCompleted, nil)
	default:
		m.finalize(a, domain.StateFailed, fmt.Errorf("verification %s: %s", sum.Overall, strings.Join(sum.Reasons, "; ")))
	}
	return results, sum, nil
}

type verifEnv struct {
	m *Manager
	a *activeExec
}

func (v *verifEnv) Dir() string { return v.a.dir }
func (v *verifEnv) RunCommand(ctx context.Context, name, exe string, args []string, dir string, timeout time.Duration) (verification.CommandOutcome, error) {
	c, tail, res := v.m.runCommand(ctx, v.a, "verify", name, exe, args, dir, nil, timeout)
	if res.Err != nil {
		return verification.CommandOutcome{CommandID: c.ID}, res.Err
	}
	return verification.CommandOutcome{ExitCode: res.ExitCode, Output: tail, CommandID: c.ID, TimedOut: res.TimedOut, Cancelled: res.Cancelled}, nil
}
func (v *verifEnv) ServiceHealth(name string) (bool, string, bool) {
	inf, ok := v.m.Sup.Get(svcKey(v.a.id, name))
	if !ok {
		return false, "", false
	}
	return inf.State == supervisor.StateRunning, string(inf.Health), true
}
func (v *verifEnv) RunningServices() []string {
	var out []string
	prefix := svcKey(v.a.id, "")
	for _, s := range v.m.Sup.List() {
		if strings.HasPrefix(s.Key, prefix) && s.State == supervisor.StateRunning {
			out = append(out, strings.TrimPrefix(s.Key, prefix))
		}
	}
	return out
}

// Finish completes a RUNNING execution without verification.
func (m *Manager) Finish(execID string) error {
	a := m.get(execID)
	if a == nil {
		return ErrNotRunning
	}
	x, err := m.St.GetExecution(execID)
	if err != nil {
		return err
	}
	if x.State != domain.StateRunning {
		return fmt.Errorf("execution is %s; only RUNNING executions can be finished", x.State)
	}
	_ = m.St.SetExecutionVerification(execID, "none")
	m.finalize(a, domain.StateCompleted, nil)
	return nil
}

// Cancel cancels an execution: running commands are interrupted, services are
// stopped and the execution ends CANCELLED. Executions that never started are
// simply marked cancelled.
func (m *Manager) Cancel(execID, reason string) error {
	x, err := m.St.GetExecution(execID)
	if err != nil {
		return err
	}
	if x.State.Terminal() {
		return fmt.Errorf("execution is already %s", x.State)
	}
	if a := m.get(execID); a != nil {
		a.cancel()
		m.finalize(a, domain.StateCancelled, errors.New(reason))
		return nil
	}
	if err := m.St.TransitionExecution(execID, domain.StateCancelled); err != nil {
		return err
	}
	m.ev(execID, domain.EvExecutionCancelled, domain.SevWarn, "", map[string]any{"reason": reason})
	return nil
}

// finalize ends an execution exactly once: stops what is running, records the
// file changes, moves the state machine and writes the closing events.
func (m *Manager) finalize(a *activeExec, final domain.State, cause error) {
	a.mu.Lock()
	if a.ended {
		a.mu.Unlock()
		return
	}
	a.ended = true
	cancels := make([]context.CancelFunc, 0, len(a.running))
	for _, c := range a.running {
		cancels = append(cancels, c)
	}
	a.mu.Unlock()
	for _, c := range cancels {
		c()
	}
	if final != domain.StateCompleted || a.cfg.StopServices() {
		m.stopServices(a)
	}
	// file changes
	if a.before != nil {
		if after, err := files.Take(files.Options{Root: a.dir, Exclude: a.cfg.Exclude}); err == nil {
			changes := files.Diff(a.before, after)
			if err := m.St.ReplaceFileChanges(a.id, changes); err == nil {
				counts := map[string]int{}
				for _, c := range changes {
					counts[c.Change]++
				}
				m.ev(a.id, domain.EvFilesChanged, domain.SevInfo, "", map[string]any{"total": len(changes), "counts": counts, "skippedSensitive": after.Skipped["sensitive"]})
			}
		}
	}
	if cause != nil {
		_ = m.St.SetExecutionError(a.id, cause.Error())
	}
	if err := m.St.TransitionExecution(a.id, final); err != nil {
		m.ev(a.id, domain.EvNote, domain.SevError, "recorder", map[string]any{"message": "could not record final state: " + err.Error()})
	}
	status := strings.ToLower(string(final))
	if cur, err := m.St.GetWorkspaceRow(a.ws.ID); err == nil && domain.CanTransitionWorkspace(cur.State, final) {
		_ = m.St.TransitionWorkspace(a.ws.ID, final, status)
	}
	typ, sev := domain.EvExecutionCompleted, domain.SevInfo
	switch final {
	case domain.StateCancelled:
		typ, sev = domain.EvExecutionCancelled, domain.SevWarn
	case domain.StateInterrupted:
		typ, sev = domain.EvExecutionInterrupted, domain.SevError
	case domain.StateFailed, domain.StateRequiresAttention:
		sev = domain.SevError
	}
	data := map[string]any{"state": string(final)}
	if cause != nil {
		data["reason"] = cause.Error()
	}
	m.ev(a.id, typ, sev, "", data)
	if a.cfg.Cleanup.RemoveWorktree && final == domain.StateCompleted {
		// best effort and safe: refuses when the worktree has uncommitted work
		_ = m.RemoveWorkspaceDir(a.ws.ID, false)
	}
	a.cancel()
	m.mu.Lock()
	delete(m.active, a.id)
	m.mu.Unlock()
}

// Environment is what an execution recorded about the machine it ran on.
type Environment = domain.Environment

// ToolVersion is a detected tool (no filesystem paths: they contain user names).
type ToolVersion = domain.ToolVersion

func (m *Manager) environmentSnapshot(ctx context.Context, a *activeExec) []byte {
	env := Environment{OS: runtime.GOOS, Arch: runtime.GOARCH, RuntideVersion: m.Version, Isolation: a.iso.Name(), InheritEnv: a.cfg.InheritEnv, CapturedAt: time.Now().UTC()}
	for k := range a.cfg.Env {
		env.EnvKeys = append(env.EnvKeys, k)
	}
	detect := m.Tools
	if detect == nil {
		detect = tools.Detect
	}
	for _, t := range detect(ctx) {
		if t.Installed {
			env.Tools = append(env.Tools, ToolVersion{ID: t.ID, Name: t.Name, Version: t.Version, OnPath: t.OnPath, Method: t.Method})
		}
	}
	env.Git = files.Git(a.dir)
	env.Git.Root = "" // local path; the commit id is what matters for reproduction
	env.Git.Status = nil
	b, _ := json.Marshal(env)
	return b
}

// Orphan is a recorded managed process that is still alive after RUNTIDE
// restarted. It is reported, never killed automatically.
type Orphan struct {
	ExecutionID string    `json:"executionId"`
	CommandID   string    `json:"commandId"`
	PID         int       `json:"pid"`
	Exe         string    `json:"exe"`
	StartedAt   time.Time `json:"startedAt"`
	Verified    bool      `json:"verified"` // pid exists AND its start time matches the record
	rowID       int64
	startNs     int64
}
