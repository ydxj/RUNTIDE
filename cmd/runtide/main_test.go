package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// run executes the CLI in-process against an isolated data directory and
// captures stdout. Commands here only use the in-process engine (no daemon).
func run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	t.Setenv("RUNTIDE_HOME", t.TempDir())
	old := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w
	done := make(chan string)
	go func() { b, _ := io.ReadAll(r); done <- string(b) }() // drain concurrently: output can exceed the pipe buffer
	jsonOut = false
	cmd := newRoot()
	cmd.SetArgs(args)
	var errBuf bytes.Buffer
	cmd.SetErr(&errBuf)
	execErr := cmd.Execute()
	w.Close()
	os.Stdout = old
	return <-done, execErr
}

func TestVersionJSON(t *testing.T) {
	out, err := run(t, "version", "--json")
	require.NoError(t, err)
	var v map[string]string
	require.NoError(t, json.Unmarshal([]byte(out), &v))
	assert.NotEmpty(t, v["version"])
}

func TestPortsAndProcessesJSONAreReal(t *testing.T) {
	out, err := run(t, "ports", "--json")
	require.NoError(t, err)
	var ports []map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &ports))
	assert.NotEmpty(t, ports)
	assert.Contains(t, ports[0], "pid")

	out, err = run(t, "processes", "--json")
	require.NoError(t, err)
	var ps []map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &ps))
	assert.Greater(t, len(ps), 20)
}

func TestToolsListsGo(t *testing.T) {
	out, err := run(t, "tools", "--json")
	require.NoError(t, err)
	var ts []struct {
		ID        string
		Installed bool
	}
	require.NoError(t, json.Unmarshal([]byte(out), &ts))
	found := false
	for _, x := range ts {
		if x.ID == "go" && x.Installed {
			found = true
		}
	}
	assert.True(t, found)
}

func TestKillRefusesProtectedAndInvalid(t *testing.T) {
	_, err := run(t, "kill", "4", "--yes")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "protected")
	_, err = run(t, "kill", "abc")
	var ee *exitError
	require.True(t, errors.As(err, &ee))
	assert.Equal(t, exitUsage, ee.code)
}

func TestUsageErrorsHaveExitCode2(t *testing.T) {
	_, err := run(t, "ports", "--no-such-flag")
	var ee *exitError
	require.True(t, errors.As(err, &ee))
	assert.Equal(t, exitUsage, ee.code)
}

func TestStackImportRequiresConfirmationWhenNotInteractive(t *testing.T) {
	dir := t.TempDir()
	f := dir + `\w.yaml`
	require.NoError(t, os.WriteFile(f, []byte("name: t\nservices:\n  - {name: a, command: cmd.exe}\n"), 0o600))
	_, err := run(t, "stack", "import", f)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--yes")
	out, err := run(t, "stacks", "--json")
	require.NoError(t, err)
	assert.JSONEq(t, "[]", out, "nothing may be registered without confirmation")
}

func TestHelpers(t *testing.T) {
	assert.Equal(t, "1.5 KB", bytesStr(1536))
	assert.Equal(t, "2m05s", durStr(125))
	assert.Equal(t, "-", dash(""))
	assert.Equal(t, "abc…", trunc("abcdef", 4))
}
