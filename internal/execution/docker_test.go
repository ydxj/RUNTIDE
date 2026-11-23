package execution

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/runtide/runtide/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// dockerReady reports whether the Docker engine answers.
func dockerReady() bool {
	if _, err := exec.LookPath("docker"); err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	// `docker info` can exit 0 without a server, so require a real server version.
	out, err := exec.CommandContext(ctx, "docker", "version", "--format", "{{.Server.Version}}").Output()
	return err == nil && strings.TrimSpace(string(out)) != ""
}

func TestDockerIsolationNeverFallsBackToNativeExecution(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker CLI not installed")
	}
	e := setup(t)
	// An image that cannot exist (and cannot be pulled): whether or not the
	// engine is running, the execution must fail instead of running natively.
	y := `version: 1
task: Missing image
isolation: docker
docker: {image: "runtide-test/no-such-image:never"}
start:
  - {name: probe, command: cmd, args: ['/c', 'echo native > native-ran.txt']}
`
	ws := e.workspace(t, y)
	require.NoError(t, e.m.ApproveWorkspace(ws.ID))
	x, err := e.m.NewExecution(ws.ID, "")
	require.NoError(t, err)
	assert.Error(t, e.m.Start(context.Background(), x.ID))
	_, statErr := os.Stat(filepath.Join(e.root, "native-ran.txt"))
	assert.True(t, os.IsNotExist(statErr), "the command must not have run natively")
	got, _ := e.st.GetExecution(x.ID)
	assert.Equal(t, domain.StateFailed, got.State)
}

// localImage returns a small image that is already present, so the test never
// downloads anything.
func localImage(t *testing.T) string {
	t.Helper()
	for _, img := range []string{"busybox", "alpine", "busybox:latest", "alpine:latest"} {
		if exec.Command("docker", "image", "inspect", img).Run() == nil {
			return img
		}
	}
	return ""
}

func TestDockerIsolationRunsCommandsInsideAContainer(t *testing.T) {
	if !dockerReady() {
		t.Skip("Docker engine is not running; container execution was NOT tested")
	}
	img := localImage(t)
	if img == "" {
		t.Skip("no local busybox/alpine image; not pulling images during tests")
	}
	e := setup(t)
	y := `version: 1
task: Run in a container
isolation: docker
docker: {image: ` + img + `, network: none, memory: 256m}
start:
  - {name: probe, command: sh, args: ['-c', 'cat /etc/os-release | head -1 > os.txt; echo inside-container; ls /workspace > /dev/null']}
verify:
  name: acceptance
  checks:
    - {name: os-file, type: file, path: os.txt, min_size: 1}
`
	ws := e.workspace(t, y)
	assert.Equal(t, domain.IsolationDocker, ws.Isolation)
	require.NoError(t, e.m.ApproveWorkspace(ws.ID))
	x, err := e.m.NewExecution(ws.ID, "")
	require.NoError(t, err)
	require.NoError(t, e.m.Start(context.Background(), x.ID))
	_, sum, err := e.m.Verify(context.Background(), x.ID, nil)
	require.NoError(t, err)
	assert.Equal(t, "passed", sum.Overall)
	b, err := os.ReadFile(filepath.Join(e.root, "os.txt"))
	require.NoError(t, err)
	assert.NotContains(t, strings.ToLower(string(b)), "windows", "written by the container's Linux userland")
}
