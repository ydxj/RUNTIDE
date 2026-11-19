# Product specification

**RUNTIDE** - *Everything executed. Everything verified.* A self-hosted developer execution and verification platform, built on a local-first Windows infrastructure control center (the 0.1 product: *Everything running. Under control.*).

## Principles

1. Real data only; nothing simulated in production code. Unavailable things are reported as unavailable, with a reason.
2. Local first: no accounts, telemetry or network calls. The optional self-hosted server is the only networked component and is off by default.
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
| G | Stacks | (0.1 "Workspace Manager", renamed) YAML schema, validation, dependency-ordered execution, approval |
| H | Environment Inspector | Tool detection by execution |
| I | Env file inspector | `.env` analysis, diff, example generation with backups |
| J | Health Monitor | HTTP/TCP/process/command checks |
| K | Doctor | Rule-based diagnostics with opt-in fixes |
| L | Docker | Status, containers, start/stop, Compose awareness |
| M | WSL | Read-only distribution status |
| N | Windows integration | Tray, notifications, opt-in autostart, graceful shutdown |

## Execution platform modules (0.2)

| ID | Module | Summary | Doc |
| --- | --- | --- | --- |
| O | Projects and task workspaces | Validated lifecycle `CREATED → CONFIGURING → READY → RUNNING → VERIFYING → COMPLETED` plus `FAILED`, `CANCELLED`, `INTERRUPTED`, `REQUIRES_ATTENTION`; approval bound to the configuration hash | [EXECUTION_CAPSULES.md](EXECUTION_CAPSULES.md) |
| P | Execution Capsules | Managed, git worktree, Docker, WSL2 with honest isolation statements | [EXECUTION_CAPSULES.md](EXECUTION_CAPSULES.md) |
| Q | Execution Recorder | Provenance-tagged timeline, bounded redacted output, file-change intelligence, orphan detection | [EXECUTION_RECORDING.md](EXECUTION_RECORDING.md) |
| R | Verification engine | Pipelines with dependencies and five statuses | [VERIFICATION.md](VERIFICATION.md) |
| S | Run Receipts | Versioned, hash-listed `.runtide.zip`; export, import, verify, inspect, HTML report, optional Ed25519 signing | [RUN_RECEIPTS.md](RUN_RECEIPTS.md) |
| T | Reproduction and replay | Four reproducibility classes; approval-gated replay in an isolated worktree | [REPRODUCTION.md](REPRODUCTION.md) |
| U | Comparison | Verdicts, commands, files, failures, environment between executions or receipts | [RUN_RECEIPTS.md](RUN_RECEIPTS.md) |
| V | MCP server | Three permission levels; agents cannot approve commands | [MCP_INTEGRATION.md](MCP_INTEGRATION.md) |
| W | Self-hosted server | Accounts, project roles, audit, rate limits, OpenAPI; stores receipts, never executes | [SELF_HOSTING.md](SELF_HOSTING.md) |

Not implemented: PostgreSQL for the server, PDF reports, a server web UI. See [DEVELOPMENT_PROGRESS.md](DEVELOPMENT_PROGRESS.md).

## Non-goals

Replacing Docker, a terminal, an IDE or a process monitor for production servers; editing global environment
variables; managing remote machines; cloud sync.

## Quality bar

See [TESTING.md](TESTING.md) and [SECURITY_MODEL.md](SECURITY_MODEL.md). Known gaps are tracked in
[DEVELOPMENT_PROGRESS.md](DEVELOPMENT_PROGRESS.md).
