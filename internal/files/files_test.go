package files

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
}

func find(cs []string, s string) bool {
	for _, c := range cs {
		if c == s {
			return true
		}
	}
	return false
}

func TestSensitivePaths(t *testing.T) {
	for _, p := range []string{".env", ".env.local", "config/server.pem", "id_rsa", "a/b/credentials.json", "terraform.tfvars", ".npmrc"} {
		assert.True(t, SensitivePath(p), p)
	}
	for _, p := range []string{".env.example", "src/main.go", "README.md", "keyboard.go"} {
		assert.False(t, SensitivePath(p), p)
	}
}

func TestDiffWithoutGit(t *testing.T) {
	root := t.TempDir()
	write(t, root, "src/a.go", "package a\n")
	write(t, root, "src/b.go", "package b\n")
	write(t, root, "src/gone.go", "package gone\n")
	write(t, root, "old-name.txt", "same content")
	write(t, root, ".env", "SECRET=1")
	write(t, root, "node_modules/lib/x.js", "x")
	opts := Options{Root: root}
	before, err := Take(opts)
	require.NoError(t, err)
	assert.False(t, before.UsedGit)
	_, hasEnv := before.Files[".env"]
	assert.False(t, hasEnv, "secrets never enter a snapshot")
	_, hasNM := before.Files["node_modules/lib/x.js"]
	assert.False(t, hasNM)

	write(t, root, "src/a.go", "package a\n// changed\n")
	write(t, root, "src/new.go", "package n\n")
	require.NoError(t, os.Remove(filepath.Join(root, "src", "gone.go")))
	require.NoError(t, os.Rename(filepath.Join(root, "old-name.txt"), filepath.Join(root, "new-name.txt")))
	write(t, root, ".env", "SECRET=2")
	after, err := Take(opts)
	require.NoError(t, err)
	ch := Diff(before, after)
	got := map[string]string{}
	for _, c := range ch {
		got[c.Path] = c.Change
	}
	assert.Equal(t, map[string]string{"src/a.go": "modified", "src/new.go": "added", "src/gone.go": "deleted", "new-name.txt": "renamed"}, got)
	for _, c := range ch {
		if c.Change == "renamed" {
			assert.Equal(t, "old-name.txt", c.OldPath)
		}
	}
	assert.Greater(t, before.Skipped["sensitive"], 0)
}

func TestGitIgnoreIsRespected(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
	}
	run("init", "-q")
	run("config", "user.email", "t@example.com")
	run("config", "user.name", "t")
	write(t, root, ".gitignore", "*.log\ntmp/\n")
	write(t, root, "main.go", "package main\n")
	write(t, root, "debug.log", "noise")
	write(t, root, "tmp/x", "noise")
	run("add", ".gitignore", "main.go")
	run("commit", "-q", "-m", "init")
	snap, err := Take(Options{Root: root})
	require.NoError(t, err)
	assert.True(t, snap.UsedGit)
	var names []string
	for p := range snap.Files {
		names = append(names, p)
	}
	assert.True(t, find(names, "main.go"))
	assert.False(t, find(names, "debug.log"), "gitignored")
	assert.False(t, find(names, "tmp/x"))
	g := Git(root)
	assert.True(t, g.Repo)
	assert.False(t, g.Dirty)
	assert.Len(t, g.Head, 40)
	write(t, root, "main.go", "package main\n// edit\n")
	write(t, root, ".env", "TOKEN=abc")
	g = Git(root)
	assert.True(t, g.Dirty)
	for _, s := range g.Status {
		assert.NotContains(t, s, ".env", "sensitive paths are not listed")
	}
}

func TestBaselinesAndUnifiedDiff(t *testing.T) {
	root := t.TempDir()
	write(t, root, "a.txt", "one\ntwo\nthree\n")
	require.NoError(t, os.WriteFile(filepath.Join(root, "bin.dat"), []byte{0, 1, 2, 3}, 0o644))
	snap, _ := Take(Options{Root: root})
	bl := Baselines(root, snap, 0, 0)
	assert.Contains(t, bl, "a.txt")
	assert.NotContains(t, bl, "bin.dat", "binary files are not captured")
	d := UnifiedDiff("a/a.txt", "b/a.txt", "one\ntwo\nthree\n", "one\n2\nthree\nfour\n")
	assert.Contains(t, d, "-two")
	assert.Contains(t, d, "+2")
	assert.Contains(t, d, "+four")
	assert.Contains(t, d, "@@ -1,3 +1,4 @@")
	assert.Equal(t, 0, strings.Count(UnifiedDiff("a", "b", "x\n", "x\n"), "@@"))
}
