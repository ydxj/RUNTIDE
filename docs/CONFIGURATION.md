# Configuration and data

## Locations

| What | Where |
| --- | --- |
| Data directory | `%LOCALAPPDATA%\Runtide` (override with the `RUNTIDE_HOME` environment variable) |
| Database | `runtide.db` (+ `-wal`, `-shm` while running) |
| Backups | `backups\runtide-<timestamp>.db` |
| Engine endpoint | `api.json` (address, token, pid) while an engine runs; mode 0600; removed on clean shutdown |
| Quarantined corrupt DB | `runtide.db.corrupt-<timestamp>` |

## Settings (`runtide config`, Settings page)

| Key | Default | Meaning |
| --- | --- | --- |
| `theme` | `light` | `light`, `dark`, `system` |
| `refreshSec` | 3 | UI polling interval (paused while the window is hidden) |
| `sampleSec` | 2 | Resource sampling interval |
| `logMemoryLines` | 5000 | Lines per service kept in memory (applies on restart of the engine) |
| `logRetentionDays` / `logMaxRows` | 7 / 200000 | Persisted log retention |
| `redaction` / `extraRedaction` | true / none | Secret masking and extra regular expressions |
| `activityDays` / `healthDays` / `historyDays` | 30 / 14 / 90 | Retention of activity, health events, run/workspace/diagnostic history |
| `minimizeToTray`, `notifications`, `startWithWindows`, `stopServicesOnExit` | true, true, false, true | Windows integration |

Settings are validated (ranges, regexes) before they are saved.

## Database

SQLite, WAL, foreign keys on, pure-Go driver. Migrations are numbered and transactional (`schema_migrations`).

| Table | Purpose |
| --- | --- |
| `settings` | key/value application settings |
| `workspaces` | name, root, description, canonical YAML, approval hash |
| `services` | standalone service definitions (JSON) |
| `service_runs` | every launch: pid, start/end, exit code, reason |
| `logs` | persisted log lines (indexed by service and time) |
| `health_events` | health transitions |
| `activity` | notable events (starts, crashes, kills, fixes) |
| `workspace_history` | save/approve/up/down outcomes |
| `diagnostic_history` | past Doctor reports |

Retention runs at startup and hourly. `runtide config backup|restore|reset` and the Settings page manage data; a reset
makes a backup first and requires confirmation. Secrets are not stored except values you type into a service's
environment field (prefer `env_file`, which is read at launch and never stored).

## Environment variables

`RUNTIDE_HOME` (data directory), `NO_COLOR` (disable CLI color).
