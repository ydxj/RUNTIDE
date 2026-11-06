// Package paths resolves RUNTIDE's per-user data locations.
package paths

import (
	"fmt"
	"os"
	"path/filepath"
)

// Home returns the data directory. RUNTIDE_HOME overrides the default
// (%LOCALAPPDATA%\Runtide); tests use this to stay isolated.
func Home() (string, error) {
	dir := os.Getenv("RUNTIDE_HOME")
	if dir == "" {
		base, err := os.UserConfigDir() // %AppData% on Windows; use Local when available
		if l := os.Getenv("LOCALAPPDATA"); l != "" {
			base, err = l, nil
		}
		if err != nil {
			return "", fmt.Errorf("locate user data directory: %w", err)
		}
		dir = filepath.Join(base, "Runtide")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create data directory: %w", err)
	}
	return dir, nil
}

// DB returns the SQLite database path.
func DB(home string) string { return filepath.Join(home, "runtide.db") }

// BackupDir returns (and creates) the backup directory.
func BackupDir(home string) string {
	d := filepath.Join(home, "backups")
	_ = os.MkdirAll(d, 0o700)
	return d
}

// APIFile is where a running engine publishes its loopback address and token.
func APIFile(home string) string { return filepath.Join(home, "api.json") }
