# Database

RUNTIDE keeps everything in one **SQLite** file (`runtide.db`, WAL mode, pure-Go driver `modernc.org/sqlite`) in its data
directory: `%LOCALAPPDATA%\Runtide`, or `RUNTIDE_HOME` when set. PostgreSQL is **not** used by the desktop app or CLI, and is not
implemented for the server either (see [SELF_HOSTING.md](SELF_HOSTING.md)).

## Schema

Version 1 (RUNTIDE 0.1, unchanged): `settings`, `workspaces` (now shown as **Stacks**), `services`, `service_runs`, `logs`,
`health_events`, `activity`, `workspace_history`, `diagnostic_history`.

Version 2 (0.2, additive): `projects`, `task_workspaces`, `executions`, `exec_events`, `exec_commands`, `exec_output`,
`managed_processes`, `exec_services`, `file_changes`, `file_baselines`, `verification_results`, `receipts`, `audit_events`.

Task workspaces and executions never reuse the 0.1 `workspaces` table: that table keeps meaning "stack". Executions reference
their workspace and project; events, commands, output, processes, services, file changes and verification results reference
their execution and are deleted with it. A workspace with recorded executions cannot be deleted.

## Migrations and your data

* Migrations are numbered and recorded in `schema_migrations`.
* **Before any migration runs on a non-empty database, RUNTIDE writes a consistent copy** to
  `backups\pre-migration-v<N>-<timestamp>.db` (`VACUUM INTO`). If the backup fails, the migration does not run.
* A database written by a **newer** RUNTIDE is refused rather than downgraded.
* A corrupt database is moved aside and a fresh one is created; the original is kept.
* Tests build a real version-1 database with data, migrate it and assert that every row survived (`internal/database`).

## Retention and bounds

Output, events and history are bounded per execution (see [EXECUTION_RECORDING.md](EXECUTION_RECORDING.md)) and pruned by the
retention settings (`historyDays`, `activityDays`, …). `runtide config` shows paths and counts; `runtide config reset` makes a
backup first, then deletes data.

## Backup and restore

`runtide config backup` writes a consistent copy into the backups folder; `runtide config restore <backup-file>` restores one.
A candidate is validated (integrity check and schema version) before it replaces the live database.
