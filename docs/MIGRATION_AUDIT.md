# Migration audit: RUNTIDE 0.1 (process manager) to RUNTIDE 0.2 (execution platform)

Audited on the working tree before any 0.2 change (22 Go packages, ~13k lines of Go, 27 frontend tests, 6 Playwright tests;
`git status` showed 0.1 as partly committed by the owner and partly untracked). Everything below was verified by running the
code, not inferred from documentation.

## What exists and actually works

| Area | State | Verdict |
| --- | --- | --- |
| `internal/platform/windows` (toolhelp, PEB cmdline, socket tables, Job Objects, console CTRL_BREAK, tray, autostart) | Works; unit + integration tested | **Keep**: it is the foundation for managed execution |
| `internal/process`, `ports`, `resources` | Real data, tested | **Keep** (Services/Processes/Ports/Resources pages) |
| `internal/supervisor` | Job-Object supervised long-running services, restart policy with back-off, graceful stop, tree cleanup; tested | **Keep and reuse** as the service half of an execution |
| `internal/health` | HTTP/TCP/process/command probes + state machine | **Keep**; reused by verification |
| `internal/logs` | Bounded ring + redaction + SQLite batches | **Keep** for live stacks; executions get their own bounded, per-command store |
| `internal/redact` | Secret masking | **Keep**; extended for command arguments |
| `internal/config` | Strict YAML schema, path containment, cycle detection | **Keep**; service/health schema reused inside execution workspaces |
| `internal/database` | SQLite, numbered migrations, backup/restore, corruption quarantine | **Keep**; 0.2 adds only *additive* migrations |
| `internal/workspaces` | Named groups of services with dependency-ordered start/stop and approval hashing | **Keep, rename the concept to "Stacks"**: it is a service group, not a task environment |
| `internal/tools`, `envfile`, `diagnostics`, `docker`, `wsl`, `winservices` | Real, tested | **Keep** |
| `internal/api` + `app.Engine.Dispatch` | Loopback+token RPC shared by CLI and desktop | **Keep** and extend with the new domain methods |
| `cmd/runtide` (cobra) | Works; background daemon model | **Extend**; the old `workspace` command group moves to `stack` |
| Desktop (Wails + React) | Works; real data; design system, command palette | **Keep the shell and design system**; add execution pages |
| Packaging | Inno Setup installer, portable zip; verified silent install/upgrade/uninstall | **Keep**; extend with CLI-only server artefacts |

## Not simulated

No production code path returns fabricated data. Mocks exist only in tests (fake transport in the frontend, canned `docker ps`
output in the Docker tests).

## Gaps relative to the 0.2 product

There is no notion of a *task*, an *execution*, recorded evidence, verification pipelines, receipts, comparison,
file-change tracking, Git worktrees, a server, user accounts or MCP. 0.1 manages what is running; 0.2 must also explain what
happened and let someone else verify it.

## Naming conflict resolved

0.1 used "workspace" for a group of services (`runtide workspace up`). 0.2 uses "workspace" for the environment of one task, as
in the product mission. The old concept is renamed **Stack** in CLI and UI (`runtide stack up`); internal Dispatch method names
`workspaces.*` and database tables `workspaces`/`services` stay unchanged so no stored data needs conversion.

## Data and migration strategy

* `%LOCALAPPDATA%\Runtide\runtide.db` exists on developer machines running 0.1 (workspaces, services, logs, history). 0.2 adds
  migrations v2+ that only `CREATE TABLE`/`CREATE INDEX`: no existing table is altered or dropped. A test opens a v1 database,
  migrates it and proves the old rows survive.
* Before applying a migration to an existing non-empty database, the engine makes a `pre-migration-v<N>` copy in `backups\`
  (`VACUUM INTO`). A schema newer than the binary supports is refused.
* The v1 CLI contract is kept except the `workspace` -> `stack` rename (documented in the changelog).

## Components removed or rewritten

None are removed. The only behaviour changes are the CLI/UI rename and the `Workspaces` navigation entry becoming `Stacks`.
