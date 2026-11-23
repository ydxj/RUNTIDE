# Changelog

All notable changes to this project are documented here. Format: [Keep a Changelog](https://keepachangelog.com/).

## [Unreleased] - 0.9.0 (release candidate)

Reusable verification profiles (desktop, CLI, MCP); project edit/archive/defaults; explicit receipt `integrity` and
`signatureState`; replay pre-flight (dependencies, destructive operations, permissions) with four outcomes and a desktop approval
flow; timeline events `output.captured` and `receipt.generated`; execution retention and limits; isolation availability in the
Environment page and `runtide diagnostics isolation`; CLI groups `mcp` and `diagnostics`; `runtide run prune`; server structured
request log and Linux build; Overview project health, verification outcomes and receipt activity; WSL2 isolation tested for real;
security review and new documentation. Database schema v3 (additive, backed up first).

Fixed: junctions/symlinks could be followed outside a workspace (high severity); comparing executions left phantom receipt-list entries; null lists crashed the replay pre-flight page; concurrent uploads of one receipt could return
HTTP 500; Docker reported as running when stopped; the server image could not have built (Windows-only imports); a flaky capsule
test. See [docs/SECURITY_REVIEW.md](docs/SECURITY_REVIEW.md) and [docs/RELEASE_NOTES.md](docs/RELEASE_NOTES.md).

Still open (why this is not 1.0.0): Docker execution and the server image were never run for real, and no real MCP client was
available. See [docs/KNOWN_LIMITATIONS.md](docs/KNOWN_LIMITATIONS.md).

## 0.2.0

Execution and verification platform. **Renamed:** the 0.1 "workspace" is now a *stack* (`runtide stack …`, **Stacks** page);
"workspace" now means a task's execution workspace. Stored data is unchanged (a backup is written before the v2 migration).

Added: projects and task workspaces with a validated lifecycle and approval; Execution Capsules (managed, git worktree, Docker,
WSL2); Execution Recorder with provenance, bounds, redaction and file-change intelligence; verification pipelines; Run Receipts
(export, import, verify, inspect, HTML report, Ed25519 signing, comparison, replay) and reproducibility classes; orphan detection
after crashes; `runtide-mcp` (MCP server, three permission levels); `runtide-server` (accounts, roles, audit, rate limits,
OpenAPI, SQLite); desktop pages Projects, Workspaces, Executions, Live Activity, Verification, Receipts, Compare, Integrations;
Dockerfile and compose for the server; new documentation.

Not implemented: PostgreSQL for the server, PDF reports, a web UI for the server.

## 0.1.0

First implementation: process explorer, port manager, supervised services with Job Objects, workspaces with
dependency ordering and approval gating, live logs with redaction, health checks, resource monitor, environment and
`.env` inspector, Doctor, Docker/WSL/Windows-service integrations, tray and notifications, CLI, SQLite persistence
with backup/restore, Wails desktop app, Vitest/Go/Playwright test suites, CI workflows and Inno Setup installer script.
