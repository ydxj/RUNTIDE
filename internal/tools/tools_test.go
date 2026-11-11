package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectRealToolsHaveValidatedVersions(t *testing.T) {
	ts := Detect(context.Background())
	require.Len(t, ts, len(defs()))
	for _, tl := range ts {
		if tl.Installed {
			assert.NotEmpty(t, tl.Version, tl.ID)
			assert.NotEmpty(t, tl.Path, tl.ID)
			_, err := os.Stat(tl.Path)
			assert.NoError(t, err, tl.ID)
		}
	}
	// The Go toolchain running this test must be detected.
	var goTool Tool
	for _, tl := range ts {
		if tl.ID == "go" {
			goTool = tl
		}
	}
	assert.True(t, goTool.Installed)
}

func TestFolderAloneIsNotInstallation(t *testing.T) {
	dir := t.TempDir()
	// A fake "node.exe" that is not a real executable must be rejected.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "node.exe"), []byte("not an exe"), 0o755))
	t.Setenv("PATH", dir)
	t.Setenv("PATHEXT", ".EXE")
	d := defs()[0]
	d.locations = nil
	r := detectOne(context.Background(), d)
	assert.False(t, r.Installed)
	assert.Contains(t, r.Note, "not usable")
}

func TestParseVersion(t *testing.T) {
	assert.Equal(t, "8.2.1", parseVersion(def{re: defs()[4].re}, "PHP 8.2.1 (cli) (built: ...)"))
	assert.Equal(t, "20.11.0", parseVersion(def{}, "v20.11.0"))
	assert.True(t, isStoreStub(`C:\Users\x\AppData\Local\Microsoft\WindowsApps\python.exe`))
}
