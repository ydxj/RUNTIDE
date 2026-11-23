package execution

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/runtide/runtide/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// wslDistro returns a running-capable WSL2 distribution or skips the test.
// A skip is the honest outcome on machines without WSL2: nothing is faked.
func wslDistro(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("wsl.exe", "-l", "-q").Output()
	if err != nil {
		t.Skip("WSL is not available: " + err.Error())
	}
	// wsl.exe prints UTF-16LE.
	u := make([]uint16, 0, len(out)/2)
	for i := 0; i+1 < len(out); i += 2 {
		u = append(u, uint16(out[i])|uint16(out[i+1])<<8)
	}
	for _, l := range strings.Split(string(utf16.Decode(u)), "\n") {
		name := strings.TrimSpace(strings.Trim(l, "\x00\r"))
		if name == "" || strings.HasPrefix(name, "docker-desktop") {
			continue
		}
		var b bytes.Buffer
		c := exec.Command("wsl.exe", "-d", name, "--exec", "true")
		c.Stderr = &b
		if c.Run() == nil {
			return name
		}
	}
	t.Skip("no usable WSL2 distribution installed")
	return ""
}

func TestWSLIsolationRunsCommandsInsideTheDistribution(t *testing.T) {
	distro := wslDistro(t)
	e := setup(t)
	y := `version: 1
task: Run inside WSL
isolation: wsl
wsl: {distro: ` + distro + `}
start:
  - {name: probe, command: sh, args: ['-c', 'uname -s > kernel.txt; echo "inside-wsl user=$(id -un)"']}
verify:
  name: acceptance
  checks:
    - {name: kernel-file, type: file, path: kernel.txt, contains: Linux}
    - {name: linux-command, command: uname, args: [-s], expect_output: Linux}
`
	ws := e.workspace(t, y)
	assert.Equal(t, domain.IsolationWSL, ws.Isolation, "the stored isolation level must be what was configured")
	require.NoError(t, e.m.ApproveWorkspace(ws.ID))
	x, err := e.m.NewExecution(ws.ID, "")
	require.NoError(t, err)
	require.NoError(t, e.m.Start(context.Background(), x.ID))
	_, sum, err := e.m.Verify(context.Background(), x.ID, nil)
	require.NoError(t, err)
	assert.Equal(t, "passed", sum.Overall)

	// The file was created by a Linux process against the Windows directory, and
	// the output really came from inside the distribution.
	b, err := os.ReadFile(filepath.Join(e.root, "kernel.txt"))
	require.NoError(t, err)
	assert.Contains(t, string(b), "Linux")
	d, err := e.m.Detail(x.ID)
	require.NoError(t, err)
	var saw bool
	for _, c := range d.Commands {
		assert.Equal(t, domain.IsolationWSL, c.Isolation, "every command records the level it ran at")
		out, _ := e.st.ListOutput(c.ID, 0, 100)
		for _, l := range out {
			if strings.Contains(l.Line, "inside-wsl user=") {
				saw = true
			}
		}
	}
	assert.True(t, saw, "command output was recorded")
}

func TestWSLIsolationNeverFallsBackToNativeExecution(t *testing.T) {
	if _, err := exec.LookPath("wsl.exe"); err != nil {
		t.Skip("wsl.exe not found")
	}
	e := setup(t)
	// The distribution does not exist: the execution must fail, not run natively.
	y := `version: 1
task: Missing distro
isolation: wsl
wsl: {distro: Runtide-Does-Not-Exist}
start:
  - {name: probe, command: cmd, args: ['/c', 'echo native > native-ran.txt']}
`
	ws := e.workspace(t, y)
	require.NoError(t, e.m.ApproveWorkspace(ws.ID))
	x, err := e.m.NewExecution(ws.ID, "")
	require.NoError(t, err)
	err = e.m.Start(context.Background(), x.ID)
	assert.Error(t, err, "a missing runtime is an error, never a silent downgrade")
	_, statErr := os.Stat(filepath.Join(e.root, "native-ran.txt"))
	assert.True(t, os.IsNotExist(statErr), "the command must not have run natively")
	got, _ := e.st.GetExecution(x.ID)
	assert.Equal(t, domain.StateFailed, got.State)
}
