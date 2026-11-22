package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// junction creates a directory junction (no administrator rights needed).
func junction(t *testing.T, link, target string) {
	t.Helper()
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		t.Skipf("cannot create a junction here: %v %s", err, out)
	}
}

func TestResolveInsideRejectsLexicalAndJunctionEscapes(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("TOPSECRET"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "src"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "src", "ok.txt"), []byte("fine"), 0o644))
	junction(t, filepath.Join(root, "docs"), outside)

	for _, ok := range []string{"src/ok.txt", `src\ok.txt`, "src/new-file-not-created-yet.txt", "new-dir/x.txt"} {
		_, err := ResolveInside(root, ok)
		assert.NoError(t, err, ok)
	}
	for _, bad := range []string{"../x", `..\x`, "src/../../x", `C:\Windows\win.ini`, "/etc/passwd", `\server\share\x`} {
		_, err := ResolveInside(root, bad)
		assert.Error(t, err, bad)
	}
	for _, viaJunction := range []string{"docs/secret.txt", "docs", "docs/not-there.txt", `docs\sub\x.txt`} {
		_, err := ResolveInside(root, viaJunction)
		require.Error(t, err, "a junction pointing outside the workspace must be rejected: %s", viaJunction)
		assert.Contains(t, err.Error(), "outside the workspace")
	}
	// The purely lexical resolver still allows it: that is exactly the gap.
	_, err := ResolveDir(root, "docs/secret.txt")
	assert.NoError(t, err)
}
