package diagnostics

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/runtide/runtide/internal/config"
	"github.com/runtide/runtide/internal/docker"
	"github.com/runtide/runtide/internal/health"
	"github.com/runtide/runtide/internal/ports"
	"github.com/runtide/runtide/internal/supervisor"
	"github.com/runtide/runtide/internal/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func find(r Report, prefix string) *Finding {
	for i := range r.Findings {
		if len(r.Findings[i].ID) >= len(prefix) && r.Findings[i].ID[:len(prefix)] == prefix {
			return &r.Findings[i]
		}
	}
	return nil
}

func target(root string, s config.ServiceSpec) Target {
	return Target{Workspace: "ws", Root: root, Spec: s, Key: "ws/" + s.Name}
}

func TestStructuralRules(t *testing.T) {
	root := t.TempDir()
	in := Inputs{
		Roots: []string{root},
		Targets: []Target{target(root, config.ServiceSpec{
			Name: "web", Type: "node", Command: "definitely-missing-binary-xyz", WorkingDirectory: "nope",
		})},
		Tools: []tools.Tool{{ID: "node", Installed: false}},
	}
	r := Run(context.Background(), in)
	f := find(r, "workdir-missing")
	require.NotNil(t, f)
	assert.True(t, f.FixAvailable)
	assert.NotNil(t, find(r, "exe-missing"))
	assert.NotNil(t, find(r, "runtime-missing"))
	assert.Greater(t, r.Errors, 0)
	// Every finding carries the mandatory fields.
	for _, x := range r.Findings {
		assert.NotEmpty(t, x.Title)
		assert.NotEmpty(t, x.Severity)
		assert.NotEmpty(t, x.Description)
		assert.NotEmpty(t, x.Component)
		assert.NotEmpty(t, x.Resolution)
	}
}

func TestPortConflictUsesRealOwnerInfo(t *testing.T) {
	root := t.TempDir()
	spec := config.ServiceSpec{Name: "api", Command: "cmd", Health: &config.HealthSpec{Type: "tcp", Port: 5000}}
	in := Inputs{
		Roots:     []string{root},
		Targets:   []Target{target(root, spec)},
		Listeners: []ports.Listener{{Protocol: "tcp", Port: 5000, PID: 42, ProcessName: "other.exe", Address: "0.0.0.0"}},
	}
	f := find(Run(context.Background(), in), "port-conflict")
	require.NotNil(t, f)
	assert.Contains(t, f.Evidence[0], "other.exe")
	// The service's own listener is not a conflict.
	in.Listeners[0].Managed = "ws/api"
	assert.Nil(t, find(Run(context.Background(), in), "port-conflict"))
}

func TestEnvAndDatabaseRules(t *testing.T) {
	root := t.TempDir()
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	port := l.Addr().(*net.TCPAddr).Port
	l.Close() // guaranteed closed port
	require.NoError(t, os.WriteFile(filepath.Join(root, ".env.example"), []byte("DB_HOST=\nDB_PORT=\nAPP_NAME=\n"), 0o600))
	in := Inputs{Roots: []string{root}, Targets: []Target{target(root, config.ServiceSpec{Name: "app", Command: "cmd"})}}
	r := Run(context.Background(), in)
	f := find(r, "env-missing")
	require.NotNil(t, f)
	require.True(t, f.FixAvailable)

	// Preview then apply the fix; ensure it never overwrites and stays inside roots.
	plan, err := PlanFix(f.FixID, in.Roots)
	require.NoError(t, err)
	assert.NotEmpty(t, plan.Changes)
	_, err = ApplyFix(f.FixID, in.Roots)
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(root, ".env"))
	assert.NoError(t, err)
	_, err = PlanFix(f.FixID, in.Roots)
	assert.Error(t, err, "second application must refuse to overwrite")
	_, err = PlanFix("mkdir:"+filepath.Join(os.TempDir(), "outside"), in.Roots)
	assert.Error(t, err)

	require.NoError(t, os.WriteFile(filepath.Join(root, ".env"), []byte("DB_HOST=127.0.0.1\nDB_PORT="+strconv.Itoa(port)+"\n"), 0o600))
	r = Run(context.Background(), in)
	assert.NotNil(t, find(r, "db-unreachable"))
	assert.NotNil(t, find(r, "env-vars-missing"))
}

func TestFailedAndUnhealthy(t *testing.T) {
	root := t.TempDir()
	code := 2
	in := Inputs{
		Roots:   []string{root},
		Targets: []Target{target(root, config.ServiceSpec{Name: "a", Command: "cmd"}), target(root, config.ServiceSpec{Name: "b", Command: "cmd", Health: &config.HealthSpec{Type: "tcp", Port: 9}})},
		Runtime: map[string]supervisor.Info{
			"ws/a": {State: supervisor.StateFailed, LastError: "exited with code 2", ExitCode: &code, TotalRestart: 3},
			"ws/b": {State: supervisor.StateRunning, Health: health.Unhealthy, HealthDetail: "connection refused"},
		},
		LastLogs: func(string, int) []string { return []string{"stack trace line"} },
		Docker:   docker.Status{Message: "none"},
	}
	r := Run(context.Background(), in)
	f := find(r, "service-failed")
	require.NotNil(t, f)
	assert.Contains(t, f.Evidence, "stack trace line")
	assert.NotNil(t, find(r, "restart-loop"))
	assert.NotNil(t, find(r, "unhealthy"))
	assert.NotNil(t, find(r, "docker-absent"))
}

func TestHealthCheckRemoteHostFlagged(t *testing.T) {
	root := t.TempDir()
	in := Inputs{Roots: []string{root}, Targets: []Target{target(root, config.ServiceSpec{Name: "a", Command: "cmd", Health: &config.HealthSpec{Type: "http", URL: "http://example.com"}})}}
	assert.NotNil(t, find(Run(context.Background(), in), "health-invalid"))
}
