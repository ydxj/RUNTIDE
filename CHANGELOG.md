# Changelog

All notable changes to this project are documented here. Format: [Keep a Changelog](https://keepachangelog.com/).

## [Unreleased] - 0.2.0

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
