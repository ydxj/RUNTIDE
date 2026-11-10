package database

// Execution-platform schema (RUNTIDE 0.2). Purely additive: no 0.1 table is
// altered or dropped, so existing user data is untouched.
func init() {
	migrations = append(migrations, migration{2, "execution platform", `
CREATE TABLE projects (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL UNIQUE CHECK (length(name) BETWEEN 1 AND 63),
  root TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  git_root TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE TABLE task_workspaces (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 63),
  task TEXT NOT NULL DEFAULT '',
  state TEXT NOT NULL CHECK (state IN ('CREATED','CONFIGURING','READY','RUNNING','VERIFYING','COMPLETED','FAILED','CANCELLED','INTERRUPTED','REQUIRES_ATTENTION')),
  isolation INTEGER NOT NULL CHECK (isolation BETWEEN 1 AND 4),
  dir TEXT NOT NULL,
  source_dir TEXT NOT NULL,
  worktree INTEGER NOT NULL DEFAULT 0,
  branch TEXT NOT NULL DEFAULT '',
  config_yaml TEXT NOT NULL,
  approved_hash TEXT NOT NULL DEFAULT '',
  final_status TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  UNIQUE (project_id, name)
);
CREATE INDEX idx_tw_project ON task_workspaces(project_id, created_at DESC);
CREATE TABLE executions (
  id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL REFERENCES task_workspaces(id) ON DELETE RESTRICT,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  task TEXT NOT NULL DEFAULT '',
  state TEXT NOT NULL CHECK (state IN ('CREATED','CONFIGURING','READY','RUNNING','VERIFYING','COMPLETED','FAILED','CANCELLED','INTERRUPTED','REQUIRES_ATTENTION')),
  isolation INTEGER NOT NULL,
  runtide_version TEXT NOT NULL DEFAULT '',
  error TEXT NOT NULL DEFAULT '',
  verification TEXT NOT NULL DEFAULT '',
  environment_json TEXT NOT NULL DEFAULT '{}',
  created_at INTEGER NOT NULL,
  started_at INTEGER,
  ended_at INTEGER
);
CREATE INDEX idx_exec_ws ON executions(workspace_id, created_at DESC);
CREATE INDEX idx_exec_state ON executions(state);
CREATE TABLE exec_events (
  id TEXT PRIMARY KEY,
  execution_id TEXT NOT NULL REFERENCES executions(id) ON DELETE CASCADE,
  seq INTEGER NOT NULL,
  ts INTEGER NOT NULL,
  type TEXT NOT NULL,
  source TEXT NOT NULL CHECK (source IN ('observed','reported','inferred')),
  severity TEXT NOT NULL CHECK (severity IN ('info','warn','error')),
  subject TEXT NOT NULL DEFAULT '',
  data_json TEXT NOT NULL DEFAULT '{}',
  UNIQUE (execution_id, seq)
);
CREATE INDEX idx_events_type ON exec_events(execution_id, type);
CREATE TABLE exec_commands (
  id TEXT PRIMARY KEY,
  execution_id TEXT NOT NULL REFERENCES executions(id) ON DELETE CASCADE,
  seq INTEGER NOT NULL,
  kind TEXT NOT NULL,
  name TEXT NOT NULL,
  executable TEXT NOT NULL,
  args_json TEXT NOT NULL,
  cwd TEXT NOT NULL,
  env_keys_json TEXT NOT NULL DEFAULT '[]',
  isolation INTEGER NOT NULL,
  started_at INTEGER NOT NULL,
  ended_at INTEGER,
  exit_code INTEGER,
  pid INTEGER NOT NULL DEFAULT 0,
  cancelled INTEGER NOT NULL DEFAULT 0,
  timed_out INTEGER NOT NULL DEFAULT 0,
  truncated INTEGER NOT NULL DEFAULT 0,
  redacted INTEGER NOT NULL DEFAULT 0,
  stdout_bytes INTEGER NOT NULL DEFAULT 0,
  stderr_bytes INTEGER NOT NULL DEFAULT 0,
  error TEXT NOT NULL DEFAULT '',
  UNIQUE (execution_id, seq)
);
CREATE TABLE exec_output (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  command_id TEXT NOT NULL REFERENCES exec_commands(id) ON DELETE CASCADE,
  ts INTEGER NOT NULL,
  stream TEXT NOT NULL CHECK (stream IN ('stdout','stderr')),
  line TEXT NOT NULL
);
CREATE INDEX idx_output_cmd ON exec_output(command_id, id);
CREATE TABLE managed_processes (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  execution_id TEXT NOT NULL REFERENCES executions(id) ON DELETE CASCADE,
  command_id TEXT NOT NULL,
  pid INTEGER NOT NULL,
  start_time_ns INTEGER NOT NULL,
  exe TEXT NOT NULL DEFAULT '',
  started_at INTEGER NOT NULL,
  ended_at INTEGER
);
CREATE INDEX idx_mp_open ON managed_processes(ended_at) WHERE ended_at IS NULL;
CREATE TABLE exec_services (
  execution_id TEXT NOT NULL REFERENCES executions(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  command_id TEXT NOT NULL DEFAULT '',
  health TEXT NOT NULL DEFAULT 'unknown',
  health_detail TEXT NOT NULL DEFAULT '',
  restarts INTEGER NOT NULL DEFAULT 0,
  last_state TEXT NOT NULL DEFAULT '',
  updated_at INTEGER NOT NULL,
  PRIMARY KEY (execution_id, name)
);
CREATE TABLE file_changes (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  execution_id TEXT NOT NULL REFERENCES executions(id) ON DELETE CASCADE,
  path TEXT NOT NULL,
  change TEXT NOT NULL CHECK (change IN ('added','modified','deleted','renamed')),
  old_path TEXT NOT NULL DEFAULT '',
  size_before INTEGER NOT NULL DEFAULT 0,
  size_after INTEGER NOT NULL DEFAULT 0,
  hash_before TEXT NOT NULL DEFAULT '',
  hash_after TEXT NOT NULL DEFAULT '',
  mod_time TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_fc_exec ON file_changes(execution_id);
CREATE TABLE file_baselines (
  execution_id TEXT NOT NULL REFERENCES executions(id) ON DELETE CASCADE,
  path TEXT NOT NULL,
  content BLOB NOT NULL,
  PRIMARY KEY (execution_id, path)
);
CREATE TABLE verification_results (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  execution_id TEXT NOT NULL REFERENCES executions(id) ON DELETE CASCADE,
  run INTEGER NOT NULL DEFAULT 1,
  seq INTEGER NOT NULL,
  name TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  type TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('passed','failed','skipped','timed_out','not_applicable')),
  required INTEGER NOT NULL DEFAULT 1,
  started_at INTEGER NOT NULL,
  ended_at INTEGER NOT NULL,
  exit_code INTEGER,
  output TEXT NOT NULL DEFAULT '',
  error TEXT NOT NULL DEFAULT '',
  needs_json TEXT NOT NULL DEFAULT '[]',
  evidence_json TEXT NOT NULL DEFAULT '[]'
);
CREATE INDEX idx_vr_exec ON verification_results(execution_id, run, seq);
CREATE TABLE receipts (
  id TEXT PRIMARY KEY,
  execution_id TEXT NOT NULL DEFAULT '',
  project_name TEXT NOT NULL DEFAULT '',
  path TEXT NOT NULL,
  sha256 TEXT NOT NULL,
  size INTEGER NOT NULL,
  imported INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL DEFAULT '',
  signed_by TEXT NOT NULL DEFAULT '',
  manifest_json TEXT NOT NULL,
  created_at INTEGER NOT NULL
);
CREATE UNIQUE INDEX idx_receipts_sha ON receipts(sha256);
CREATE TABLE audit_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  ts INTEGER NOT NULL,
  actor TEXT NOT NULL,
  action TEXT NOT NULL,
  subject TEXT NOT NULL DEFAULT '',
  detail TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_audit_ts ON audit_events(ts DESC);
`})
}
