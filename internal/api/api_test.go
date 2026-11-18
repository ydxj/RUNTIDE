package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"testing"
	"time"

	"github.com/runtide/runtide/internal/app"
	"github.com/runtide/runtide/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMain doubles as a fake dev server: it listens on HELPER_PORT, prints to
// stdout/stderr, and exits cleanly on interrupt.
func TestMain(m *testing.M) {
	if os.Getenv("RUNTIDE_HELPER") == "serve" {
		l, err := net.Listen("tcp", "127.0.0.1:"+os.Getenv("HELPER_PORT"))
		if err != nil {
			os.Exit(4)
		}
		fmt.Println("listening for requests")
		fmt.Fprintln(os.Stderr, "warning: using insecure token=abc123def456")
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt)
		go func() { <-c; os.Exit(0) }()
		for {
			if cn, err := l.Accept(); err == nil {
				cn.Close()
			}
		}
	}
	os.Exit(m.Run())
}

func newEngine(t *testing.T) *app.Engine {
	t.Helper()
	e, err := app.New(app.Options{Home: t.TempDir()})
	require.NoError(t, err)
	t.Cleanup(func() { e.Close(true) })
	return e
}

func call[T any](t *testing.T, c *Client, method string, params any) T {
	t.Helper()
	var out T
	require.NoError(t, c.Call(method, params, &out), method)
	return out
}

func TestFullServiceLifecycleOverAPI(t *testing.T) {
	e := newEngine(t)
	srv, err := Serve(e, "daemon")
	require.NoError(t, err)
	defer srv.Close()

	c, err := Connect(e.Home)
	require.NoError(t, err)

	// A second engine for the same home is refused.
	e2, err := app.New(app.Options{Home: t.TempDir()})
	require.NoError(t, err)
	defer e2.Close(true)

	l, _ := net.Listen("tcp", "127.0.0.1:0")
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()

	spec := config.ServiceSpec{Name: "demo", Type: "custom", Command: os.Args[0], WorkingDirectory: os.TempDir(),
		Env:    map[string]string{"RUNTIDE_HELPER": "serve", "HELPER_PORT": fmt.Sprint(port)},
		Health: &config.HealthSpec{Type: "tcp", Port: port, IntervalSec: 1}, StopTimeoutSec: 4}
	require.NoError(t, c.Call("services.save", spec, nil))

	require.NoError(t, c.Call("services.start", map[string]string{"key": "demo"}, nil))
	require.Eventually(t, func() bool {
		svcs := call[[]app.ServiceView](t, c, "services.list", nil)
		return len(svcs) == 1 && svcs[0].Runtime != nil && svcs[0].Runtime.Health == "healthy"
	}, 15*time.Second, 200*time.Millisecond, "health check must actually pass")

	// Port manager sees the real listener and attributes it to the service.
	type L struct {
		Port    uint16 `json:"port"`
		Managed string `json:"managed"`
		PID     uint32 `json:"pid"`
	}
	ls := call[[]L](t, c, "ports.list", map[string]any{"query": fmt.Sprint(port)})
	require.NotEmpty(t, ls)
	assert.Equal(t, "demo", ls[0].Managed)

	// Logs are captured, redacted and persisted.
	var lines []struct{ Line, Stream string }
	require.Eventually(t, func() bool {
		lines = call[[]struct{ Line, Stream string }](t, c, "logs.recent", map[string]any{"services": []string{"demo"}})
		return len(lines) >= 3
	}, 5*time.Second, 100*time.Millisecond)
	joined := ""
	for _, l := range lines {
		joined += l.Line + "\n"
	}
	assert.Contains(t, joined, "listening for requests")
	assert.NotContains(t, joined, "abc123def456", "secrets must be redacted before storage")

	// Overview and doctor use real data.
	ov := call[app.Overview](t, c, "overview", nil)
	assert.Greater(t, ov.Processes, 10)
	assert.Equal(t, 1, ov.Services.Running)
	rep := call[map[string]any](t, c, "doctor.run", nil)
	assert.NotNil(t, rep["findings"])

	// Stop is verified and idempotent.
	require.NoError(t, c.Call("services.stop", map[string]string{"key": "demo"}, nil))
	svcs := call[[]app.ServiceView](t, c, "services.list", nil)
	assert.Equal(t, "stopped", string(svcs[0].Runtime.State))
	require.NoError(t, c.Call("services.stop", map[string]string{"key": "demo"}, nil))
	runs := call[[]struct {
		ExitCode *int `json:"exitCode"`
	}](t, c, "services.runs", map[string]string{"key": "demo"})
	require.Len(t, runs, 1)
	require.NotNil(t, runs[0].ExitCode)

	require.NoError(t, c.Call("services.delete", map[string]string{"key": "demo"}, nil))
	assert.Empty(t, call[[]app.ServiceView](t, c, "services.list", nil))
}

func TestAPISecurity(t *testing.T) {
	e := newEngine(t)
	srv, err := Serve(e, "desktop")
	require.NoError(t, err)
	defer srv.Close()
	c, err := Connect(e.Home)
	require.NoError(t, err)
	_, err = Serve(e, "daemon")
	assert.ErrorIs(t, err, ErrAlreadyRunning)

	url := "http://" + c.Info.Addr + "/rpc"
	body := []byte(`{"method":"version"}`)
	do := func(mod func(*http.Request)) int {
		req, _ := http.NewRequest("POST", url, bytes.NewReader(body))
		mod(req)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		resp.Body.Close()
		return resp.StatusCode
	}
	assert.Equal(t, 401, do(func(r *http.Request) {}), "no token")
	assert.Equal(t, 401, do(func(r *http.Request) { r.Header.Set("Authorization", "Bearer wrong") }))
	assert.Equal(t, 403, do(func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer "+c.Info.Token)
		r.Header.Set("Origin", "http://evil.example")
	}), "browser origins rejected")
	assert.Equal(t, 403, do(func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer "+c.Info.Token)
		r.Host = "evil.example"
	}), "DNS rebinding host rejected")
	assert.Equal(t, 200, do(func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+c.Info.Token) }))
	assert.True(t, strings.HasPrefix(c.Info.Addr, "127.0.0.1:"), "must bind loopback only")

	// Unknown parameters and methods are rejected.
	assert.Error(t, c.Call("nope", nil, nil))
	assert.Error(t, c.Call("processes.list", map[string]any{"bogus": 1}, nil))
}

func TestSettingsDatabaseAndDestructiveConfirmation(t *testing.T) {
	e := newEngine(t)
	ctx := context.Background()
	d := func(m string, p any) (any, error) {
		b, _ := json.Marshal(p)
		return e.Dispatch(ctx, m, b)
	}
	_, err := d("settings.set", map[string]any{"refreshSec": 0})
	assert.Error(t, err)
	_, err = d("settings.set", map[string]any{"extraRedaction": []string{"("}})
	assert.Error(t, err)
	_, err = d("settings.set", map[string]any{"theme": "dark", "refreshSec": 5})
	require.NoError(t, err)
	assert.Equal(t, "dark", e.Settings().Theme)

	_, err = d("db.reset", map[string]any{})
	assert.ErrorContains(t, err, "confirmation")
	_, err = d("processes.kill", map[string]any{"pid": os.Getpid()})
	assert.Error(t, err, "own process is protected")
	_, err = d("db.backup", nil)
	require.NoError(t, err)
	_, err = d("db.reset", map[string]any{"confirm": true})
	require.NoError(t, err)
	assert.Equal(t, "light", e.Settings().Theme)
	_, err = d("winservices.control", map[string]any{"name": "Spooler", "action": "stop"})
	assert.ErrorContains(t, err, "confirmation")
}

func TestWorkspaceApprovalViaDispatch(t *testing.T) {
	e := newEngine(t)
	root := t.TempDir()
	y := "name: demo\nservices:\n  - {name: a, command: 'cmd.exe', args: ['/c', 'echo hi']}\n"
	b, _ := json.Marshal(map[string]any{"yaml": y, "root": root})
	_, err := e.Dispatch(context.Background(), "workspaces.save", b)
	require.NoError(t, err)
	up, _ := json.Marshal(map[string]any{"name": "demo"})
	_, err = e.Dispatch(context.Background(), "workspaces.up", up)
	assert.ErrorContains(t, err, "not been approved")
	// A workspace service cannot be started individually without approval either.
	k, _ := json.Marshal(map[string]any{"key": "demo/a"})
	_, err = e.Dispatch(context.Background(), "services.start", k)
	assert.ErrorContains(t, err, "not been approved")
}
