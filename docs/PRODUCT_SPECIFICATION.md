# Product specification

**RUNTIDE** - *Everything running. Under control.* A local-first Windows developer infrastructure control center.

## Principles

1. Real data only; nothing simulated in production code. Unavailable things are reported as unavailable, with a reason.
2. Local first: no accounts, telemetry or network calls.
3. Safe by default: never elevate, never run imported definitions without explicit approval, confirm destructive actions, redact secrets.
4. One engine, several front doors: desktop, CLI and API share the same logic.
5. Integrate with Docker/WSL/etc.; never require them.

## Modules (all implemented)

| ID | Module | Summary |
| --- | --- | --- |
| A | Overview | Measured machine/services/workspace summary with warnings |
| B | Process Explorer | List, tree, details, protected-aware termination |
| C | Port Manager | TCP/UDP sockets with owners, exposure, conflicts |
| D | Service Manager | Supervised services, restart policy, run history; native Windows services kept separate |
| E | Log Center | Live, searchable, bounded, redacted, persisted logs |
| F | Resource Monitor | CPU/memory history, disks, interfaces |
| G | Workspace Manager | YAML schema, validation, dependency-ordered execution, approval |
| H | Environment Inspector | Tool detection by execution |
| I | Env file inspector | `.env` analysis, diff, example generation with backups |
| J | Health Monitor | HTTP/TCP/process/command checks |
| K | Doctor | Rule-based diagnostics with opt-in fixes |
| L | Docker | Status, containers, start/stop, Compose awareness |
| M | WSL | Read-only distribution status |
| N | Windows integration | Tray, notifications, opt-in autostart, graceful shutdown |

## Non-goals

Replacing Docker, a terminal, an IDE or a process monitor for production servers; editing global environment
variables; managing remote machines; cloud sync.

## Quality bar

See [TESTING.md](TESTING.md) and [SECURITY_MODEL.md](SECURITY_MODEL.md). Known gaps are tracked in
[DEVELOPMENT_PROGRESS.md](DEVELOPMENT_PROGRESS.md).
