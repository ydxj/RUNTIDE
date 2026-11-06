// Package database owns RUNTIDE's SQLite store: opening, migrations, backup,
// restore and corruption handling.
package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure-Go driver: no cgo toolchain needed
)

// DB wraps *sql.DB with RUNTIDE specifics.
type DB struct {
	*sql.DB
	Path string
}

type migration struct {
	version int
	name    string
	sql     string
}

var migrations = []migration{
	{1, "initial schema", `
CREATE TABLE settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE TABLE workspaces (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL UNIQUE CHECK (length(name) BETWEEN 1 AND 63),
  root TEXT NOT NULL DEFAULT '',
  description TEXT NOT NULL DEFAULT '',
  yaml TEXT NOT NULL,
  approved_hash TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE TABLE services (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL UNIQUE CHECK (length(name) BETWEEN 1 AND 63),
  spec_json TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE TABLE service_runs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  service TEXT NOT NULL,
  started_at INTEGER NOT NULL,
  ended_at INTEGER,
  exit_code INTEGER,
  reason TEXT NOT NULL DEFAULT '',
  pid INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_runs_service ON service_runs(service, started_at DESC);
CREATE TABLE logs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  service TEXT NOT NULL,
  ts INTEGER NOT NULL,
  stream TEXT NOT NULL CHECK (stream IN ('stdout','stderr','system')),
  level TEXT NOT NULL DEFAULT '',
  line TEXT NOT NULL
);
CREATE INDEX idx_logs_service_id ON logs(service, id);
CREATE INDEX idx_logs_ts ON logs(ts);
CREATE TABLE health_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  service TEXT NOT NULL,
  ts INTEGER NOT NULL,
  status TEXT NOT NULL,
  detail TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_health_service ON health_events(service, ts DESC);
CREATE TABLE activity (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  ts INTEGER NOT NULL,
  kind TEXT NOT NULL,
  subject TEXT NOT NULL DEFAULT '',
  detail TEXT NOT NULL DEFAULT '',
  level TEXT NOT NULL DEFAULT 'info'
);
CREATE INDEX idx_activity_ts ON activity(ts DESC);
CREATE TABLE workspace_history (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  workspace TEXT NOT NULL,
  ts INTEGER NOT NULL,
  action TEXT NOT NULL,
  outcome TEXT NOT NULL,
  detail TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_wshist ON workspace_history(workspace, ts DESC);
CREATE TABLE diagnostic_history (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  ts INTEGER NOT NULL,
  findings INTEGER NOT NULL,
  errors INTEGER NOT NULL,
  warnings INTEGER NOT NULL,
  report_json TEXT NOT NULL
);
`},
}

// Open opens (creating if needed) the database, verifies integrity, and
// applies migrations. If the file is corrupt it is moved aside to
// <name>.corrupt-<timestamp> and a fresh database is created; the returned
// recovered string describes that event ("" when nothing happened).
func Open(path string) (db *DB, recovered string, err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, "", err
	}
	d, err := openRaw(path)
	if err == nil {
		err = checkIntegrity(d)
	}
	if err != nil {
		if d != nil {
			d.Close()
		}
		if _, statErr := os.Stat(path); statErr != nil {
			return nil, "", fmt.Errorf("open database: %w", err)
		}
		moved := fmt.Sprintf("%s.corrupt-%s", path, time.Now().Format("20060102-150405"))
		if rerr := os.Rename(path, moved); rerr != nil {
			return nil, "", fmt.Errorf("database unusable (%v) and could not be moved aside: %w", err, rerr)
		}
		_ = os.Remove(path + "-wal")
		_ = os.Remove(path + "-shm")
		recovered = fmt.Sprintf("database was corrupt (%v); moved to %s and recreated", err, moved)
		d, err = openRaw(path)
		if err != nil {
			return nil, recovered, err
		}
	}
	if err := migrate(d); err != nil {
		d.Close()
		return nil, recovered, err
	}
	return &DB{DB: d, Path: path}, recovered, nil
}

func openRaw(path string) (*sql.DB, error) {
	dsn := "file:" + filepath.ToSlash(path) + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)"
	d, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	d.SetMaxOpenConns(4)
	if err := d.Ping(); err != nil {
		d.Close()
		return nil, err
	}
	return d, nil
}

func checkIntegrity(d *sql.DB) error {
	var res string
	if err := d.QueryRow("PRAGMA integrity_check").Scan(&res); err != nil {
		return err
	}
	if res != "ok" {
		return fmt.Errorf("integrity check failed: %s", res)
	}
	return nil
}

func migrate(d *sql.DB) error {
	if _, err := d.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at INTEGER NOT NULL)`); err != nil {
		return err
	}
	var cur int
	if err := d.QueryRow(`SELECT COALESCE(MAX(version),0) FROM schema_migrations`).Scan(&cur); err != nil {
		return err
	}
	if cur > migrations[len(migrations)-1].version {
		return fmt.Errorf("database schema version %d is newer than this RUNTIDE build supports", cur)
	}
	for _, m := range migrations {
		if m.version <= cur {
			continue
		}
		tx, err := d.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(m.sql); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d (%s): %w", m.version, m.name, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations(version,name,applied_at) VALUES(?,?,?)`, m.version, m.name, time.Now().Unix()); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// SchemaVersion returns the applied schema version.
func (d *DB) SchemaVersion() (int, error) {
	var v int
	err := d.QueryRow(`SELECT COALESCE(MAX(version),0) FROM schema_migrations`).Scan(&v)
	return v, err
}

// Tx runs fn in a transaction, rolling back on error or panic.
func (d *DB) Tx(ctx context.Context, fn func(*sql.Tx) error) (err error) {
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if p := recover(); p != nil {
			tx.Rollback()
			panic(p)
		}
	}()
	if err = fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// Backup writes a consistent copy of the database to dst using VACUUM INTO.
func (d *DB) Backup(dst string) error {
	if _, err := os.Stat(dst); err == nil {
		return fmt.Errorf("backup target %s already exists", dst)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	q := strings.ReplaceAll(filepath.ToSlash(dst), "'", "''")
	if _, err := d.Exec(fmt.Sprintf("VACUUM INTO '%s'", q)); err != nil {
		return fmt.Errorf("backup failed: %w", err)
	}
	return nil
}

// Validate opens a candidate backup read-only and checks integrity and schema.
func Validate(path string) error {
	d, err := openRaw(path)
	if err != nil {
		return err
	}
	defer d.Close()
	if err := checkIntegrity(d); err != nil {
		return err
	}
	var n int
	if err := d.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name IN ('workspaces','services','settings')`).Scan(&n); err != nil || n != 3 {
		return errors.New("file is not a RUNTIDE database")
	}
	return nil
}

// Restore replaces the live database with the backup at src. The caller must
// close the DB first; the previous file is preserved as a .pre-restore copy.
func Restore(livePath, src string) (string, error) {
	if err := Validate(src); err != nil {
		return "", fmt.Errorf("backup is not usable: %w", err)
	}
	keep := fmt.Sprintf("%s.pre-restore-%s", livePath, time.Now().Format("20060102-150405"))
	if _, err := os.Stat(livePath); err == nil {
		if err := os.Rename(livePath, keep); err != nil {
			return "", err
		}
	} else {
		keep = ""
	}
	_ = os.Remove(livePath + "-wal")
	_ = os.Remove(livePath + "-shm")
	in, err := os.ReadFile(src)
	if err != nil {
		return keep, err
	}
	if err := os.WriteFile(livePath, in, 0o600); err != nil {
		return keep, err
	}
	return keep, nil
}

// Prune applies retention policies. Zero values disable that rule.
type Retention struct {
	LogDays, LogMaxRows, ActivityDays, HealthDays, RunDays, HistoryDays int
}

// Prune deletes expired history rows and returns the number removed.
func (d *DB) Prune(r Retention) (int64, error) {
	var total int64
	now := time.Now()
	del := func(q string, args ...any) error {
		res, err := d.Exec(q, args...)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		total += n
		return nil
	}
	cut := func(days int) int64 { return now.AddDate(0, 0, -days).Unix() }
	if r.LogDays > 0 {
		if err := del(`DELETE FROM logs WHERE ts < ?`, cut(r.LogDays)); err != nil {
			return total, err
		}
	}
	if r.LogMaxRows > 0 {
		if err := del(`DELETE FROM logs WHERE id <= (SELECT COALESCE(MAX(id),0) FROM logs) - ?`, r.LogMaxRows); err != nil {
			return total, err
		}
	}
	for _, p := range []struct {
		q    string
		days int
	}{
		{`DELETE FROM activity WHERE ts < ?`, r.ActivityDays},
		{`DELETE FROM health_events WHERE ts < ?`, r.HealthDays},
		{`DELETE FROM service_runs WHERE started_at < ?`, r.RunDays},
		{`DELETE FROM workspace_history WHERE ts < ?`, r.HistoryDays},
		{`DELETE FROM diagnostic_history WHERE ts < ?`, r.HistoryDays},
	} {
		if p.days > 0 {
			if err := del(p.q, cut(p.days)); err != nil {
				return total, err
			}
		}
	}
	return total, nil
}

// Reset deletes all user data (settings, workspaces, services, history) but
// keeps the schema. Callers must confirm with the user first.
func (d *DB) Reset(ctx context.Context) error {
	return d.Tx(ctx, func(tx *sql.Tx) error {
		for _, t := range []string{"logs", "health_events", "activity", "workspace_history", "diagnostic_history", "service_runs", "services", "workspaces", "settings"} {
			if _, err := tx.Exec("DELETE FROM " + t); err != nil {
				return err
			}
		}
		return nil
	})
}
