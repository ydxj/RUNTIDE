package supervisor

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/runtide/runtide/internal/config"
	"github.com/runtide/runtide/internal/health"
	"github.com/runtide/runtide/internal/logs"
	winapi "github.com/runtide/runtide/internal/platform/windows"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMain doubles as the test helper program: when RUNTIDE_HELPER is set the
// binary behaves as a tiny fake dev server instead of running tests.
func TestMain(m *testing.M) {
	switch os.Getenv("RUNTIDE_HELPER") {
	case "":
		os.Exit(m.Run())
	case "sleep":
		fmt.Println("hello stdout")
		fmt.Fprintln(os.Stderr, "hello stderr ERROR")
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt)
		<-c
		fmt.Println("graceful shutdown")
		os.Exit(0)
	case "stubborn":
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt) // swallow the interrupt, never exit
		for range c {
		}
	case "crash":
		fmt.Fprintln(os.Stderr, "boom")
		os.Exit(3)
	case "serve":
		l, err := net.Listen("tcp", "127.0.0.1:"+os.Getenv("HELPER_PORT"))
		if err != nil {
			os.Exit(4)
		}
		for {
			c, err := l.Accept()
			if err == nil {
				c.Close()
			}
		}
	case "parent":
		child := exec.Command(os.Args[0])
		child.Env = append(os.Environ(), "RUNTIDE_HELPER=stubborn")
		_ = child.Start()
		fmt.Println("child pid", child.Process.Pid)
		time.Sleep(time.Hour)
	}
}

func helper(mode string, extra ...string) config.ServiceSpec {
	env := map[string]string{"RUNTIDE_HELPER": mode}
	for _, e := range extra {
		k, v, _ := strings.Cut(e, "=")
		env[k] = v
	}
	return config.ServiceSpec{Name: "t", Command: os.Args[0], Env: env, StopTimeoutSec: 3}
}

func newMgr(t *testing.T) (*Manager, *logs.Hub) {
	h := logs.NewHub(1000, nil)
	m := NewManager(h, nil)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		m.Shutdown(ctx)
	})
	return m, h
}

func waitState(t *testing.T, m *Manager, key string, want State, d time.Duration) Info {
	t.Helper()
	var inf Info
	require.Eventually(t, func() bool { inf, _ = m.Get(key); return inf.State == want }, d, 50*time.Millisecond, "want %s, last %+v", want, inf)
	return inf
}

func TestStartCaptureAndGracefulStop(t *testing.T) {
	m, h := newMgr(t)
	require.NoError(t, m.Start("svc", helper("sleep"), ""))
	require.NoError(t, m.WaitReady(context.Background(), "svc", 10*time.Second, 500*time.Millisecond))
	require.Eventually(t, func() bool { return len(h.Recent(0, "svc")) >= 3 }, 5*time.Second, 50*time.Millisecond)
	var out, errl string
	for _, e := range h.Recent(0, "svc") {
		if e.Stream == "stdout" {
			out += e.Line + "\n"
		}
		if e.Stream == "stderr" {
			errl += e.Line + "\n"
			assert.Equal(t, "error", e.Level)
		}
	}
	assert.Contains(t, out, "hello stdout")
	assert.Contains(t, errl, "hello stderr")

	start := time.Now()
	require.NoError(t, m.Stop("svc"))
	inf, _ := m.Get("svc")
	assert.Equal(t, StateStopped, inf.State)
	assert.Less(t, time.Since(start), 3*time.Second, "graceful interrupt should beat the 3s timeout")
	require.NotNil(t, inf.ExitCode)
	assert.Equal(t, 0, *inf.ExitCode, "helper exits 0 only when it received the interrupt")
	assert.False(t, winapi.IsRunning(uint32(inf.PID+0)) && inf.PID != 0)
}

func TestForceKillWhenStubbornAndTreeCleanup(t *testing.T) {
	m, h := newMgr(t)
	require.NoError(t, m.Start("tree", helper("parent"), ""))
	require.Eventually(t, func() bool {
		for _, e := range h.Recent(0, "tree") {
			if strings.HasPrefix(e.Line, "child pid") {
				return true
			}
		}
		return false
	}, 10*time.Second, 50*time.Millisecond)
	var childPID uint32
	for _, e := range h.Recent(0, "tree") {
		fmt.Sscanf(e.Line, "child pid %d", &childPID)
	}
	require.NotZero(t, childPID)
	require.True(t, winapi.IsRunning(childPID))
	require.NoError(t, m.Stop("tree"))
	require.Eventually(t, func() bool { return !winapi.IsRunning(childPID) }, 5*time.Second, 50*time.Millisecond, "grandchild must die with the job")
}

func TestCrashNoRestartByDefault(t *testing.T) {
	m, _ := newMgr(t)
	require.NoError(t, m.Start("c", helper("crash"), ""))
	inf := waitState(t, m, "c", StateFailed, 10*time.Second)
	require.NotNil(t, inf.ExitCode)
	assert.Equal(t, 3, *inf.ExitCode)
	assert.Equal(t, 0, inf.TotalRestart)
}

func TestRestartPolicyBackoffAndGiveUp(t *testing.T) {
	m, h := newMgr(t)
	spec := helper("crash")
	spec.Restart = config.RestartSpec{Policy: "on-failure", MaxRetries: 2, BackoffMs: 100, MaxBackoffMs: 200}
	var mu sync.Mutex
	var kinds []string
	m.OnEvent(func(e Event) { mu.Lock(); kinds = append(kinds, e.Kind); mu.Unlock() })
	require.NoError(t, m.Start("r", spec, ""))
	inf := waitState(t, m, "r", StateFailed, 20*time.Second)
	assert.Equal(t, 2, inf.TotalRestart, "must stop after exactly MaxRetries restarts, not loop forever")
	assert.Contains(t, inf.LastError, "gave up")
	mu.Lock()
	assert.Contains(t, kinds, "gave-up")
	mu.Unlock()
	var starts int
	for _, e := range h.Recent(0, "r") {
		if strings.HasPrefix(e.Line, "started ") {
			starts++
		}
	}
	assert.Equal(t, 3, starts)
}

func TestHealthAndPortAndReadiness(t *testing.T) {
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	m, _ := newMgr(t)
	spec := helper("serve", fmt.Sprintf("HELPER_PORT=%d", port))
	spec.Health = &config.HealthSpec{Type: "tcp", Port: port, IntervalSec: 1, TimeoutSec: 1, FailureThreshold: 1}
	require.NoError(t, m.Start("web", spec, ""))
	require.NoError(t, m.WaitReady(context.Background(), "web", 15*time.Second, 0))
	inf, _ := m.Get("web")
	assert.Equal(t, health.Healthy, inf.Health)
	// And the port really is owned by the managed pid.
	pids := m.ManagedPIDs()
	assert.Equal(t, "web", pids[uint32(inf.PID)])
	require.NoError(t, m.Stop("web"))
	inf, _ = m.Get("web")
	assert.Equal(t, health.Unknown, inf.Health)
}

func TestWaitReadyFailsFastOnCrash(t *testing.T) {
	m, _ := newMgr(t)
	require.NoError(t, m.Start("x", helper("crash"), ""))
	err := m.WaitReady(context.Background(), "x", 10*time.Second, time.Second)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed")
}

func TestStartErrors(t *testing.T) {
	m, _ := newMgr(t)
	err := m.Start("bad", config.ServiceSpec{Name: "bad", Command: "definitely-missing-binary-xyz"}, "")
	require.Error(t, err)
	inf, _ := m.Get("bad")
	assert.Equal(t, StateFailed, inf.State)
	// Double start is rejected.
	require.NoError(t, m.Start("d", helper("sleep"), ""))
	assert.Error(t, m.Start("d", helper("sleep"), ""))
	// Invalid spec rejected before anything launches.
	assert.Error(t, m.Start("e", config.ServiceSpec{Name: "e", Command: ""}, ""))
}

func TestBackoffCalculation(t *testing.T) {
	r := config.RestartSpec{BackoffMs: 100, MaxBackoffMs: 450}
	assert.Equal(t, 100*time.Millisecond, backoff(r, 0))
	assert.Equal(t, 200*time.Millisecond, backoff(r, 1))
	assert.Equal(t, 400*time.Millisecond, backoff(r, 2))
	assert.Equal(t, 450*time.Millisecond, backoff(r, 3))
}
