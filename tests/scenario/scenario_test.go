// Package scenario runs RUNTIDE's end-to-end workflow through the real CLI
// binary: real processes, real ports, real files, real receipts.
package scenario

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var cli, helper string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "runtide-scenario-bin-")
	if err != nil {
		panic(err)
	}
	build := func(out, pkg string) string {
		p := filepath.Join(dir, out)
		cmd := exec.Command("go", "build", "-o", p, pkg)
		cmd.Dir = repoRoot()
		if o, err := cmd.CombinedOutput(); err != nil {
			fmt.Fprintln(os.Stderr, string(o))
			os.Exit(1)
		}
		return p
	}
	cli = build("runtide.exe", "./cmd/runtide")
	helper = build("helper.exe", "./tests/scenario/helper")
	build("runtide-mcp.exe", "./cmd/runtide-mcp") // next to runtide.exe, where `runtide mcp` looks for it
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func repoRoot() string {
	wd, _ := os.Getwd()
	return filepath.Dir(filepath.Dir(wd))
}

type rt struct {
	t    *testing.T
	home string
}

func (r *rt) run(args ...string) (string, string, int) {
	r.t.Helper()
	cmd := exec.Command(cli, args...)
	cmd.Env = append(os.Environ(), "RUNTIDE_HOME="+r.home, "NO_COLOR=1")
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		r.t.Fatalf("running runtide %v: %v", args, err)
	}
	return out.String(), errb.String(), code
}

func (r *rt) ok(args ...string) string {
	r.t.Helper()
	o, e, c := r.run(args...)
	require.Equal(r.t, 0, c, "runtide %v\nstdout: %s\nstderr: %s", args, o, e)
	return o
}

func freePort(t *testing.T) int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func portOpen(port int) bool {
	c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 400*time.Millisecond)
	if err != nil {
		return false
	}
	c.Close()
	return true
}

func writeConfig(t *testing.T, dir string, port int, failVerify bool) string {
	h := strings.ReplaceAll(helper, `\`, "/")
	check := "    - {name: artifact, type: file, path: result/out.txt, contains: compiled}\n"
	if failVerify {
		check = "    - {name: artifact, type: file, path: result/never.txt}\n    - {name: dependent, command: '" + h + "', args: [echo, hi], needs: [artifact]}\n"
	}
	y := fmt.Sprintf(`version: 1
task: Implement authentication
policy: {allow_adhoc: true}
env: {API_PASSWORD: do-not-leak-me}
start:
  - {name: compile, command: '%[1]s', args: [write, result/out.txt, compiled]}
services:
  - name: api
    command: '%[1]s'
    args: [serve, '%[2]d']
    stop_timeout_sec: 4
    healthcheck: {type: http, url: 'http://127.0.0.1:%[2]d/health', interval_sec: 1}
verify:
  name: acceptance
  checks:
%[3]s    - {name: api-health, type: http, url: 'http://127.0.0.1:%[2]d/health', expected_status: 200, timeout_sec: 10}
    - {name: services, type: services_healthy, timeout_sec: 10}
`, h, port, check)
	p := filepath.Join(dir, "workspace.yaml")
	require.NoError(t, os.WriteFile(p, []byte(y), 0o644))
	return p
}

func jsonOf(t *testing.T, s string, v any) {
	require.NoError(t, json.Unmarshal([]byte(s), v), s)
}

func TestFullWorkflowThroughTheCLI(t *testing.T) {
	home := t.TempDir()
	r := &rt{t: t, home: home}
	t.Cleanup(func() { r.run("daemon", "stop") })
	project := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(project, "src"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(project, "src", "app.txt"), []byte("v1\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(project, ".env"), []byte("DB_PASSWORD=must-not-appear"), 0o644))
	port := freePort(t)
	cfg := writeConfig(t, t.TempDir(), port, false)

	// 1-2: project and workspace (plan is shown, creation needs confirmation).
	r.ok("project", "create", "shop", "--root", project)
	_, _, code := r.run("workspace", "create", "auth", "--project", "shop", "--config", cfg)
	assert.NotEqual(t, 0, code, "non-interactive creation without --yes must refuse")
	out := r.ok("workspace", "create", "auth", "--project", "shop", "--config", cfg, "--yes")
	assert.Contains(t, out, "will create")
	assert.Contains(t, out, "managed isolation does not separate files")

	// Nothing runs before approval.
	var ws []map[string]any
	jsonOf(t, r.ok("workspace", "list", "--json"), &ws)
	require.Len(t, ws, 1)
	assert.Equal(t, false, ws[0]["approved"])
	r.ok("run", "create", "--workspace", "shop/auth")
	var runs []map[string]any
	jsonOf(t, r.ok("run", "list", "--json"), &runs)
	require.Len(t, runs, 1)
	id := runs[0]["id"].(string)
	_, errs, code := r.run("run", "start", id)
	assert.NotEqual(t, 0, code, "start without approval and without --yes must not run anything")
	_ = errs
	assert.False(t, portOpen(port))

	// 3-7: start with approval, real processes.
	r.ok("run", "start", id, "--yes")
	require.True(t, portOpen(port), "service must really listen")
	status := r.ok("run", "status", id)
	assert.Contains(t, status, "RUNNING")
	assert.Contains(t, status, "healthy")
	logs := r.ok("run", "logs", id)
	assert.Contains(t, logs, "wrote result/out.txt")
	assert.Contains(t, logs, "api listening")
	assert.NotContains(t, logs, "abcdef0123456789abcdef", "secrets are redacted in recorded output")
	assert.Contains(t, r.ok("run", "events", id, "--type", "service.healthy"), "service.healthy")

	// ad-hoc command changes a tracked file; consent is required.
	_, _, code = r.run("run", "exec", id, "--", helper, "append", filepath.Join(project, "src", "app.txt"), "v2")
	assert.NotEqual(t, 0, code, "ad-hoc execution without --yes in a non-interactive shell is refused")
	r.ok("run", "exec", id, "--yes", "--name", "edit", "--", helper, "append", filepath.Join(project, "src", "app.txt"), "v2")

	// 12-13: verification with real checks.
	vout := r.ok("run", "verify", id)
	assert.Contains(t, vout, "Overall: passed")
	assert.False(t, portOpen(port), "services are stopped when the execution completes")
	var d struct {
		Execution   struct{ State, Verification string }
		FileChanges []struct{ Path, Change string }
		Commands    []struct{ Name, Kind string }
	}
	jsonOf(t, r.ok("run", "status", id, "--json"), &d)
	assert.Equal(t, "COMPLETED", d.Execution.State)
	changes := map[string]string{}
	for _, c := range d.FileChanges {
		changes[c.Path] = c.Change
	}
	assert.Equal(t, "added", changes["result/out.txt"])
	assert.Equal(t, "modified", changes["src/app.txt"])
	assert.NotContains(t, changes, ".env")
	assert.Contains(t, r.ok("run", "diff", id, "src/app.txt"), "+v2")

	// 14-15: receipt, integrity, tamper detection.
	rcpt := filepath.Join(t.TempDir(), "auth.runtide.zip")
	exp := r.ok("run", "export", id, "-o", rcpt)
	assert.Contains(t, exp, "reproducibility")
	assert.Contains(t, r.ok("receipt", "verify", rcpt), "VALID")
	insp := r.ok("receipt", "inspect", rcpt)
	assert.Contains(t, insp, "integrity:  OK")
	assert.Contains(t, insp, "reproducibility: partially_reconstructable")
	data, _ := os.ReadFile(rcpt)
	assert.NotContains(t, string(data), "must-not-appear")
	tampered := filepath.Join(t.TempDir(), "tampered.runtide.zip")
	zr, _ := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range zr.File {
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		rc.Close()
		if strings.HasPrefix(f.Name, "logs/") {
			b = append(b, []byte("[stdout] forged line\n")...)
		}
		w, _ := zw.Create(f.Name)
		w.Write(b)
	}
	zw.Close()
	require.NoError(t, os.WriteFile(tampered, buf.Bytes(), 0o644))
	vo, _, code := r.run("receipt", "verify", tampered)
	assert.Equal(t, 3, code)
	assert.Contains(t, vo, "INVALID")

	// 16: import on another "machine" (separate data directory) and inspect there.
	r2 := &rt{t: t, home: t.TempDir()}
	assert.Contains(t, r2.ok("receipt", "import", rcpt), "Imported")
	assert.Contains(t, r2.ok("receipt", "list"), "COMPLETED")
	_, _, code = r2.run("receipt", "import", tampered)
	assert.NotEqual(t, 0, code)
	r2.ok("receipt", "report", rcpt, "-o", filepath.Join(t.TempDir(), "report.html"))

	// 17: a second execution that fails verification, then compare.
	cfg2 := writeConfig(t, t.TempDir(), port, true)
	r.ok("workspace", "reopen", "shop/auth")
	r.ok("workspace", "configure", "shop/auth", cfg2)
	r.ok("run", "create", "--workspace", "shop/auth", "second attempt")
	jsonOf(t, r.ok("run", "list", "--json", "--state", "CREATED"), &runs)
	id2 := runs[0]["id"].(string)
	r.ok("run", "start", id2, "--yes")
	vo, _, code = r.run("run", "verify", id2)
	assert.Equal(t, 3, code, "failed verification yields exit code 3")
	assert.Contains(t, vo, "failed")
	assert.Contains(t, vo, "prerequisite")
	assert.Contains(t, vo, "Overall: failed")
	cmp := r.ok("run", "compare", id, id2)
	assert.Contains(t, cmp, "New failures")
	assert.Contains(t, cmp, "artifact")
	assert.Contains(t, cmp, "does not make two executions equivalent")

	// 18-19: cleanup is real.
	assert.False(t, portOpen(port))
	_, _, code = r.run("run", "orphans")
	assert.Equal(t, 0, code)
	assert.Contains(t, r.ok("run", "orphans"), "No orphaned processes")
	r.ok("daemon", "stop")

	// 20: a fresh start of RUNTIDE still has the history.
	jsonOf(t, r.ok("run", "list", "--json"), &runs)
	assert.Len(t, runs, 2)
	assert.Contains(t, r.ok("run", "status", id), "COMPLETED")
	assert.Contains(t, r.ok("receipt", "list"), "shop")
}

func TestCancelStopsEverythingAndInterruptedRecovery(t *testing.T) {
	r := &rt{t: t, home: t.TempDir()}
	t.Cleanup(func() { r.run("daemon", "stop") })
	project := t.TempDir()
	port := freePort(t)
	cfg := writeConfig(t, t.TempDir(), port, false)
	r.ok("project", "create", "p", "--root", project)
	r.ok("workspace", "create", "w", "--project", "p", "--config", cfg, "--yes")
	var runs []map[string]any
	r.ok("run", "create", "--workspace", "p/w")
	jsonOf(t, r.ok("run", "list", "--json"), &runs)
	id := runs[0]["id"].(string)
	r.ok("run", "start", id, "--yes")
	require.True(t, portOpen(port))
	r.ok("run", "cancel", id, "--reason", "test")
	assert.Eventually(t, func() bool { return !portOpen(port) }, 8*time.Second, 200*time.Millisecond)
	assert.Contains(t, r.ok("run", "status", id), "CANCELLED")

	// Crash simulation: start another run, kill the engine process hard, then restart.
	r.ok("workspace", "reopen", "p/w")
	r.ok("run", "create", "--workspace", "p/w")
	jsonOf(t, r.ok("run", "list", "--json", "--state", "CREATED"), &runs)
	id2 := runs[0]["id"].(string)
	r.ok("run", "start", id2, "--yes")
	var st struct {
		Running bool
		PID     int
	}
	jsonOf(t, r.ok("daemon", "status", "--json"), &st)
	require.True(t, st.Running)
	kill := exec.Command("taskkill", "/PID", fmt.Sprint(st.PID), "/F")
	require.NoError(t, kill.Run())
	assert.Eventually(t, func() bool { return !portOpen(port) }, 8*time.Second, 200*time.Millisecond, "job objects free the service when the engine dies")
	out := r.ok("run", "status", id2) // starts a fresh engine which runs recovery
	assert.Contains(t, out, "INTERRUPTED")
	assert.Contains(t, r.ok("run", "events", id2, "--type", "execution.interrupted"), "execution.interrupted")
	// Interrupted executions can still be exported as evidence.
	rcpt := filepath.Join(t.TempDir(), "i.runtide.zip")
	r.ok("run", "export", id2, "-o", rcpt)
	assert.Contains(t, r.ok("receipt", "inspect", rcpt), "INTERRUPTED")
}

func TestProjectsProfilesRetentionAndSettingsThroughTheCLI(t *testing.T) {
	r := &rt{t: t, home: t.TempDir()}
	t.Cleanup(func() { r.run("daemon", "stop") })
	root := t.TempDir()
	h := strings.ReplaceAll(helper, `\`, "/")
	r.ok("project", "create", "shop", "--root", root)

	// Profiles: validated on save, listed, shown, reused by two executions.
	bad := filepath.Join(t.TempDir(), "bad.yaml")
	require.NoError(t, os.WriteFile(bad, []byte("name: x\nchecks: []\n"), 0o644))
	_, errs, code := r.run("verification", "save", "bad", bad)
	assert.NotEqual(t, 0, code)
	assert.Contains(t, errs, "no checks")
	prof := filepath.Join(t.TempDir(), "p.yaml")
	require.NoError(t, os.WriteFile(prof, []byte("name: built\nchecks:\n  - {name: out, type: file, path: result/out.txt, contains: compiled}\n"), 0o644))
	r.ok("verification", "save", "built", prof, "--description", "build output exists")
	assert.Contains(t, r.ok("verification", "list"), "built")
	assert.Contains(t, r.ok("verification", "show", "built"), "result/out.txt")

	cfgOf := func(task string) string {
		p := filepath.Join(t.TempDir(), "w.yaml")
		y := fmt.Sprintf("version: 1\ntask: %s\nstart:\n  - {name: compile, command: '%s', args: [write, result/out.txt, compiled]}\n", task, h)
		require.NoError(t, os.WriteFile(p, []byte(y), 0o644))
		return p
	}
	r.ok("workspace", "create", "one", "--project", "shop", "--config", cfgOf("first"), "--yes")
	var runs []map[string]any
	r.ok("run", "create", "--workspace", "shop/one")
	jsonOf(t, r.ok("run", "list", "--json"), &runs)
	id := runs[0]["id"].(string)
	r.ok("run", "start", id, "--yes")
	_, errs, code = r.run("run", "verify", id)
	assert.NotEqual(t, 0, code, "no verify section and no default profile")
	assert.Contains(t, errs, "no verification defined")
	out := r.ok("run", "verify", id, "--profile", "built")
	assert.Contains(t, out, "Overall: passed")
	assert.Contains(t, r.ok("run", "status", id), "COMPLETED")

	// Project defaults: a default profile is honoured by a second execution.
	r.ok("project", "update", "shop", "--description", "demo shop", "--default-profile", "built")
	assert.Contains(t, r.ok("project", "show", "shop"), "default profile:   built")
	r.ok("workspace", "reopen", "shop/one")
	r.ok("run", "create", "--workspace", "shop/one", "second")
	jsonOf(t, r.ok("run", "list", "--json", "--state", "CREATED"), &runs)
	id2 := runs[0]["id"].(string)
	r.ok("run", "start", id2, "--yes")
	assert.Contains(t, r.ok("run", "verify", id2), "Overall: passed")

	// Comparing two executions uses throw-away receipts: they must not appear in the receipt index
	// (a phantom entry would point at a deleted file) nor leave events behind.
	r.ok("run", "compare", id, id2)
	var recs []map[string]any
	jsonOf(t, r.ok("receipt", "list", "--json"), &recs)
	assert.Empty(t, recs, "compare must not register receipts")
	assert.NotContains(t, r.ok("run", "events", id, "--type", "receipt.generated"), "receipt.generated")

	// Archive: hidden from the default list, kept with --all, blocks new workspaces; unarchive restores.
	r.ok("project", "archive", "shop")
	assert.NotContains(t, r.ok("project", "list"), "shop")
	assert.Contains(t, r.ok("project", "list", "--all"), "shop (archived)")
	_, errs, code = r.run("workspace", "create", "two", "--project", "shop", "--config", cfgOf("x"), "--yes")
	assert.NotEqual(t, 0, code)
	assert.Contains(t, errs, "archived")
	assert.Contains(t, r.ok("run", "status", id), "COMPLETED", "history survives archiving")
	r.ok("project", "unarchive", "shop")
	assert.Contains(t, r.ok("project", "list"), "shop")

	// Retention: nothing is old, dry-run and real prune both report zero and delete nothing.
	assert.Contains(t, r.ok("run", "prune", "--older-than", "1", "--dry-run"), "0 execution(s)")
	assert.Contains(t, r.ok("run", "prune", "--older-than", "1", "--yes"), "0 execution(s)")
	_, _, code = r.run("run", "prune")
	assert.Equal(t, 2, code, "missing --older-than is a usage error")
	assert.Contains(t, r.ok("run", "status", id), "COMPLETED")

	// Settings: execution limits are validated and persisted.
	_, errs, code = r.run("config", "set", "execMaxOutputMB", "0")
	assert.NotEqual(t, 0, code)
	assert.Contains(t, errs, "execMaxOutputMB")
	r.ok("config", "set", "execMaxOutputMB", "16")
	assert.Contains(t, r.ok("config"), "16")
}

func TestDiagnosticsAndMCPGroupsTellTheTruth(t *testing.T) {
	r := &rt{t: t, home: t.TempDir()}
	t.Cleanup(func() { r.run("daemon", "stop") })

	var rows []struct {
		Level     int
		Name      string
		Available bool
		Detail    string
	}
	jsonOf(t, r.ok("diagnostics", "isolation", "--json"), &rows)
	require.Len(t, rows, 4)
	assert.True(t, rows[0].Available, "managed is always available")
	for _, row := range rows {
		assert.NotEmpty(t, row.Detail, "every level explains its status: %s", row.Name)
	}

	// MCP: configuration printing changes nothing; check performs a real handshake and calls.
	out := r.ok("mcp", "config", "--level", "read")
	assert.Contains(t, out, "claude mcp add runtide")
	assert.Contains(t, out, "--level read")
	_, errs, code := r.run("mcp", "config", "--level", "root")
	assert.Equal(t, 2, code, "an unknown permission level is a usage error")
	assert.Contains(t, errs, "unknown permission level")
	for level, want := range map[string]int{"read": 9, "controlled": 14, "sensitive": 18} {
		var chk struct {
			Tools []string
			Calls map[string]string
		}
		jsonOf(t, r.ok("mcp", "check", "--level", level, "--json"), &chk)
		assert.Len(t, chk.Tools, want, level)
		assert.Equal(t, "ok", chk.Calls["list_projects"], level)
	}
}

// The server also builds for Linux (it is what the Docker image contains). This
// runs that Linux binary inside WSL2 against a receipt produced on Windows, so
// the receipt format and the server are exercised across operating systems.
func TestLinuxServerVerifiesAWindowsReceiptInsideWSL(t *testing.T) {
	if _, err := exec.LookPath("wsl.exe"); err != nil {
		t.Skip("WSL is not installed; the Linux server was NOT exercised")
	}
	wsl := func(args ...string) (string, error) {
		out, err := exec.Command("wsl.exe", args...).CombinedOutput()
		return strings.ReplaceAll(strings.ReplaceAll(string(out), "\x00", ""), "\r", ""), err
	}
	if _, err := wsl("--exec", "true"); err != nil {
		t.Skip("no usable WSL distribution; the Linux server was NOT exercised")
	}

	r := &rt{t: t, home: t.TempDir()}
	t.Cleanup(func() { r.run("daemon", "stop") })
	project := t.TempDir()
	port := freePort(t)
	cfg := writeConfig(t, t.TempDir(), port, false)
	r.ok("project", "create", "lin", "--root", project)
	r.ok("workspace", "create", "w", "--project", "lin", "--config", cfg, "--yes")
	r.ok("run", "create", "--workspace", "lin/w")
	var runs []map[string]any
	jsonOf(t, r.ok("run", "list", "--json"), &runs)
	id := runs[0]["id"].(string)
	r.ok("run", "start", id, "--yes")
	r.ok("run", "verify", id)
	rcpt := filepath.Join(t.TempDir(), "win.runtide.zip")
	r.ok("run", "export", id, "-o", rcpt)

	bin := filepath.Join(t.TempDir(), "runtide-server-linux")
	build := exec.Command("go", "build", "-trimpath", "-o", bin, "./cmd/runtide-server")
	build.Dir = repoRoot()
	build.Env = append(os.Environ(), "GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=0")
	if o, err := build.CombinedOutput(); err != nil {
		t.Fatalf("the server must build for Linux (the Docker image depends on it): %v\n%s", err, o)
	}
	wp := func(p string) string {
		out, err := wsl("--exec", "wslpath", "-a", p)
		require.NoError(t, err, out)
		return strings.TrimSpace(out)
	}
	script := filepath.Join(repoRoot(), "tests", "linux", "server-smoke.sh")
	out, err := wsl("--exec", "bash", wp(script), wp(bin), wp(rcpt))
	require.NoError(t, err, out)
	assert.Contains(t, out, "uname: Linux")
	assert.Contains(t, out, `healthz: {"status":"ok"`)
	assert.Contains(t, out, "unauthenticated /projects: HTTP 401")
	assert.Contains(t, out, "HTTP 201", "a receipt made on Windows is verified and stored by the Linux server")
	assert.Contains(t, out, "report mentions integrity: 1")
	assert.Contains(t, out, "truncated upload: HTTP 422", "a damaged archive is refused")
}
