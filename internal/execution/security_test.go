package execution

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/runtide/runtide/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func (e *env) outputOf(t *testing.T, execID string) string {
	t.Helper()
	d, err := e.m.Detail(execID)
	require.NoError(t, err)
	var sb strings.Builder
	for _, c := range d.Commands {
		lines, _ := e.st.ListOutput(c.ID, 0, 1000)
		for _, l := range lines {
			sb.WriteString(l.Line + "\n")
		}
	}
	return sb.String()
}

// Arguments are passed as a vector: shell metacharacters stay literal text and
// nothing is interpreted, whatever the argument contains.
func TestArgumentsAreNeverInterpretedByAShell(t *testing.T) {
	e := setup(t)
	evil := []string{
		`a & echo pwned > pwned.txt`, `"quoted" "x"`, `%PATH%`, `^caret`, `x|y`, `$(id)`, "`id`",
		`; del important.txt`, `&& calc`, `a"b`, `trailing\`, `C:\path with spaces\file`, `'single'`,
	}
	args := ""
	for _, a := range evil {
		args += "'" + strings.ReplaceAll(a, "'", "''") + "', "
	}
	y := "version: 1\ntask: args\nstart:\n  - {name: echo, command: '" + exe() + "', env: {EXEC_HELPER: args}, args: [" + args + "]}\n"
	ws := e.workspace(t, y)
	require.NoError(t, os.WriteFile(filepath.Join(e.root, "important.txt"), []byte("keep"), 0o644))
	x := e.runWith(t, ws)
	out := e.outputOf(t, x.ID)
	for _, a := range evil {
		assert.Contains(t, out, "ARG<"+a+">", "argument must arrive exactly as written: %q", a)
	}
	for _, f := range []string{"pwned.txt"} {
		_, err := os.Stat(filepath.Join(e.root, f))
		assert.True(t, os.IsNotExist(err), "%s must not have been created by an injected command", f)
	}
	b, err := os.ReadFile(filepath.Join(e.root, "important.txt"))
	require.NoError(t, err)
	assert.Equal(t, "keep", string(b))
}

// Batch files are interpreted by cmd.exe, which re-parses arguments: the
// classic Windows injection route. The Go runtime must refuse or neutralise it.
func TestBatchFileTargetsCannotBeUsedForInjection(t *testing.T) {
	e := setup(t)
	require.NoError(t, os.WriteFile(filepath.Join(e.root, "show.cmd"), []byte("@echo off\r\necho GOT:%1\r\n"), 0o644))
	y := "version: 1\ntask: bat\nstart:\n  - {name: bat, command: './show.cmd', args: ['x&echo pwned>pwned-bat.txt'], allow_failure: true}\n"
	ws := e.workspace(t, y)
	require.NoError(t, e.m.ApproveWorkspace(ws.ID))
	x, err := e.m.NewExecution(ws.ID, "")
	require.NoError(t, err)
	startErr := e.m.Start(context.Background(), x.ID) // refusing is acceptable
	t.Logf("start result: %v", startErr)
	if startErr == nil {
		out := e.outputOf(t, x.ID)
		assert.Contains(t, out, "GOT:", "the batch file really ran")
		assert.Contains(t, out, "pwned", "the metacharacters were passed as text, not executed")
	}
	_, statErr := os.Stat(filepath.Join(e.root, "pwned-bat.txt"))
	assert.True(t, os.IsNotExist(statErr), "an injected command ran through cmd.exe")
	_ = exec.Command("cmd", "/c", "exit").Run()
}

// A junction (or symlink) inside the workspace that points outside it must not
// let a verification check read, or a receipt embed, files outside.
func TestJunctionOutOfWorkspaceCannotLeakFiles(t *testing.T) {
	e := setup(t)
	outside := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("TOPSECRET-VALUE"), 0o644))
	link := filepath.Join(e.root, "docs")
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, outside).CombinedOutput(); err != nil {
		t.Skipf("cannot create a junction here: %v %s", err, out)
	}
	y := `version: 1
task: junction
start:
  - {name: noop, command: '` + exe() + `', env: {EXEC_HELPER: ok}}
verify:
  name: v
  checks:
    - {name: leak, type: file, path: docs/secret.txt, contains: TOPSECRET}
`
	ws := e.workspace(t, y)
	x := e.runWith(t, ws)
	results, sum, err := e.m.Verify(context.Background(), x.ID, nil)
	require.NoError(t, err)
	assert.Equal(t, "failed", sum.Overall)
	require.Len(t, results, 1)
	assert.Equal(t, domain.VFailed, results[0].Status)
	assert.Contains(t, results[0].Error, "outside the workspace")
	assert.NotContains(t, results[0].Error, "TOPSECRET", "the check must not even reveal whether the outside file matched")
}
