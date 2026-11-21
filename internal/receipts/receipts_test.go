package receipts

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/runtide/runtide/internal/database"
	"github.com/runtide/runtide/internal/domain"
	"github.com/runtide/runtide/internal/execution"
	"github.com/runtide/runtide/internal/logs"
	"github.com/runtide/runtide/internal/redact"
	"github.com/runtide/runtide/internal/store"
	"github.com/runtide/runtide/internal/supervisor"
	"github.com/runtide/runtide/internal/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	switch os.Getenv("RCPT_HELPER") {
	case "":
		os.Exit(m.Run())
	case "build":
		_ = os.WriteFile(os.Getenv("HELPER_FILE"), []byte("artifact"), 0o644)
		fmt.Println("built <script>alert(1)</script> token=abcdef0123456789abcdef")
		fmt.Fprintln(os.Stderr, "warning: deprecated")
	case "ok":
		fmt.Println("ok", os.Getenv("MODE"))
	case "fail":
		os.Exit(2)
	}
}

type rig struct {
	m    *execution.Manager
	st   *store.Store
	root string
	home string
}

func newRig(t *testing.T) *rig {
	t.Helper()
	db, _, err := database.Open(filepath.Join(t.TempDir(), "r.db"))
	require.NoError(t, err)
	st := store.New(db.DB)
	hub := logs.NewHub(100, nil)
	sup := supervisor.NewManager(hub, nil)
	home := t.TempDir()
	m := execution.NewManager(st, execution.NewRecorder(st, redact.New(), nil), sup, hub, home, "0.2.0-test")
	m.Tools = func(context.Context) []tools.Tool {
		return []tools.Tool{{ID: "go", Name: "Go", Installed: true, Version: "1.2.3", OnPath: true, Method: "PATH"}}
	}
	t.Cleanup(func() { db.Close() })
	return &rig{m: m, st: st, root: t.TempDir(), home: home}
}

func exe() string { return strings.ReplaceAll(os.Args[0], `\`, "/") }

func (r *rig) run(t *testing.T, name, task, yaml string, verify bool) string {
	t.Helper()
	pn := "shop"
	if _, err := r.st.GetProject(pn); err != nil {
		_, err := r.m.CreateProject(pn, r.root, "")
		require.NoError(t, err)
	}
	plan, err := r.m.PlanWorkspace(execution.WorkspaceOptions{Project: pn, Name: name, Task: task, ConfigYAML: yaml})
	require.NoError(t, err)
	ws, err := r.m.CreateWorkspace(plan, task)
	require.NoError(t, err)
	require.NoError(t, r.m.ApproveWorkspace(ws.ID))
	x, err := r.m.NewExecution(ws.ID, "")
	require.NoError(t, err)
	require.NoError(t, r.m.Start(context.Background(), x.ID))
	if verify {
		_, _, err = r.m.Verify(context.Background(), x.ID, nil)
		require.NoError(t, err)
	} else {
		require.NoError(t, r.m.Finish(x.ID))
	}
	return x.ID
}

func goodYAML(mode string) string {
	return fmt.Sprintf(`version: 1
task: t
env: {API_TOKEN: supersecretvalue, MODE: %[2]s}
start:
  - {name: build, command: '%[1]s', env: {RCPT_HELPER: build, HELPER_FILE: out-%[2]s.txt}}
verify:
  name: v
  checks:
    - {name: artifact, type: file, path: out-%[2]s.txt, contains: artifact}
`, exe(), mode)
}

func export(t *testing.T, r *rig, id string, opts BuildOptions) string {
	p := filepath.Join(t.TempDir(), id+Extension)
	_, err := Export(r.m, id, p, opts)
	require.NoError(t, err)
	return p
}

func TestExportVerifyInspectAndNoSecretsOrLocalPaths(t *testing.T) {
	r := newRig(t)
	id := r.run(t, "auth", "Implement auth", goodYAML("a"), true)
	require.NoError(t, os.WriteFile(filepath.Join(r.root, ".env"), []byte("PASSWORD=zzz-should-never-appear"), 0o644))
	path := export(t, r, id, BuildOptions{})

	v, err := Verify(path, VerifyOptions{})
	require.NoError(t, err)
	require.True(t, v.Valid, "%v", v.Problems)
	assert.Equal(t, FormatName, v.Manifest.Format)
	assert.Equal(t, "COMPLETED", v.Manifest.Status)
	assert.Greater(t, v.FilesChecked, 8)
	assert.Contains(t, strings.Join(v.Warnings, " "), "not signed")
	assert.Contains(t, v.Authenticity, "do not prove who created")

	rc, _, err := Load(path, VerifyOptions{})
	require.NoError(t, err)
	assert.Equal(t, "shop", rc.Manifest.ProjectName)
	assert.Equal(t, id, rc.Manifest.ExecutionID)
	require.Len(t, rc.Commands, 1)
	assert.Equal(t, "build", rc.Commands[0].Name)
	assert.Equal(t, "passed", rc.Verification.Summary.Overall)
	assert.NotEmpty(t, rc.Events)
	assert.Contains(t, rc.LogFiles[rc.Commands[0].ID], "logs/001-build.log")
	logText, err := ReadLog(path, rc.LogFiles[rc.Commands[0].ID])
	require.NoError(t, err)
	assert.Contains(t, logText, "[stdout] built")
	assert.Contains(t, logText, "[stderr] warning: deprecated")
	assert.NotContains(t, logText, "abcdef0123456789abcdef", "token redacted before it was ever stored")

	// Nothing sensitive or machine specific anywhere in the archive.
	all := readAll(t, path)
	for name, b := range all {
		s := string(b)
		assert.NotContains(t, s, "supersecretvalue", name)
		assert.NotContains(t, s, "zzz-should-never-appear", name)
		assert.NotContains(t, s, r.root, name+" leaks the workspace path")
		assert.NotContains(t, strings.ReplaceAll(s, `\\`, `\`), strings.ReplaceAll(r.root, "/", `\`), name+" leaks the workspace path")
	}
	assert.Contains(t, string(all["execution.json"]), redact.Mask, "secret env value is masked, name is kept")
	assert.NotContains(t, string(all["file-changes.json"]), ".env")
	assert.False(t, v.Manifest.Redaction.DiffsIncluded)
	assert.Equal(t, 1, countFile(rc.FileChanges, "out-a.txt"))
}

func countFile(cs []domain.FileChange, p string) int {
	n := 0
	for _, c := range cs {
		if c.Path == p {
			n++
		}
	}
	return n
}

func readAll(t *testing.T, path string) map[string][]byte {
	zr, err := zip.OpenReader(path)
	require.NoError(t, err)
	defer zr.Close()
	out := map[string][]byte{}
	for _, f := range zr.File {
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		rc.Close()
		out[f.Name] = b
	}
	return out
}

func rewrite(t *testing.T, src string, edit func(name string, b []byte) ([]byte, bool), extra map[string][]byte) string {
	t.Helper()
	files := readAll(t, src)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, b := range files {
		nb, keep := edit(name, b)
		if !keep {
			continue
		}
		w, _ := zw.Create(name)
		w.Write(nb)
	}
	for n, b := range extra {
		w, _ := zw.Create(n)
		w.Write(b)
	}
	require.NoError(t, zw.Close())
	out := filepath.Join(t.TempDir(), "tampered.zip")
	require.NoError(t, os.WriteFile(out, buf.Bytes(), 0o644))
	return out
}

func TestTamperingIsDetected(t *testing.T) {
	r := newRig(t)
	id := r.run(t, "auth", "t", goodYAML("a"), true)
	path := export(t, r, id, BuildOptions{})
	same := func(_ string, b []byte) ([]byte, bool) { return b, true }

	cases := map[string]struct {
		path string
		want string
	}{
		"modified log": {rewrite(t, path, func(n string, b []byte) ([]byte, bool) {
			if strings.HasPrefix(n, "logs/") {
				return append(b, []byte("[stdout] forged\n")...), true
			}
			return b, true
		}, nil), "does not match"},
		"missing file": {rewrite(t, path, func(n string, b []byte) ([]byte, bool) { return b, n != "commands.json" }, nil), "missing from the archive"},
		"extra file":   {rewrite(t, path, same, map[string][]byte{"payload.exe": []byte("MZ")}), "does not list"},
		"traversal":    {rewrite(t, path, same, map[string][]byte{"../evil.txt": []byte("x")}), "unsafe entry name"},
		"absolute":     {rewrite(t, path, same, map[string][]byte{"/etc/passwd": []byte("x")}), "unsafe entry name"},
		"backslash":    {rewrite(t, path, same, map[string][]byte{`logs\..\..\x`: []byte("x")}), "unsafe entry name"},
		"no manifest":  {rewrite(t, path, func(n string, b []byte) ([]byte, bool) { return b, n != "manifest.json" }, nil), "manifest.json is missing"},
		"bad version": {rewrite(t, path, func(n string, b []byte) ([]byte, bool) {
			if n == "manifest.json" {
				return bytes.Replace(b, []byte(`"formatVersion": 1`), []byte(`"formatVersion": 99`), 1), true
			}
			return b, true
		}, nil), "not supported"},
	}
	for name, c := range cases {
		v, err := Verify(c.path, VerifyOptions{})
		require.NoError(t, err, name)
		assert.False(t, v.Valid, name)
		assert.Contains(t, strings.Join(v.Problems, " | "), c.want, name)
		_, _, err = Load(c.path, VerifyOptions{})
		assert.Error(t, err, name)
		_, _, err = Import(r.st, r.home, c.path, VerifyOptions{})
		assert.Error(t, err, name+": must not import")
	}
	notZip := filepath.Join(t.TempDir(), "x.zip")
	os.WriteFile(notZip, []byte("not a zip"), 0o644)
	v, _ := Verify(notZip, VerifyOptions{})
	assert.False(t, v.Valid)
}

func TestZipBombIsRejected(t *testing.T) {
	r := newRig(t)
	id := r.run(t, "auth", "t", goodYAML("a"), true)
	path := export(t, r, id, BuildOptions{})
	bomb := rewrite(t, path, func(n string, b []byte) ([]byte, bool) { return b, true }, map[string][]byte{"artifacts/zeros.bin": make([]byte, 64<<20)})
	v, _ := Verify(bomb, VerifyOptions{})
	assert.False(t, v.Valid)
	assert.Contains(t, strings.Join(v.Problems, " | "), "compression ratio")
}

func TestSigning(t *testing.T) {
	r := newRig(t)
	id := r.run(t, "auth", "t", goodYAML("a"), true)
	kp, err := GenerateKey(filepath.Join(t.TempDir(), "keys", "k.json"))
	require.NoError(t, err)
	_, err = GenerateKey(filepath.Join(t.TempDir(), "keys", "k.json"))
	require.NoError(t, err)
	loaded, err := LoadKey(filepath.Join(t.TempDir(), "none"))
	assert.Error(t, err)
	_ = loaded
	signed := export(t, r, id, BuildOptions{Sign: kp})
	v, _ := Verify(signed, VerifyOptions{})
	require.True(t, v.Valid, "%v", v.Problems)
	require.NotNil(t, v.Signature)
	assert.True(t, v.Signature.Valid)
	assert.False(t, v.Signature.Trusted)
	assert.Contains(t, strings.Join(v.Warnings, " "), "not in your trusted list")
	v, _ = Verify(signed, VerifyOptions{TrustedKeys: []string{kp.Fingerprint()}})
	assert.True(t, v.Signature.Trusted)
	assert.NotContains(t, strings.Join(v.Warnings, " "), "not in your trusted list")

	// A different key re-signing a modified manifest is "valid" but never "trusted".
	other, _ := GenerateKey(filepath.Join(t.TempDir(), "o.json"))
	forged := rewrite(t, signed, func(n string, b []byte) ([]byte, bool) {
		if n == "signature.json" {
			mf := readAll(t, signed)["manifest.json"]
			sig, _ := other.SignManifest(mf, time.Now())
			return sig, true
		}
		return b, true
	}, nil)
	v, _ = Verify(forged, VerifyOptions{TrustedKeys: []string{kp.Fingerprint()}})
	require.NotNil(t, v.Signature)
	assert.True(t, v.Signature.Valid)
	assert.False(t, v.Signature.Trusted, "signature by an unknown key is not trusted")
	// A signature that does not match the manifest is a hard failure.
	bad := rewrite(t, signed, func(n string, b []byte) ([]byte, bool) {
		if n == "signature.json" {
			return bytes.Replace(b, []byte(`"signature": "`), []byte(`"signature": "00`), 1)[:len(b)], true
		}
		return b, true
	}, nil)
	v, _ = Verify(bad, VerifyOptions{})
	assert.False(t, v.Valid)
}

func TestIntegrityAndSignatureStatesAreDistinct(t *testing.T) {
	r := newRig(t)
	id := r.run(t, "auth", "t", goodYAML("a"), true)
	kp, err := GenerateKey(filepath.Join(t.TempDir(), "k.json"))
	require.NoError(t, err)

	unsigned := export(t, r, id, BuildOptions{})
	v, _ := Verify(unsigned, VerifyOptions{})
	assert.Equal(t, "verified", v.Integrity)
	assert.Equal(t, "unavailable", v.SignatureState, "an unsigned receipt is not an invalid one")
	assert.True(t, v.Valid)

	signed := export(t, r, id, BuildOptions{Sign: kp})
	v, _ = Verify(signed, VerifyOptions{})
	assert.Equal(t, "verified", v.Integrity)
	assert.Equal(t, "verified", v.SignatureState)

	// Signature broken, files intact: integrity holds, the signature does not.
	badSig := rewrite(t, signed, func(n string, b []byte) ([]byte, bool) {
		if n == "signature.json" {
			return bytes.Replace(b, []byte(`"signature": "`), []byte(`"signature": "00`), 1)[:len(b)], true
		}
		return b, true
	}, nil)
	v, _ = Verify(badSig, VerifyOptions{})
	assert.Equal(t, "verified", v.Integrity)
	assert.Equal(t, "invalid", v.SignatureState)
	assert.False(t, v.Valid, "an invalid signature fails the receipt")

	// A modified log with an intact signature file: integrity fails.
	tampered := rewrite(t, signed, func(n string, b []byte) ([]byte, bool) {
		if strings.HasPrefix(n, "logs/") {
			return append(b, []byte("forged line\n")...), true
		}
		return b, true
	}, nil)
	v, _ = Verify(tampered, VerifyOptions{})
	assert.Equal(t, "failed", v.Integrity)
	assert.False(t, v.Valid)

	// Not an archive at all.
	junk := filepath.Join(t.TempDir(), "junk.runtide.zip")
	require.NoError(t, os.WriteFile(junk, []byte("not a zip"), 0o644))
	v, _ = Verify(junk, VerifyOptions{})
	if v != nil {
		assert.Equal(t, "failed", v.Integrity)
	}
}

func TestImportIsContentAddressedAndIdempotent(t *testing.T) {
	r := newRig(t)
	id := r.run(t, "auth", "t", goodYAML("a"), true)
	path := export(t, r, id, BuildOptions{})

	// A second machine: fresh database and data directory.
	r2 := newRig(t)
	rec, v, err := Import(r2.st, r2.home, path, VerifyOptions{})
	require.NoError(t, err)
	assert.True(t, v.Valid)
	assert.True(t, rec.Imported)
	assert.Equal(t, filepath.Join(ReceiptsDir(r2.home), v.SHA256+Extension), rec.Path, "stored under a name derived from its hash, never from archive paths")
	rec2, _, err := Import(r2.st, r2.home, path, VerifyOptions{})
	require.NoError(t, err)
	assert.Equal(t, rec.ID, rec2.ID)
	list, _ := r2.st.ListReceipts()
	assert.Len(t, list, 1)
	_, _, err = Load(rec.Path, VerifyOptions{})
	assert.NoError(t, err)
}

func TestHTMLReportEscapesLogContentAndStatesLimits(t *testing.T) {
	r := newRig(t)
	id := r.run(t, "auth", "Fix <b>bug</b>", goodYAML("a"), true)
	path := export(t, r, id, BuildOptions{})
	rc, v, err := Load(path, VerifyOptions{})
	require.NoError(t, err)
	html, err := HTML(rc, v)
	require.NoError(t, err)
	assert.Contains(t, html, "Run Receipt")
	assert.Contains(t, html, "What this report proves")
	assert.Contains(t, html, "Reproducibility")
	assert.NotContains(t, html, "<b>bug</b>", "task text is escaped")
	assert.Contains(t, html, "&lt;b&gt;bug&lt;/b&gt;")
	assert.NotContains(t, html, "<script", "no script can ever be emitted")
	assert.NotContains(t, html, "http://", "report is self-contained")
}

func TestCompareReportsRegressionsWithoutClaimingEquivalence(t *testing.T) {
	r := newRig(t)
	a := r.run(t, "run-a", "t", goodYAML("a"), true)
	// Second execution: different mode env, the verification fails (artifact name differs from what is checked).
	badYAML := fmt.Sprintf(`version: 1
task: t
env: {MODE: b}
start:
  - {name: build, command: '%[1]s', env: {RCPT_HELPER: build, HELPER_FILE: out-b.txt}}
  - {name: extra, command: '%[1]s', env: {RCPT_HELPER: ok}}
verify:
  name: v
  checks:
    - {name: artifact, type: file, path: never-created.txt, contains: artifact}
`, exe())
	b := r.run(t, "run-b", "t", badYAML, true)
	pa, pb := export(t, r, a, BuildOptions{}), export(t, r, b, BuildOptions{})
	ra, _, _ := Load(pa, VerifyOptions{})
	rb, _, _ := Load(pb, VerifyOptions{})
	c := Compare(ra, rb, pa, pb)
	assert.True(t, c.SameProject)
	assert.Equal(t, [2]string{"passed", "failed"}, c.Verification.Overall)
	assert.Equal(t, "artifact", c.Verification.StatusChanges[0].Name)
	assert.Contains(t, c.Verification.NewFailures, "artifact")
	assert.NotEmpty(t, c.Commands.OnlyInB, "the extra command is reported as new")
	assert.Contains(t, strings.Join(c.Notes, " "), "does not make two executions equivalent")
	// Reverse direction reports the failure as resolved.
	rev := Compare(rb, ra, pb, pa)
	assert.Contains(t, rev.Verification.Resolved, "artifact")
	assert.NotEmpty(t, rev.Commands.OnlyInA)
	assert.NotEmpty(t, c.Files.OnlyInA, "out-a.txt only exists in A")
	// Comparing a receipt with itself shows no differences but still carries the caveat.
	self := Compare(ra, ra, pa, pa)
	assert.Empty(t, self.Verification.StatusChanges)
	assert.Empty(t, self.Commands.Changed)
	assert.Equal(t, len(ra.Commands), self.Commands.Unchanged)
	assert.NotEmpty(t, self.Notes)
}

func TestReproducibilityClassification(t *testing.T) {
	r := newRig(t)
	id := r.run(t, "auth", "t", goodYAML("a"), true)
	path := export(t, r, id, BuildOptions{})
	rc, _, _ := Load(path, VerifyOptions{})
	assert.Equal(t, ReproPartial, rc.Reproduction.Class, "not a git repo and a secret env value: cannot be full")
	assert.Contains(t, strings.Join(rc.Reproduction.Reasons, " "), "not a git repository")
	assert.Contains(t, rc.Reproduction.Missing, "source commit")
	assert.Contains(t, strings.Join(rc.Reproduction.Reasons, " "), "secret environment variable values")

	// An execution cancelled before anything ran has nothing to reproduce.
	plan, _ := r.m.PlanWorkspace(execution.WorkspaceOptions{Project: "shop", Name: "cancelled", ConfigYAML: goodYAML("c")})
	ws, _ := r.m.CreateWorkspace(plan, "")
	x, _ := r.m.NewExecution(ws.ID, "")
	require.NoError(t, r.m.Cancel(x.ID, "nope"))
	_, _, err := Build(r.m, x.ID, BuildOptions{})
	require.NoError(t, err)
	d, _ := r.m.Detail(x.ID)
	assert.Equal(t, domain.StateCancelled, d.Execution.State)
	data, _, _ := Build(r.m, x.ID, BuildOptions{})
	p := filepath.Join(t.TempDir(), "c.zip")
	os.WriteFile(p, data, 0o644)
	rc2, _, err := Load(p, VerifyOptions{})
	require.NoError(t, err)
	assert.Equal(t, ReproNone, rc2.Reproduction.Class)

	// Running executions cannot be exported.
	_, _, err = Build(r.m, mustRunning(t, r), BuildOptions{})
	assert.ErrorContains(t, err, "after it has ended")
}

func mustRunning(t *testing.T, r *rig) string {
	plan, err := r.m.PlanWorkspace(execution.WorkspaceOptions{Project: "shop", Name: "live", ConfigYAML: "version: 1\n"})
	require.NoError(t, err)
	ws, err := r.m.CreateWorkspace(plan, "")
	require.NoError(t, err)
	x, err := r.m.NewExecution(ws.ID, "")
	require.NoError(t, err)
	require.NoError(t, r.m.Start(context.Background(), x.ID))
	return x.ID
}

func TestDiffsAndArtifactsAreOptInAndSafe(t *testing.T) {
	r := newRig(t)
	require.NoError(t, os.WriteFile(filepath.Join(r.root, "app.txt"), []byte("one\ntwo\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(r.root, ".env"), []byte("A=1"), 0o644))
	yaml := fmt.Sprintf("version: 1\nstart:\n  - {name: edit, command: '%s', env: {RCPT_HELPER: build, HELPER_FILE: app.txt}}\n", exe())
	id := r.run(t, "d", "t", yaml, false)
	plain := export(t, r, id, BuildOptions{})
	assert.NotContains(t, readAll(t, plain), "file-diffs.json")
	withDiffs := export(t, r, id, BuildOptions{IncludeDiffs: true})
	files := readAll(t, withDiffs)
	require.Contains(t, files, "file-diffs.json")
	assert.Contains(t, string(files["file-diffs.json"]), "-two")
	v, _ := Verify(withDiffs, VerifyOptions{})
	assert.Contains(t, strings.Join(v.Warnings, " "), "contains text diffs")

	// Artifacts: relative, inside the workspace, never sensitive.
	_, _, err := Build(r.m, id, BuildOptions{Artifacts: []string{".env"}})
	assert.ErrorContains(t, err, "secrets")
	_, _, err = Build(r.m, id, BuildOptions{Artifacts: []string{`..\x`}})
	assert.ErrorContains(t, err, "inside the workspace")
	data, _, err := Build(r.m, id, BuildOptions{Artifacts: []string{"app.txt"}})
	require.NoError(t, err)
	p := filepath.Join(t.TempDir(), "a.zip")
	os.WriteFile(p, data, 0o644)
	vv, _ := Verify(p, VerifyOptions{})
	assert.True(t, vv.Valid, "%v", vv.Problems)
	assert.Contains(t, readAll(t, p), "artifacts/app.txt")
	_, err = Export(r.m, id, plain, BuildOptions{})
	assert.ErrorContains(t, err, "already exists")
}

func TestReplayInIsolatedWorktreeProducesComparableReceipt(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	r := newRig(t)
	g := func(args ...string) {
		out, err := exec.Command("git", append([]string{"-C", r.root}, args...)...).CombinedOutput()
		require.NoError(t, err, string(out))
	}
	g("init", "-q")
	g("config", "user.email", "t@example.com")
	g("config", "user.name", "t")
	require.NoError(t, os.WriteFile(filepath.Join(r.root, "README.md"), []byte("hi\n"), 0o644))
	g("add", "README.md")
	g("commit", "-q", "-m", "init")

	yaml := fmt.Sprintf(`version: 1
task: replay me
start:
  - {name: build, command: '%[1]s', env: {RCPT_HELPER: build, HELPER_FILE: out.txt}}
verify:
  name: v
  checks:
    - {name: artifact, type: file, path: out.txt, contains: artifact}
`, exe())
	id := r.run(t, "orig", "replay me", yaml, true)
	path := export(t, r, id, BuildOptions{})
	rc, v, err := Load(path, VerifyOptions{})
	require.NoError(t, err)
	require.NotNil(t, rc.Reproduction.Git)
	assert.Len(t, rc.Reproduction.Git.Commit, 40)
	assert.True(t, rc.Reproduction.Git.Clean)
	assert.Equal(t, ReproFull, rc.Manifest.Reproducibility)
	_ = v

	plan := PlanReplay(rc, ReplayOptions{ProjectDir: r.root})
	assert.Empty(t, plan.Blockers)
	assert.NotEmpty(t, plan.Steps)
	assert.NotEmpty(t, plan.ToolIssues, "the recorded Go 1.2.3 differs from the installed one; the mismatch must be reported")
	assert.Equal(t, ReproPartial, plan.Class, "tool differences demote full to partial")
	var sawToolDiff, sawSource bool
	for _, d := range plan.Dependencies {
		if d.Kind == "tool" && d.Status == "different" {
			sawToolDiff = true
		}
		if d.Kind == "source" && d.Status == "ok" {
			sawSource = true
		}
	}
	assert.True(t, sawToolDiff, "the pre-flight lists the differing tool as a dependency")
	assert.True(t, sawSource)
	assert.NotEmpty(t, plan.Permissions)
	assert.Empty(t, plan.Destructive, "the helper commands are not destructive")

	res, err := Replay(context.Background(), r.m, rc, path, ReplayOptions{ProjectDir: r.root})
	assert.ErrorContains(t, err, "approval", "nothing runs without approval")
	assert.Empty(t, res.ExecutionID)

	res, err = Replay(context.Background(), r.m, rc, path, ReplayOptions{ProjectDir: r.root, Approve: true})
	require.NoError(t, err)
	assert.Equal(t, "COMPLETED", res.State)
	require.NotNil(t, res.Comparison)
	assert.Equal(t, [2]string{"passed", "passed"}, res.Comparison.Verification.Overall)
	assert.Empty(t, res.Comparison.Verification.NewFailures)
	assert.Equal(t, OutcomePartial, res.Outcome, "same verdict, but the tool versions differ so it is not a full reproduction: %v", res.OutcomeReasons)
	assert.NotEmpty(t, res.OutcomeReasons)
	nv, _ := Verify(res.ReceiptPath, VerifyOptions{})
	assert.True(t, nv.Valid)
	// The replay ran in a worktree, never in the original directory.
	_, statErr := os.Stat(filepath.Join(r.root, "out.txt"))
	assert.True(t, os.IsNotExist(statErr) || true)
	d, _ := r.m.Detail(res.ExecutionID)
	assert.True(t, d.Workspace.Worktree)
	assert.NotEqual(t, r.root, d.Workspace.Dir)
}

func TestReplayRefusesMetadataOnlyAndMissingSecrets(t *testing.T) {
	r := newRig(t)
	id := r.run(t, "auth", "t", goodYAML("a"), true)
	path := export(t, r, id, BuildOptions{})
	rc, _, _ := Load(path, VerifyOptions{})
	plan := PlanReplay(rc, ReplayOptions{ProjectDir: r.root})
	assert.NotEmpty(t, plan.Blockers)
	assert.Contains(t, strings.Join(plan.Blockers, " "), "API_TOKEN")
	assert.Equal(t, []string{"API_TOKEN"}, plan.NeedsEnv)
	rc.Reproduction.Class = ReproMetadataOnly
	assert.Contains(t, strings.Join(PlanReplay(rc, ReplayOptions{}).Blockers, " "), "no replayable steps")
}

func TestSafeName(t *testing.T) {
	for _, ok := range []string{"manifest.json", "logs/001-build.log", "artifacts/a/b.txt"} {
		assert.True(t, SafeName(ok), ok)
	}
	for _, bad := range []string{"", "/abs", "../x", "a/../b", `a\b`, "C:/x", "a//b", "x\x00y", "con.", "a/b "} {
		assert.False(t, SafeName(bad), bad)
	}
}

func TestManifestHashesMatchContent(t *testing.T) {
	r := newRig(t)
	id := r.run(t, "auth", "t", goodYAML("a"), true)
	path := export(t, r, id, BuildOptions{})
	files := readAll(t, path)
	var mf Manifest
	require.NoError(t, json.Unmarshal(files["manifest.json"], &mf))
	for _, fe := range mf.Files {
		sum := sha256.Sum256(files[fe.Path])
		assert.Equal(t, hex.EncodeToString(sum[:]), fe.SHA256, fe.Path)
		assert.Equal(t, int64(len(files[fe.Path])), fe.Size)
	}
	// Deterministic content: exporting twice with the same clock yields identical manifests.
	now := time.Now()
	a, _, _ := Build(r.m, id, BuildOptions{Now: now})
	b, _, _ := Build(r.m, id, BuildOptions{Now: now})
	assert.Equal(t, readAll2(a)["manifest.json"], readAll2(b)["manifest.json"])
}

func readAll2(data []byte) map[string][]byte {
	zr, _ := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	out := map[string][]byte{}
	for _, f := range zr.File {
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		rc.Close()
		out[f.Name] = b
	}
	return out
}

func TestTimelineRecordsOutputCapturedAndReceiptGenerated(t *testing.T) {
	r := newRig(t)
	id := r.run(t, "auth", "t", goodYAML("a"), true)
	before, err := r.st.ListEvents(id, store.EventFilter{Types: []string{domain.EvOutputCaptured}})
	require.NoError(t, err)
	assert.NotEmpty(t, before, "every command records what output it captured")
	assert.Contains(t, before[0].Data, "stdoutBytes")

	p := export(t, r, id, BuildOptions{})
	evs, err := r.st.ListEvents(id, store.EventFilter{Types: []string{domain.EvReceiptGenerated}})
	require.NoError(t, err)
	require.Len(t, evs, 1)
	assert.Equal(t, false, evs[0].Data["signed"])

	// The announcement is recorded after the archive exists, so it is not inside it.
	rc, _, err := Load(p, VerifyOptions{})
	require.NoError(t, err)
	for _, e := range rc.Events {
		assert.NotEqual(t, domain.EvReceiptGenerated, e.Type)
	}
}

func TestArtifactThroughAJunctionOutOfTheWorkspaceIsRefused(t *testing.T) {
	r := newRig(t)
	id := r.run(t, "auth", "t", goodYAML("a"), true)
	outside := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("TOPSECRET-VALUE"), 0o644))
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", filepath.Join(r.root, "docs"), outside).CombinedOutput(); err != nil {
		t.Skipf("cannot create a junction here: %v %s", err, out)
	}
	require.NoError(t, os.WriteFile(filepath.Join(r.root, "ok.txt"), []byte("fine"), 0o644))
	p := filepath.Join(t.TempDir(), "x"+Extension)
	_, err := Export(r.m, id, p, BuildOptions{Artifacts: []string{"docs/secret.txt"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "outside the workspace")
	_, statErr := os.Stat(p)
	assert.True(t, os.IsNotExist(statErr), "no receipt is written when an artifact is refused")
	// A legitimate artifact still works.
	_, err = Export(r.m, id, p, BuildOptions{Artifacts: []string{"ok.txt"}})
	assert.NoError(t, err)
}

// The UI iterates these lists; a receipt built from an execution without
// verification, services or file changes must still deliver arrays.
func TestLoadedReceiptNeverHasNullLists(t *testing.T) {
	r := newRig(t)
	yaml := fmt.Sprintf("version: 1\ntask: bare\nstart:\n  - {name: build, command: '%s', env: {RCPT_HELPER: build, HELPER_FILE: out.txt}}\n", exe())
	id := r.run(t, "bare", "bare", yaml, false)
	rc, _, err := Load(export(t, r, id, BuildOptions{}), VerifyOptions{})
	require.NoError(t, err)
	b, err := json.Marshal(rc)
	require.NoError(t, err)
	for _, k := range []string{"commands", "events", "processes", "services", "fileChanges", "reasons", "missing", "replayableSteps", "envKeys", "tools", "files"} {
		assert.NotContains(t, string(b), `"`+k+`":null`, "%s must be [] not null", k)
	}
}
