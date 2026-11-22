package execution

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/runtide/runtide/internal/database"
	"github.com/runtide/runtide/internal/domain"
	"github.com/runtide/runtide/internal/logs"
	winapi "github.com/runtide/runtide/internal/platform/windows"
	"github.com/runtide/runtide/internal/redact"
	"github.com/runtide/runtide/internal/store"
	"github.com/runtide/runtide/internal/supervisor"
	"github.com/runtide/runtide/internal/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMain doubles as the fake project tooling executed by the engine.
func TestMain(m *testing.M) {
	switch os.Getenv("EXEC_HELPER") {
	case "":
		os.Exit(m.Run())
	case "serve":
		l, err := net.Listen("tcp", "127.0.0.1:"+os.Getenv("HELPER_PORT"))
		if err != nil {
			os.Exit(4)
		}
		fmt.Println("serving")
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt)
		go func() { <-c; os.Exit(0) }()
		for {
			if cn, err := l.Accept(); err == nil {
				cn.Close()
			}
		}
	case "write":
		_ = os.WriteFile(os.Getenv("HELPER_FILE"), []byte("built "+time.Now().String()), 0o644)
		fmt.Println("built ok password=hunter2hunter2")
	case "fail":
		fmt.Fprintln(os.Stderr, "compile error: boom")
		os.Exit(3)
	case "ok":
		fmt.Println("ok")
	case "args":
		for _, a := range os.Args[1:] {
			fmt.Println("ARG<" + a + ">")
		}
	case "sleep":
		time.Sleep(time.Hour)
	}
}

type env struct {
	m    *Manager
	st   *store.Store
	sup  *supervisor.Manager
	root string
}

func setup(t *testing.T) *env {
	t.Helper()
	db, _, err := database.Open(filepath.Join(t.TempDir(), "x.db"))
	require.NoError(t, err)
	st := store.New(db.DB)
	hub := logs.NewHub(1000, nil)
	sup := supervisor.NewManager(hub, nil)
	rec := NewRecorder(st, redact.New(), nil)
	m := NewManager(st, rec, sup, hub, t.TempDir(), "test")
	m.Tools = func(context.Context) []tools.Tool {
		return []tools.Tool{{ID: "node", Name: "Node.js", Installed: true, Version: "22.0.0", OnPath: true, Method: "PATH"}}
	}
	sup.OnEvent(m.HandleSupervisorEvent)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		sup.Shutdown(ctx)
		db.Close()
	})
	return &env{m: m, st: st, sup: sup, root: t.TempDir()}
}

func exe() string { return strings.ReplaceAll(os.Args[0], `\`, "/") }

func freePort(t *testing.T) int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func (e *env) workspace(t *testing.T, yaml string) *domain.Workspace {
	t.Helper()
	p, err := e.m.CreateProject("shop", e.root, "test project")
	require.NoError(t, err)
	plan, err := e.m.PlanWorkspace(WorkspaceOptions{Project: p.Name, Name: "auth", Task: "Implement auth", ConfigYAML: yaml})
	require.NoError(t, err)
	ws, err := e.m.CreateWorkspace(plan, "Implement auth")
	require.NoError(t, err)
	assert.Equal(t, domain.StateReady, ws.State)
	return ws
}

func TestFullExecutionLifecycleWithRealProcesses(t *testing.T) {
	e := setup(t)
	port := freePort(t)
	require.NoError(t, os.WriteFile(filepath.Join(e.root, "keep.txt"), []byte("unchanged"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(e.root, ".env"), []byte("SECRET=1"), 0o644))
	y := fmt.Sprintf(`version: 1
task: Implement auth
policy: {allow_adhoc: true}
start:
  - {name: build, command: '%[1]s', env: {EXEC_HELPER: write, HELPER_FILE: out.txt}}
services:
  - name: api
    command: '%[1]s'
    env: {EXEC_HELPER: serve, HELPER_PORT: '%[2]d'}
    stop_timeout_sec: 4
    healthcheck: {type: tcp, port: %[2]d, interval_sec: 1}
verify:
  name: acceptance
  checks:
    - {name: artifact, type: file, path: out.txt, contains: built}
    - {name: api-up, type: tcp, port: %[2]d, timeout_sec: 5}
    - {name: healthy, type: services_healthy, services: [api], timeout_sec: 10, needs: [api-up]}
`, exe(), port)
	ws := e.workspace(t, y)
	assert.False(t, ws.Approved)

	x, err := e.m.NewExecution(ws.ID, "")
	require.NoError(t, err)
	err = e.m.Start(context.Background(), x.ID)
	require.ErrorIs(t, err, ErrNotApproved, "nothing runs before approval")
	got, _ := e.st.GetExecution(x.ID)
	assert.Equal(t, domain.StateCreated, got.State, "refusal must not change state")

	require.NoError(t, e.m.ApproveWorkspace(ws.ID))
	require.NoError(t, e.m.Start(context.Background(), x.ID))
	got, _ = e.st.GetExecution(x.ID)
	assert.Equal(t, domain.StateRunning, got.State)
	w2, _ := e.m.Workspace(ws.ID)
	assert.Equal(t, domain.StateRunning, w2.State)
	// A second execution on the same workspace is rejected.
	_, err = e.m.NewExecution(ws.ID, "")
	assert.Error(t, err, "workspace is RUNNING, not READY")

	// The service is really listening and attributed to the execution.
	c, derr := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second)
	require.NoError(t, derr)
	c.Close()

	// Ad-hoc commands need approval and obey policy.
	_, err = e.m.RunAdHoc(context.Background(), x.ID, "probe", exe(), nil, false, 0)
	assert.ErrorContains(t, err, "approval")
	ad, err := e.m.RunAdHoc(context.Background(), x.ID, "probe", exe(), []string{"-test.run=NONE"}, true, 30*time.Second)
	require.NoError(t, err)
	require.NotNil(t, ad.ExitCode)

	results, sum, err := e.m.Verify(context.Background(), x.ID, nil)
	require.NoError(t, err)
	assert.Equal(t, "passed", sum.Overall, "%+v", results)
	require.Len(t, results, 3)

	got, _ = e.st.GetExecution(x.ID)
	assert.Equal(t, domain.StateCompleted, got.State)
	assert.Equal(t, "passed", got.Verification)
	w2, _ = e.m.Workspace(ws.ID)
	assert.Equal(t, domain.StateCompleted, w2.State)
	assert.False(t, e.m.IsActive(x.ID))

	// Cleanup: the service port is closed again.
	assert.Eventually(t, func() bool {
		c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 300*time.Millisecond)
		if err == nil {
			c.Close()
			return false
		}
		return true
	}, 8*time.Second, 200*time.Millisecond)

	d, err := e.m.Detail(x.ID)
	require.NoError(t, err)
	kinds := map[string]int{}
	for _, c := range d.Commands {
		kinds[c.Kind]++
		assert.NotNil(t, c.EndedAt, "command %s must be finished", c.Name)
		assert.NotContains(t, strings.Join(c.EnvKeys, ","), "hunter2")
	}
	assert.Equal(t, map[string]int{"start": 1, "service": 1, "adhoc": 1, "verify": 0}, map[string]int{"start": kinds["start"], "service": kinds["service"], "adhoc": kinds["adhoc"], "verify": kinds["verify"]})

	// Output was captured and the secret redacted before storage.
	var build *domain.Command
	for i := range d.Commands {
		if d.Commands[i].Name == "build" {
			build = &d.Commands[i]
		}
	}
	require.NotNil(t, build)
	lines, _ := e.st.ListOutput(build.ID, 0, 100)
	require.NotEmpty(t, lines)
	assert.Contains(t, lines[0].Line, "built ok")
	assert.NotContains(t, lines[0].Line, "hunter2hunter2")

	// File changes: out.txt added; .env never mentioned.
	got2 := map[string]string{}
	for _, c := range d.FileChanges {
		got2[c.Path] = c.Change
	}
	assert.Equal(t, "added", got2["out.txt"])
	assert.NotContains(t, got2, ".env")
	assert.NotContains(t, got2, "keep.txt")

	// Timeline contains the required evidence in order.
	evs, _ := e.st.ListEvents(x.ID, store.EventFilter{})
	var types []string
	for _, ev := range evs {
		types = append(types, ev.Type)
		assert.Equal(t, domain.SourceObserved, ev.Source)
	}
	joined := strings.Join(types, " ")
	for _, want := range []string{domain.EvExecutionCreated, domain.EvWorkspacePrepared, domain.EvCommandStarted, domain.EvProcessStarted, domain.EvCommandFinished, domain.EvServiceHealthy, domain.EvVerificationPassed, domain.EvFilesChanged, domain.EvExecutionCompleted} {
		assert.Contains(t, joined, want)
	}
	assert.Less(t, strings.Index(joined, domain.EvExecutionCreated), strings.Index(joined, domain.EvExecutionCompleted))
	var env Environment
	require.NoError(t, jsonUnmarshal(d.Environment, &env))
	assert.Equal(t, "22.0.0", env.Tools[0].Version)
	assert.NotContains(t, strings.Join(env.EnvKeys, ","), "SECRET")
	open, _ := e.st.OpenManagedProcesses()
	assert.Empty(t, open, "every managed process is closed")
}

func TestFailingStartCommandFailsExecutionAndStopsServices(t *testing.T) {
	e := setup(t)
	port := freePort(t)
	y := fmt.Sprintf("version: 1\nstart:\n  - {name: compile, command: '%[1]s', env: {EXEC_HELPER: fail}}\nservices:\n  - name: api\n    command: '%[1]s'\n    env: {EXEC_HELPER: serve, HELPER_PORT: '%[2]d'}\n", exe(), port)
	ws := e.workspace(t, y)
	require.NoError(t, e.m.ApproveWorkspace(ws.ID))
	x, _ := e.m.NewExecution(ws.ID, "")
	err := e.m.Start(context.Background(), x.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exit code 3")
	got, _ := e.st.GetExecution(x.ID)
	assert.Equal(t, domain.StateFailed, got.State)
	assert.Contains(t, got.Error, "compile")
	w, _ := e.m.Workspace(ws.ID)
	assert.Equal(t, domain.StateFailed, w.State)
	cmds, _ := e.st.ListCommands(x.ID)
	require.Len(t, cmds, 1, "services after the failing command never started")
	lines, _ := e.st.ListOutput(cmds[0].ID, 0, 10)
	require.NotEmpty(t, lines)
	assert.Equal(t, "stderr", lines[0].Stream)
	// Workspace can be reopened and run again.
	require.NoError(t, e.m.ReopenWorkspace(ws.ID))
	_, err = e.m.NewExecution(ws.ID, "retry")
	assert.NoError(t, err)
}

func TestVerificationFailureFailsExecutionAndSkipsDependents(t *testing.T) {
	e := setup(t)
	y := fmt.Sprintf("version: 1\nverify:\n  name: v\n  checks:\n    - {name: missing, type: file, path: nope.txt}\n    - {name: after, command: '%s', needs: [missing]}\n", exe())
	ws := e.workspace(t, y)
	require.NoError(t, e.m.ApproveWorkspace(ws.ID))
	x, _ := e.m.NewExecution(ws.ID, "")
	require.NoError(t, e.m.Start(context.Background(), x.ID))
	results, sum, err := e.m.Verify(context.Background(), x.ID, nil)
	require.NoError(t, err)
	assert.Equal(t, "failed", sum.Overall)
	assert.Equal(t, domain.VFailed, results[0].Status)
	assert.Equal(t, domain.VSkipped, results[1].Status)
	got, _ := e.st.GetExecution(x.ID)
	assert.Equal(t, domain.StateFailed, got.State)
	assert.Equal(t, "failed", got.Verification)
}

func TestCancelStopsRunningCommandAndService(t *testing.T) {
	e := setup(t)
	port := freePort(t)
	y := fmt.Sprintf("version: 1\npolicy: {allow_adhoc: true}\nservices:\n  - name: api\n    command: '%[1]s'\n    env: {EXEC_HELPER: serve, HELPER_PORT: '%[2]d'}\n    stop_timeout_sec: 3\n", exe(), port)
	ws := e.workspace(t, y)
	require.NoError(t, e.m.ApproveWorkspace(ws.ID))
	x, _ := e.m.NewExecution(ws.ID, "")
	require.NoError(t, e.m.Start(context.Background(), x.ID))
	done := make(chan error, 1)
	go func() {
		_, err := e.m.RunAdHoc(context.Background(), x.ID, "long", exe(), []string{"-test.run=NONE"}, true, 0)
		done <- err
	}()
	time.Sleep(200 * time.Millisecond)
	require.NoError(t, e.m.Cancel(x.ID, "user cancelled"))
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("ad-hoc command was not cancelled")
	}
	got, _ := e.st.GetExecution(x.ID)
	assert.Equal(t, domain.StateCancelled, got.State)
	assert.Eventually(t, func() bool {
		c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 300*time.Millisecond)
		if err == nil {
			c.Close()
			return false
		}
		return true
	}, 8*time.Second, 200*time.Millisecond)
	assert.Error(t, e.m.Cancel(x.ID, "again"), "terminal executions cannot be cancelled again")
	evs, _ := e.st.ListEvents(x.ID, store.EventFilter{Types: []string{domain.EvExecutionCancelled}})
	assert.Len(t, evs, 1)
}

func TestPolicyAndValidationBlockUnsafeConfigs(t *testing.T) {
	e := setup(t)
	p, _ := e.m.CreateProject("shop", e.root, "")
	for name, y := range map[string]string{
		"traversal": "version: 1\nstart:\n  - {name: a, command: x, dir: ../../Windows}\n",
		"denied":    "version: 1\npolicy: {deny: [powershell]}\nstart:\n  - {name: a, command: 'C:/Windows/System32/WindowsPowerShell/v1.0/powershell.exe'}\n",
		"unknown":   "version: 1\nbogus: true\n",
		"docker":    "version: 1\nisolation: docker\n",
		"cycle":     "version: 1\nservices:\n  - {name: a, command: x, depends_on: [b]}\n  - {name: b, command: x, depends_on: [a]}\n",
		"version":   "version: 9\n",
	} {
		_, err := e.m.PlanWorkspace(WorkspaceOptions{Project: p.Name, Name: "w", ConfigYAML: y})
		assert.Error(t, err, name)
	}
	ws := e.workspace2(t, p, "ok", "version: 1\nstart:\n  - {name: a, command: x}\n")
	x, _ := e.m.NewExecution(ws.ID, "")
	require.NoError(t, e.m.ApproveWorkspace(ws.ID))
	require.NoError(t, e.m.ReconfigureWorkspace(ws.ID, "version: 1\nstart:\n  - {name: a, command: y}\n"))
	err := e.m.Start(context.Background(), x.ID)
	assert.ErrorIs(t, err, ErrNotApproved, "editing the configuration revokes approval")
}

func (e *env) workspace2(t *testing.T, p *domain.Project, name, yaml string) *domain.Workspace {
	plan, err := e.m.PlanWorkspace(WorkspaceOptions{Project: p.Name, Name: name, ConfigYAML: yaml})
	require.NoError(t, err)
	ws, err := e.m.CreateWorkspace(plan, "")
	require.NoError(t, err)
	return ws
}

func TestRecoveryMarksInterruptedAndOrphanCleanupIsSafe(t *testing.T) {
	e := setup(t)
	ws := e.workspace(t, "version: 1\n")
	x, _ := e.m.NewExecution(ws.ID, "")
	require.NoError(t, e.st.TransitionExecution(x.ID, domain.StateConfiguring))
	require.NoError(t, e.st.TransitionExecution(x.ID, domain.StateReady))
	require.NoError(t, e.st.TransitionExecution(x.ID, domain.StateRunning))
	require.NoError(t, e.st.TransitionWorkspace(ws.ID, domain.StateRunning, ""))

	// A real process RUNTIDE "launched" before it crashed, and an unrelated one with a wrong start time.
	mine := exec.Command(os.Args[0], "-test.run=NONE")
	mine.Env = append(os.Environ(), "EXEC_HELPER=sleep")
	require.NoError(t, mine.Start())
	defer mine.Process.Kill()
	other := exec.Command(os.Args[0], "-test.run=NONE")
	other.Env = append(os.Environ(), "EXEC_HELPER=sleep")
	require.NoError(t, other.Start())
	defer other.Process.Kill()
	start := func(pid int) int64 {
		ps, _ := winapi.ListProcesses(false)
		for _, p := range ps {
			if int(p.PID) == pid {
				return p.StartTime.UnixNano()
			}
		}
		return 0
	}
	_, err := e.st.AddManagedProcess(store.ManagedProcess{ExecutionID: x.ID, CommandID: "cm_1", PID: mine.Process.Pid, StartTimeNs: start(mine.Process.Pid), Exe: "mine"})
	require.NoError(t, err)
	_, err = e.st.AddManagedProcess(store.ManagedProcess{ExecutionID: x.ID, CommandID: "cm_2", PID: other.Process.Pid, StartTimeNs: start(other.Process.Pid) - int64(time.Hour), Exe: "reused-pid"})
	require.NoError(t, err)
	_, err = e.st.AddManagedProcess(store.ManagedProcess{ExecutionID: x.ID, CommandID: "cm_3", PID: 0x7ffffff0, StartTimeNs: 1, Exe: "gone"})
	require.NoError(t, err)

	// "Restart": a brand-new manager over the same database.
	m2 := NewManager(e.st, NewRecorder(e.st, nil, nil), e.sup, e.m.Hub, e.m.Home, "test")
	ids, err := m2.Recover()
	require.NoError(t, err)
	assert.Equal(t, []string{x.ID}, ids)
	got, _ := e.st.GetExecution(x.ID)
	assert.Equal(t, domain.StateInterrupted, got.State)
	w, _ := e.m.Workspace(ws.ID)
	assert.Equal(t, domain.StateInterrupted, w.State)

	orphans, err := m2.Orphans()
	require.NoError(t, err)
	require.Len(t, orphans, 2, "the dead process's record is closed silently")
	verified := map[string]bool{}
	for _, o := range orphans {
		verified[o.Exe] = o.Verified
	}
	assert.True(t, verified["mine"])
	assert.False(t, verified["reused-pid"])

	killed, skipped, err := m2.CleanupOrphans(x.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, killed)
	assert.Equal(t, 1, skipped)
	assert.Eventually(t, func() bool { return !winapi.IsRunning(uint32(mine.Process.Pid)) }, 5*time.Second, 50*time.Millisecond)
	assert.True(t, winapi.IsRunning(uint32(other.Process.Pid)), "a process with a mismatching start time is never terminated")
}

func TestWorktreeWorkspaceFromGitRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	e := setup(t)
	run := func(args ...string) {
		out, err := exec.Command("git", append([]string{"-C", e.root}, args...)...).CombinedOutput()
		require.NoError(t, err, string(out))
	}
	run("init", "-q")
	run("config", "user.email", "t@example.com")
	run("config", "user.name", "t")
	require.NoError(t, os.WriteFile(filepath.Join(e.root, "main.go"), []byte("package main\n"), 0o644))
	run("add", "main.go")
	run("commit", "-q", "-m", "init")
	branch, _ := exec.Command("git", "-C", e.root, "rev-parse", "--abbrev-ref", "HEAD").Output()

	p, err := e.m.CreateProject("shop", e.root, "")
	require.NoError(t, err)
	assert.NotEmpty(t, p.GitRoot)

	// Dirty trees are detected and block creation until acknowledged.
	require.NoError(t, os.WriteFile(filepath.Join(e.root, "main.go"), []byte("package main\n// wip\n"), 0o644))
	plan, err := e.m.PlanWorkspace(WorkspaceOptions{Project: "shop", Name: "iso", Isolation: "worktree"})
	require.NoError(t, err)
	assert.NotEmpty(t, plan.Blockers)
	assert.NotEmpty(t, plan.Warnings)
	_, err = e.m.CreateWorkspace(plan, "")
	require.Error(t, err)
	plan, err = e.m.PlanWorkspace(WorkspaceOptions{Project: "shop", Name: "iso", Isolation: "worktree", AcceptDirty: true})
	require.NoError(t, err)
	ws, err := e.m.CreateWorkspace(plan, "")
	require.NoError(t, err)
	assert.True(t, ws.Worktree)
	assert.NotEqual(t, e.root, ws.Dir)
	b, err := os.ReadFile(filepath.Join(ws.Dir, "main.go"))
	require.NoError(t, err)
	assert.Equal(t, "package main\n", strings.ReplaceAll(string(b), "\r\n", "\n"), "worktree holds the committed content, not the uncommitted edit")
	wip, _ := os.ReadFile(filepath.Join(e.root, "main.go"))
	assert.Contains(t, string(wip), "wip", "the user's uncommitted work is untouched")
	after, _ := exec.Command("git", "-C", e.root, "rev-parse", "--abbrev-ref", "HEAD").Output()
	assert.Equal(t, string(branch), string(after), "the user's branch is unchanged")

	// Removal: refuses a dirty worktree, removes a clean one, never touches the project.
	require.NoError(t, os.WriteFile(filepath.Join(ws.Dir, "new.go"), []byte("x"), 0o644))
	assert.Error(t, e.m.RemoveWorkspaceDir(ws.ID, false))
	require.NoError(t, e.m.RemoveWorkspaceDir(ws.ID, true))
	_, err = os.Stat(ws.Dir)
	assert.True(t, os.IsNotExist(err))
	_, err = os.Stat(filepath.Join(e.root, "main.go"))
	assert.NoError(t, err)
}

func TestRemoveRefusesPathsOutsideWorktreeRoot(t *testing.T) {
	e := setup(t)
	p, _ := e.m.CreateProject("shop", e.root, "")
	ws := e.workspace2(t, p, "m", "version: 1\n")
	assert.ErrorContains(t, e.m.RemoveWorkspaceDir(ws.ID, true), "does not own a directory")
	_, err := e.st.DB.Exec(`UPDATE task_workspaces SET worktree=1 WHERE id=?`, ws.ID)
	require.NoError(t, err)
	err = e.m.RemoveWorkspaceDir(ws.ID, true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "refusing")
	_, statErr := os.Stat(e.root)
	assert.NoError(t, statErr, "the project directory must survive")
}

func TestOutputIsBounded(t *testing.T) {
	e := setup(t)
	e.m.Rec.Limits = Limits{MaxCommandOutputBytes: 1000, MaxCommandLines: 50}
	ws := e.workspace(t, "version: 1\n")
	x, _ := e.m.NewExecution(ws.ID, "")
	cr, err := e.m.Rec.StartCommand(&domain.Command{ExecutionID: x.ID, Kind: "adhoc", Name: "noisy", Executable: "x", Cwd: "c", Isolation: 1})
	require.NoError(t, err)
	for i := 0; i < 5000; i++ {
		cr.Line("stdout", strings.Repeat("x", 50))
	}
	cr.Finish(0, false, false, nil)
	c, _ := e.st.GetCommand(cr.Cmd.ID)
	assert.True(t, c.Truncated)
	assert.Equal(t, int64(5000*51), c.StdoutBytes, "bytes are counted even when not stored")
	lines, _ := e.st.ListOutput(c.ID, 0, 10000)
	assert.LessOrEqual(t, len(lines), 50)
	assert.NotEmpty(t, cr.Tail())
}
