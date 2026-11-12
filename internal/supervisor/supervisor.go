// Package supervisor runs and watches RUNTIDE-managed development processes.
// Each service is placed in a Windows Job Object so its whole process tree is
// stopped together and can never outlive RUNTIDE.
package supervisor

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/runtide/runtide/internal/config"
	"github.com/runtide/runtide/internal/health"
	"github.com/runtide/runtide/internal/logs"
	winapi "github.com/runtide/runtide/internal/platform/windows"
	"golang.org/x/sys/windows"
)

// State of a managed service.
type State string

const (
	StateStopped  State = "stopped"
	StateStarting State = "starting"
	StateRunning  State = "running"
	StateStopping State = "stopping"
	StateBackoff  State = "backoff" // waiting to restart after a crash
	StateExited   State = "exited"  // ended by itself with code 0
	StateFailed   State = "failed"  // crashed / gave up restarting / failed to start
)

// Event describes something notable (used for notifications and activity).
type Event struct {
	Kind   string `json:"kind"` // started|stopped|exited|crashed|restarting|gave-up|health
	Key    string `json:"key"`
	Detail string `json:"detail"`
	Level  string `json:"level"` // info|warn|error
}

// Recorder persists run history; every method must be safe to call concurrently.
type Recorder interface {
	RunStarted(key string, pid int) int64
	RunEnded(id int64, exitCode int, reason string)
	HealthEvent(key, status, detail string)
}

// Info is a read-only view of an instance.
type Info struct {
	Key          string             `json:"key"`
	Spec         config.ServiceSpec `json:"spec"`
	Root         string             `json:"root"`
	State        State              `json:"state"`
	PID          int                `json:"pid"`
	StartedAt    time.Time          `json:"startedAt,omitempty"`
	UptimeSec    float64            `json:"uptimeSec"`
	ExitCode     *int               `json:"exitCode"`
	LastError    string             `json:"lastError,omitempty"`
	Restarts     int                `json:"restarts"` // consecutive
	TotalRestart int                `json:"totalRestarts"`
	Health       health.Status      `json:"health"`
	HealthDetail string             `json:"healthDetail,omitempty"`
	NextRetryAt  time.Time          `json:"nextRetryAt,omitempty"`
}

type instance struct {
	key  string
	spec config.ServiceSpec
	root string

	mu        sync.Mutex
	state     State
	pid       int
	startedAt time.Time
	exitCode  *int
	lastErr   string
	restarts  int
	totalRest int
	nextRetry time.Time
	stopReq   bool
	job       *winapi.Job
	exited    chan struct{}      // closed when the current process (tree) is fully gone
	cancel    context.CancelFunc // cancels the monitor loop incl. backoff waits
	loopDone  chan struct{}
}

// Manager supervises many services.
type Manager struct {
	hub  *logs.Hub
	rec  Recorder
	mon  *health.Monitor
	mu   sync.RWMutex
	inst map[string]*instance
	subs []func(Event)
	ctx  context.Context
	stop context.CancelFunc
}

// NewManager creates a manager. rec may be nil.
func NewManager(hub *logs.Hub, rec Recorder) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{hub: hub, rec: rec, inst: map[string]*instance{}, ctx: ctx, stop: cancel}
	m.mon = health.NewMonitor(func(c health.Change) {
		if rec != nil {
			rec.HealthEvent(c.Key, string(c.To), c.Detail)
		}
		lvl := "info"
		if c.To == health.Unhealthy {
			lvl = "warn"
		}
		if c.To == health.Healthy || c.To == health.Unhealthy {
			m.emit(Event{Kind: "health", Key: c.Key, Detail: fmt.Sprintf("%s: %s", c.To, c.Detail), Level: lvl})
		}
	})
	return m
}

// OnEvent registers an event listener.
func (m *Manager) OnEvent(f func(Event)) {
	m.mu.Lock()
	m.subs = append(m.subs, f)
	m.mu.Unlock()
}

func (m *Manager) emit(e Event) {
	m.mu.RLock()
	subs := append([]func(Event){}, m.subs...)
	m.mu.RUnlock()
	for _, f := range subs {
		f(e)
	}
}

func (m *Manager) sys(key, format string, a ...any) {
	m.hub.Append(key, "system", fmt.Sprintf(format, a...))
}

// ManagedPIDs maps the root pid of each running service to its key.
func (m *Manager) ManagedPIDs() map[uint32]string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := map[uint32]string{}
	for k, i := range m.inst {
		i.mu.Lock()
		if i.pid > 0 && (i.state == StateRunning || i.state == StateStarting || i.state == StateStopping) {
			out[uint32(i.pid)] = k
		}
		i.mu.Unlock()
	}
	return out
}

// Start launches a service. It returns once the process is started (not once
// it is ready; see WaitReady). Starting an already running service is an error.
func (m *Manager) Start(key string, spec config.ServiceSpec, root string) error {
	spec.ApplyDefaults()
	if probs := config.ValidateService(&spec, root); len(probs) > 0 {
		return &config.ValidationError{Problems: probs}
	}
	m.mu.Lock()
	in := m.inst[key]
	if in == nil {
		in = &instance{key: key, state: StateStopped}
		m.inst[key] = in
	}
	m.mu.Unlock()

	in.mu.Lock()
	switch in.state {
	case StateRunning, StateStarting, StateStopping, StateBackoff:
		st := in.state
		in.mu.Unlock()
		return fmt.Errorf("service %s is already %s", key, st)
	}
	in.spec, in.root = spec, root
	in.restarts, in.stopReq, in.exitCode, in.lastErr = 0, false, nil, ""
	ctx, cancel := context.WithCancel(m.ctx)
	in.cancel = cancel
	in.loopDone = make(chan struct{})
	in.state = StateStarting
	in.mu.Unlock()

	if err := m.launch(ctx, in); err != nil {
		in.mu.Lock()
		in.state, in.lastErr = StateFailed, err.Error()
		cancel()
		close(in.loopDone)
		in.mu.Unlock()
		m.sys(key, "failed to start: %v", err)
		m.emit(Event{Kind: "crashed", Key: key, Detail: "failed to start: " + err.Error(), Level: "error"})
		return err
	}
	go m.watch(ctx, in)
	return nil
}

func resolveCommand(cmd, workdir string) (string, error) {
	if strings.ContainsAny(cmd, `\/`) && !filepath.IsAbs(cmd) {
		cmd = filepath.Join(workdir, cmd)
	}
	p, err := exec.LookPath(cmd)
	if err != nil && !errors.Is(err, exec.ErrDot) {
		return "", fmt.Errorf("executable %q not found: %w", cmd, err)
	}
	if err != nil {
		return filepath.Abs(p)
	}
	return p, nil
}

func buildEnv(spec config.ServiceSpec, root string) ([]string, error) {
	env := os.Environ()
	extra := map[string]string{}
	if spec.EnvFile != "" {
		p, err := config.ResolveDir(root, spec.EnvFile)
		if err != nil {
			return nil, err
		}
		kv, err := parseEnvFile(p)
		if err != nil {
			return nil, fmt.Errorf("env_file: %w", err)
		}
		for k, v := range kv {
			extra[k] = v
		}
	}
	for k, v := range spec.Env {
		extra[k] = v
	}
	keys := make([]string, 0, len(extra))
	for k := range extra {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		env = append(env, k+"="+extra[k])
	}
	return env, nil
}

func parseEnvFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		k, v, ok := strings.Cut(line, "=")
		k = strings.TrimSpace(k)
		if !ok || !config.ValidEnvKey(k) {
			continue
		}
		v = strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' && v[len(v)-1] == '"' || v[0] == '\'' && v[len(v)-1] == '\'') {
			v = v[1 : len(v)-1]
		}
		out[k] = v
	}
	return out, sc.Err()
}

// launch starts the OS process for the instance's current spec. The process is
// created suspended, added to a fresh job object, then resumed so no child can
// escape the job.
func (m *Manager) launch(ctx context.Context, in *instance) error {
	in.mu.Lock()
	spec, root, key := in.spec, in.root, in.key
	in.mu.Unlock()

	workdir := root
	if spec.WorkingDirectory != "" {
		d, err := config.ResolveDir(root, spec.WorkingDirectory)
		if err != nil {
			return err
		}
		workdir = d
	}
	if workdir != "" {
		if st, err := os.Stat(workdir); err != nil || !st.IsDir() {
			return fmt.Errorf("working directory %q does not exist", workdir)
		}
	}
	exe, err := resolveCommand(spec.Command, workdir)
	if err != nil {
		return err
	}
	env, err := buildEnv(spec, root)
	if err != nil {
		return err
	}
	job, err := winapi.NewJob()
	if err != nil {
		return err
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		_ = job.Close()
		return err
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		outR.Close()
		outW.Close()
		_ = job.Close()
		return err
	}
	cmd := exec.Command(exe, spec.Args...)
	cmd.Dir = workdir
	cmd.Env = env
	cmd.Stdout, cmd.Stderr = outW, errW
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_SUSPENDED | windows.CREATE_NO_WINDOW,
		HideWindow:    true,
	}
	if err := cmd.Start(); err != nil {
		outR.Close()
		outW.Close()
		errR.Close()
		errW.Close()
		_ = job.Close()
		return fmt.Errorf("start %s: %w", spec.Command, err)
	}
	outW.Close()
	errW.Close()
	pid := uint32(cmd.Process.Pid)
	if err := job.Assign(pid); err != nil {
		_ = cmd.Process.Kill()
		cmd.Wait()
		outR.Close()
		errR.Close()
		_ = job.Close()
		return err
	}
	if err := winapi.ResumeProcess(pid); err != nil {
		_ = job.Terminate(1)
		cmd.Wait()
		outR.Close()
		errR.Close()
		_ = job.Close()
		return err
	}

	var runID int64
	if m.rec != nil {
		runID = m.rec.RunStarted(key, int(pid))
	}
	exited := make(chan struct{})
	startedAt := time.Now()
	in.mu.Lock()
	in.pid, in.startedAt, in.job, in.exited = int(pid), startedAt, job, exited
	in.state = StateRunning
	in.exitCode = nil
	in.mu.Unlock()
	m.sys(key, "started %s (pid %d)", filepath.Base(exe), pid)
	m.emit(Event{Kind: "started", Key: key, Detail: fmt.Sprintf("pid %d", pid), Level: "info"})

	var readers sync.WaitGroup
	readers.Add(2)
	go func() { defer readers.Done(); m.hub.Capture(key, "stdout", outR); outR.Close() }()
	go func() { defer readers.Done(); m.hub.Capture(key, "stderr", errR); errR.Close() }()

	if spec.Health != nil {
		hctx, hcancel := context.WithCancel(ctx)
		go func() {
			<-exited
			hcancel()
		}()
		go m.mon.Run(hctx, key, spec.Health, startedAt, func() bool { return winapi.IsRunning(pid) })
	} else {
		m.mon.Set(key, health.NotMonitored, "no health check configured")
	}

	go func() {
		werr := cmd.Wait()
		code := 0
		if werr != nil {
			var ee *exec.ExitError
			if errors.As(werr, &ee) {
				code = ee.ExitCode()
			} else {
				code = -1
			}
		}
		// The service is "gone" only when the whole tree is gone; if a
		// leader exits but descendants remain they are cleaned up with the job.
		if n, err := job.ActiveProcesses(); err == nil && n > 0 {
			_ = job.Terminate(1)
		}
		done := make(chan struct{})
		go func() { readers.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
		_ = job.Close()
		in.mu.Lock()
		c := code
		in.exitCode = &c
		in.pid = 0
		in.job = nil
		stopReq := in.stopReq
		in.mu.Unlock()
		reason := "exited"
		if stopReq {
			reason = "stopped by user"
		}
		if m.rec != nil {
			m.rec.RunEnded(runID, code, reason)
		}
		close(exited)
	}()
	return nil
}

// watch handles exit, restart policy, and backoff for an instance.
func (m *Manager) watch(ctx context.Context, in *instance) {
	defer close(in.loopDone)
	for {
		in.mu.Lock()
		exited := in.exited
		in.mu.Unlock()
		select {
		case <-exited:
		case <-ctx.Done():
			// Stop() drives the shutdown; if we were cancelled while a process
			// is still alive (e.g. a restart raced with Stop), kill its tree so
			// nothing is orphaned.
			in.mu.Lock()
			j := in.job
			in.mu.Unlock()
			if j != nil {
				_ = j.Terminate(1)
			}
			<-exited
		}
		in.mu.Lock()
		code := 0
		if in.exitCode != nil {
			code = *in.exitCode
		}
		stopReq := in.stopReq
		spec := in.spec
		ranFor := time.Since(in.startedAt)
		key := in.key
		in.mu.Unlock()
		m.mon.Set(key, healthAfterExit(spec), "process not running")

		if stopReq || ctx.Err() != nil {
			in.mu.Lock()
			in.state = StateStopped
			in.mu.Unlock()
			m.sys(key, "stopped")
			m.emit(Event{Kind: "stopped", Key: key, Detail: "stopped", Level: "info"})
			return
		}
		restart := spec.Restart.Policy == "always" || (spec.Restart.Policy == "on-failure" && code != 0)
		if code == 0 && !restart {
			in.mu.Lock()
			in.state = StateExited
			in.mu.Unlock()
			m.sys(key, "exited with code 0")
			m.emit(Event{Kind: "exited", Key: key, Detail: "exited with code 0", Level: "info"})
			return
		}
		in.mu.Lock()
		if ranFor > 60*time.Second {
			in.restarts = 0 // it was stable; forgive earlier crashes
		}
		if !restart || in.restarts >= spec.Restart.MaxRetries {
			in.state = StateFailed
			msg := fmt.Sprintf("exited with code %d", code)
			if restart {
				msg += fmt.Sprintf("; gave up after %d restarts", in.restarts)
			}
			in.lastErr = msg
			in.mu.Unlock()
			m.sys(key, "%s", msg)
			kind := "crashed"
			if restart {
				kind = "gave-up"
			}
			m.emit(Event{Kind: kind, Key: key, Detail: msg, Level: "error"})
			return
		}
		delay := backoff(spec.Restart, in.restarts)
		in.restarts++
		in.totalRest++
		n := in.restarts
		in.state = StateBackoff
		in.nextRetry = time.Now().Add(delay)
		in.lastErr = fmt.Sprintf("exited with code %d", code)
		in.mu.Unlock()
		m.sys(key, "exited with code %d; restart %d/%d in %s", code, n, spec.Restart.MaxRetries, delay.Round(time.Millisecond))
		m.emit(Event{Kind: "restarting", Key: key, Detail: fmt.Sprintf("exit %d, retry %d/%d in %s", code, n, spec.Restart.MaxRetries, delay.Round(time.Millisecond)), Level: "warn"})
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			in.mu.Lock()
			in.state = StateStopped
			in.mu.Unlock()
			m.sys(key, "restart cancelled")
			return
		}
		in.mu.Lock()
		in.state = StateStarting
		in.mu.Unlock()
		if err := m.launch(ctx, in); err != nil {
			in.mu.Lock()
			in.state, in.lastErr = StateFailed, err.Error()
			in.mu.Unlock()
			m.sys(key, "restart failed: %v", err)
			m.emit(Event{Kind: "crashed", Key: key, Detail: "restart failed: " + err.Error(), Level: "error"})
			return
		}
	}
}

func healthAfterExit(spec config.ServiceSpec) health.Status {
	if spec.Health == nil {
		return health.NotMonitored
	}
	return health.Unknown
}

func backoff(r config.RestartSpec, attempt int) time.Duration {
	d := time.Duration(r.BackoffMs) * time.Millisecond
	for i := 0; i < attempt && d < time.Duration(r.MaxBackoffMs)*time.Millisecond; i++ {
		d *= 2
	}
	if max := time.Duration(r.MaxBackoffMs) * time.Millisecond; d > max {
		d = max
	}
	return d
}

// Stop stops a service: first a graceful console interrupt, then after
// stop_timeout the entire job object is terminated. It returns only when the
// process tree is gone (or an error says otherwise).
func (m *Manager) Stop(key string) error {
	m.mu.RLock()
	in := m.inst[key]
	m.mu.RUnlock()
	if in == nil {
		return fmt.Errorf("service %s is not running", key)
	}
	in.mu.Lock()
	switch in.state {
	case StateStopped, StateExited, StateFailed:
		in.mu.Unlock()
		return nil
	case StateStopping:
		loop := in.loopDone
		in.mu.Unlock()
		<-loop
		return nil
	}
	wasBackoff := in.state == StateBackoff
	in.stopReq = true
	in.state = StateStopping
	pid, job, exited, cancel, loop := uint32(in.pid), in.job, in.exited, in.cancel, in.loopDone
	timeout := time.Duration(in.spec.StopTimeoutSec) * time.Second
	in.mu.Unlock()

	if wasBackoff || job == nil {
		cancel()
		<-loop
		return nil
	}
	m.sys(key, "stopping (grace %s)", timeout)
	graceful := false
	if err := winapi.GracefulInterrupt(pid); err == nil {
		graceful = true
	}
	select {
	case <-exited:
	case <-time.After(timeout):
		if graceful {
			m.sys(key, "did not exit within %s; terminating process tree", timeout)
		}
		_ = job.Terminate(1)
		select {
		case <-exited:
		case <-time.After(10 * time.Second):
			cancel()
			return fmt.Errorf("service %s did not terminate", key)
		}
	}
	cancel()
	<-loop
	return nil
}

// Restart stops then starts the service with its last spec.
func (m *Manager) Restart(key string) error {
	m.mu.RLock()
	in := m.inst[key]
	m.mu.RUnlock()
	if in == nil {
		return fmt.Errorf("service %s is unknown to the supervisor; start it first", key)
	}
	in.mu.Lock()
	spec, root := in.spec, in.root
	in.mu.Unlock()
	if err := m.Stop(key); err != nil {
		return err
	}
	return m.Start(key, spec, root)
}

// WaitReady blocks until the service is ready: healthy when it has a health
// check, otherwise alive for at least settle. It fails fast if the process dies.
func (m *Manager) WaitReady(ctx context.Context, key string, timeout, settle time.Duration) error {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	t := time.NewTicker(100 * time.Millisecond)
	defer t.Stop()
	var runningSince time.Time
	for {
		inf, ok := m.Get(key)
		if !ok {
			return fmt.Errorf("service %s does not exist", key)
		}
		switch inf.State {
		case StateFailed, StateExited, StateStopped:
			if inf.LastError != "" {
				return fmt.Errorf("service %s %s: %s", key, inf.State, inf.LastError)
			}
			return fmt.Errorf("service %s %s before becoming ready", key, inf.State)
		case StateRunning:
			if inf.Spec.Health != nil {
				if inf.Health == health.Healthy {
					return nil
				}
			} else {
				if runningSince.IsZero() {
					runningSince = time.Now()
				}
				if time.Since(runningSince) >= settle {
					return nil
				}
			}
		default:
			runningSince = time.Time{}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("service %s was not ready within %s (state %s, health %s)", key, timeout, inf.State, inf.Health)
		case <-t.C:
		}
	}
}

// Get returns a snapshot of one service.
func (m *Manager) Get(key string) (Info, bool) {
	m.mu.RLock()
	in := m.inst[key]
	m.mu.RUnlock()
	if in == nil {
		return Info{}, false
	}
	return m.info(in), true
}

func (m *Manager) info(in *instance) Info {
	in.mu.Lock()
	inf := Info{Key: in.key, Spec: in.spec, Root: in.root, State: in.state, PID: in.pid, StartedAt: in.startedAt,
		ExitCode: in.exitCode, LastError: in.lastErr, Restarts: in.restarts, TotalRestart: in.totalRest}
	if in.state == StateRunning || in.state == StateStarting || in.state == StateStopping {
		inf.UptimeSec = time.Since(in.startedAt).Seconds()
	}
	if in.state == StateBackoff {
		inf.NextRetryAt = in.nextRetry
	}
	spec := in.spec
	st := in.state
	in.mu.Unlock()
	if st == StateRunning || st == StateStarting {
		inf.Health, inf.HealthDetail = m.mon.Status(in.key)
	} else if spec.Health != nil {
		inf.Health = health.Unknown
	} else {
		inf.Health = health.NotMonitored
	}
	return inf
}

// List returns all known services sorted by key.
func (m *Manager) List() []Info {
	m.mu.RLock()
	ins := make([]*instance, 0, len(m.inst))
	for _, i := range m.inst {
		ins = append(ins, i)
	}
	m.mu.RUnlock()
	out := make([]Info, 0, len(ins))
	for _, i := range ins {
		out = append(out, m.info(i))
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Key < out[b].Key })
	return out
}

// Forget drops a stopped service from the supervisor.
func (m *Manager) Forget(key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	in := m.inst[key]
	if in == nil {
		return nil
	}
	in.mu.Lock()
	defer in.mu.Unlock()
	switch in.state {
	case StateRunning, StateStarting, StateStopping, StateBackoff:
		return fmt.Errorf("service %s is %s", key, in.state)
	}
	delete(m.inst, key)
	m.mon.Forget(key)
	return nil
}

// Shutdown stops every service (in parallel) and releases resources.
func (m *Manager) Shutdown(ctx context.Context) error {
	m.mu.RLock()
	keys := make([]string, 0, len(m.inst))
	for k := range m.inst {
		keys = append(keys, k)
	}
	m.mu.RUnlock()
	var wg sync.WaitGroup
	var mu sync.Mutex
	var errs []error
	for _, k := range keys {
		wg.Add(1)
		go func(k string) {
			defer wg.Done()
			if err := m.Stop(k); err != nil {
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
			}
		}(k)
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
		errs = append(errs, ctx.Err())
	}
	m.stop()
	return errors.Join(errs...)
}
