package database

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// backupBeforeMigration copies a non-empty database that still needs
// migrations to <dir>/backups/pre-migration-v<N>-<time>.db so a failed or
// unwanted upgrade never loses user data. It returns "" when nothing was needed.
func backupBeforeMigration(d *sql.DB, path string) (string, error) {
	var has int
	if err := d.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name='schema_migrations'`).Scan(&has); err != nil || has == 0 {
		return "", err
	}
	var cur int
	if err := d.QueryRow(`SELECT COALESCE(MAX(version),0) FROM schema_migrations`).Scan(&cur); err != nil {
		return "", err
	}
	if cur == 0 || cur >= migrations[len(migrations)-1].version {
		return "", nil
	}
	dir := filepath.Join(filepath.Dir(path), "backups")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	dst := filepath.Join(dir, fmt.Sprintf("pre-migration-v%d-%s.db", cur, time.Now().Format("20060102-150405.000")))
	q := strings.ReplaceAll(filepath.ToSlash(dst), "'", "''")
	if _, err := d.Exec(fmt.Sprintf("VACUUM INTO '%s'", q)); err != nil {
		return "", err
	}
	return dst, nil
}
