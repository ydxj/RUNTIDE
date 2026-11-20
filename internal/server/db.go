// Package server is the self-hosted RUNTIDE server: accounts, projects, a
// receipt/artifact store and an authenticated, versioned HTTP API. It stores
// and serves Run Receipts; it deliberately has no endpoint that executes
// commands or manages processes, so a compromised account cannot run code.
package server

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

var serverMigrations = []string{`
CREATE TABLE users (
  id TEXT PRIMARY KEY,
  username TEXT NOT NULL UNIQUE COLLATE NOCASE CHECK (length(username) BETWEEN 3 AND 64),
  password_hash TEXT NOT NULL,
  role TEXT NOT NULL CHECK (role IN ('admin','member','viewer')),
  disabled INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL,
  last_login INTEGER
);
CREATE TABLE tokens (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  kind TEXT NOT NULL CHECK (kind IN ('session','api')),
  name TEXT NOT NULL DEFAULT '',
  token_hash TEXT NOT NULL UNIQUE,
  created_at INTEGER NOT NULL,
  expires_at INTEGER,
  last_used INTEGER
);
CREATE INDEX idx_tokens_user ON tokens(user_id);
CREATE TABLE projects (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL UNIQUE COLLATE NOCASE CHECK (length(name) BETWEEN 1 AND 63),
  description TEXT NOT NULL DEFAULT '',
  created_by TEXT NOT NULL REFERENCES users(id),
  created_at INTEGER NOT NULL
);
CREATE TABLE project_members (
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  role TEXT NOT NULL CHECK (role IN ('owner','writer','reader')),
  PRIMARY KEY (project_id, user_id)
);
CREATE TABLE executions (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  execution_id TEXT NOT NULL,
  receipt_sha TEXT NOT NULL,
  task TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL,
  verification TEXT NOT NULL DEFAULT '',
  reproducibility TEXT NOT NULL DEFAULT '',
  isolation TEXT NOT NULL DEFAULT '',
  signer TEXT NOT NULL DEFAULT '',
  runtide_version TEXT NOT NULL DEFAULT '',
  size INTEGER NOT NULL,
  receipt_created_at INTEGER NOT NULL,
  uploaded_at INTEGER NOT NULL,
  uploaded_by TEXT NOT NULL REFERENCES users(id),
  UNIQUE (project_id, receipt_sha)
);
CREATE INDEX idx_exec_project ON executions(project_id, uploaded_at DESC);
CREATE TABLE audit (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  ts INTEGER NOT NULL,
  actor TEXT NOT NULL,
  ip TEXT NOT NULL DEFAULT '',
  action TEXT NOT NULL,
  subject TEXT NOT NULL DEFAULT '',
  detail TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_audit_ts ON audit(ts DESC);
`}

// openDB opens or creates the server database and applies migrations.
func openDB(dir string) (*sql.DB, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "server.db")
	dsn := "file:" + filepath.ToSlash(path) + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, applied_at INTEGER NOT NULL)`); err != nil {
		db.Close()
		return nil, err
	}
	var cur int
	_ = db.QueryRow(`SELECT COALESCE(MAX(version),0) FROM schema_migrations`).Scan(&cur)
	if cur > len(serverMigrations) {
		db.Close()
		return nil, fmt.Errorf("server database schema %d is newer than this server supports (%d)", cur, len(serverMigrations))
	}
	for i := cur; i < len(serverMigrations); i++ {
		tx, err := db.Begin()
		if err != nil {
			db.Close()
			return nil, err
		}
		if _, err := tx.Exec(serverMigrations[i]); err != nil {
			tx.Rollback()
			db.Close()
			return nil, fmt.Errorf("server migration %d: %w", i+1, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations VALUES(?,?)`, i+1, time.Now().Unix()); err != nil {
			tx.Rollback()
			db.Close()
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			db.Close()
			return nil, err
		}
	}
	return db, nil
}
