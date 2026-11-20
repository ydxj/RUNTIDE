# Architecture

RUNTIDE is one Go engine with three front doors: the desktop app, the `runtide` CLI and a loopback API. All of
them call the same method dispatcher, so behaviour cannot drift between interfaces.

```
                 ┌────────────────────┐        ┌───────────────────────┐
  Wails WebView  │  React + TS UI     │        │  RUNTIDE-Desktop.exe (CLI)     │
  (WebView2)     │  features/*        │        │  cobra commands        │
                 └─────────┬──────────┘        └──────────┬─────────────┘
                           │ one Wails binding             │ loopback HTTP + bearer token
                           │ App.Call(method, json)        │ (or in-process for read-only commands)
                 ┌─────────▼───────────────────────────────▼─────────────┐
                 │  internal/app   Engine.Dispatch(method, params)        │
                 └───┬──────┬───────┬────────┬─────────┬──────────┬──────┘
                     │      │       │        │         │          │
              workspaces  supervisor logs  health  diagnostics  process/ports/resources
                     │      │       │        │         │          │
                     └──────┴──┬────┴────────┴─────────┘          │
                               │                                   │
                        internal/database (SQLite)      internal/platform/windows
                                                        (Win32: toolhelp, iphlpapi, jobs, console, tray)
```

## Packages

| Package | Responsibility |
| --- | --- |
| `internal/platform/windows` | The only place that talks to Win32: process snapshots, PEB command lines, `GetExtendedTcpTable`/`UdpTable`, job objects, console control events, system times/memory/disks, tray icon, autostart registry key. |
| `internal/process` | Process explorer: CPU sampling by delta, tree building, protection classification, safe termination with PID-reuse guard. |
| `internal/ports` | Joins the socket tables with process info; exposure and conflict detection. |
| `internal/resources` | One shared sampler with a bounded ring of samples and a configurable interval. |
| `internal/supervisor` | Launches services inside Job Objects, restart policy with back-off, graceful stop, readiness waiting, run history. |
| `internal/health` | HTTP / TCP / process / command probes and a per-service state machine (start period, failure threshold). |
| `internal/logs` | Bounded per-service ring buffers, redaction, live subscriptions that never block a process pipe, batched SQLite persistence. |
| `internal/config` | Workspace/service schema, strict YAML parsing, validation, path containment, dependency ordering and cycle detection. |
| `internal/workspaces` | Storage, approval-gated execution, dependency-ordered start/stop with rollback, status and history. |
| `internal/envfile` | `.env` parsing, diffing, example generation and backed-up writes. |
| `internal/tools` | Runtime detection by executing real binaries. |
| `internal/diagnostics` | Doctor rules and previewable opt-in fixes. |
| `internal/docker`, `internal/wsl` | Optional integrations through the existing `docker` / `wsl.exe` CLIs. |
| `internal/winservices` | Native Windows services via the Service Control Manager (separate from managed services). |
| `internal/database` | SQLite open/migrate/backup/restore/prune/reset and corruption recovery. |
| `internal/redact` | Secret masking shared by logs, env inspection and the UI. |
| `internal/api` | Loopback HTTP server and client used by the CLI (and by a second desktop instance). |
| `internal/desktop` | Wails `App`: bindings, tray, notifications, event forwarding. No business logic. |
| `internal/app` | `Engine` that wires everything together and implements `Dispatch`. |

Dependencies point downward only: `app` → feature packages → `platform/windows`. Feature packages never import
`app`, `api` or `desktop`.

## Three kinds of "things that run"

RUNTIDE keeps these strictly separate (they have different risks and different controls):

1. **Managed services**: processes RUNTIDE started. Supervised, restarted per policy, logs captured.
2. **External processes**: everything else visible in the Process Explorer. Inspectable; termination only after
   confirmation and never for protected processes. Descendants of managed services are attributed to the service.
3. **Native Windows services**: listed read-only; start/stop only on explicit request, needs rights the user
   already holds, protected core services are refused.

## Process supervision

* The child is created `CREATE_SUSPENDED | CREATE_NO_WINDOW`, assigned to a fresh Job Object (`KILL_ON_JOB_CLOSE`),
  then resumed. No descendant can escape the job, and if RUNTIDE dies the OS kills the whole tree
  (verified in the smoke test: killing the daemon frees the service ports).
* Stop = `CTRL_BREAK` delivered to the service's private hidden console, wait `stop_timeout_sec`, then
  `TerminateJobObject`. `taskkill` is never used.
* Exit handling distinguishes user stop, clean exit and crash. Restart policy is `never` (default),
  `on-failure` or `always`, with exponential back-off, a retry cap and forgiveness after 60 s of stable running.
* Stdout/stderr are read from OS pipes by dedicated goroutines; lines are redacted, truncated at 16 KiB and
  broadcast without blocking.

## Why a background engine for the CLI

A service must outlive the CLI command that started it, so long-lived state lives in an engine process: the
desktop app, or `runtide daemon run` (started automatically on first use of `start`, `stack up`, `logs`).
Read-only commands (`processes`, `ports`, `tools`, `doctor`, `env`) run against a private in-process engine when
nothing is running, and against the live engine when one is. The engine publishes `api.json` (address, token, pid)
in the user's private data folder; a stale file is detected by checking the pid.

## Persistence

SQLite (pure-Go `modernc.org/sqlite`, no cgo) in WAL mode with foreign keys on. Schema is migrated by numbered
migrations inside transactions; a newer-than-supported schema refuses to open. See
[CONFIGURATION.md](CONFIGURATION.md) for tables and retention.

## Frontend

`frontend/src`: `app/` (shell, navigation, command palette), `components/ui/` (design system), `features/<page>/`,
`hooks/` (polling that pauses while hidden, event subscription), `services/` (single `call()` transport, formatting),
`stores/` (zustand UI store), `types/` (mirror of engine JSON). Tests replace the transport with a fake engine; the
production build has no mock data anywhere.

## 0.2: execution platform

```
  desktop (Tasks pages)   CLI (run/receipt/workspace)   runtide-mcp (stdio MCP)
            └───────────────┬──────────────────────────────┘
                   Engine.Dispatch  (internal/app/exec_dispatch.go)
                            │
        internal/execution (Manager: lifecycle, recorder, recovery, workspaces)
          ├─ capsule      (managed / worktree / docker / wsl wrappers, clean environment)
          ├─ verification (pipelines, DAG, statuses)
          ├─ files        (baseline + changes, secret exclusion)
          ├─ store        (SQLite access for executions, events, output, receipts)
          └─ receipts     (build, verify, sign, compare, report, replay)

  runtide-server  →  internal/server (HTTP API, auth, roles, audit)   — stores receipts, never executes
```

* `internal/domain` holds the state machine (`CanTransitionExecution`/`CanTransitionWorkspace`), isolation levels and event types.
* Migrations are additive (v2); a pre-migration backup is written first ([DATABASE.md](DATABASE.md)).
* The recorder is the single writer of evidence; receipts are built only from stored evidence.
* The desktop app batches `log` and `exec.output` events (150 ms) before sending them to the UI.
