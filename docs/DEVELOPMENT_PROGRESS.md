# Development progress

## 0.9.2: fix release

Fixes the console windows that flashed whenever the background engine ran a helper program (tool probes, Docker, WSL, git, health
checks), which made RUNTIDE look like malware. All helpers now start without a visible window through `internal/procutil`;
measured 41 windows before and 0 after, and a test guards against new launches that skip the helper. Details:
[RELEASE_NOTES.md](RELEASE_NOTES.md).

---

## 0.9.1: fix release

CI (all jobs green) found a real defect that local testing could not: the server container could not write its data directory as a
non-root user. Fixed, together with 18 Dependabot alerts and the CI line-ending problem. The container image is now built and
health-checked in CI. Docker task isolation and a real MCP client remain unexercised. Details: [RELEASE_NOTES.md](RELEASE_NOTES.md).

---

## 0.9.0: release candidate (1.0.0 productization)

Plan and outcome: [V1_IMPLEMENTATION_PLAN.md](V1_IMPLEMENTATION_PLAN.md). Evidence: [TESTING.md](TESTING.md). Everything below was
exercised on the build machine; nothing has been published or committed by the assistant that did this work.

| Phase | Status | Remaining |
| --- | --- | --- |
| A Audit and plan | Done | none |
| B Engine reliability | Done, except Docker run | A real container run needs Docker Desktop running (the test exists and skips honestly) |
| C Recorder and receipts | Done | none known |
| D Verification and comparison | Done | none known |
| E Desktop application | Done | none known; the sidebar scrolls below about 900 px height |
| F CLI and MCP | Done, except a real MCP client | Try Claude Code and Codex with `runtide mcp config` |
| G Server and deployment | Done; image built and health-checked in CI | none known |
| H Isolation validation | Done | Docker run (see B) |
| I Security and QA | Done as a self-review | No external audit |
| J Installation and packaging | Done | Clean-VM and Windows 10 install not tested |

Defects found and fixed during this phase (details in [SECURITY_REVIEW.md](SECURITY_REVIEW.md) and [RELEASE_NOTES.md](RELEASE_NOTES.md)):
symlink/junction escape from workspaces (high), concurrent-upload HTTP 500, Docker reported running when stopped, server image could
not build, phantom receipt entries after comparing executions (plus a startup cleanup for databases that already have them),
replay pre-flight crashed the UI on `null` lists, a flaky capsule test.

**Version decision:** 0.9.0, not 1.0.0. Docker execution, a real MCP client and the container image have not been exercised
([KNOWN_LIMITATIONS.md](KNOWN_LIMITATIONS.md)).

---

## 0.2: execution and verification platform

| Area | Status | Evidence |
| --- | --- | --- |
| Migration audit and data preservation | Done | [MIGRATION_AUDIT.md](MIGRATION_AUDIT.md); `internal/database` migration + backup tests |
| Task workspaces, lifecycle, approval | Done | `internal/domain`, `internal/execution` tests |
| Git worktrees (never touch your branch/tree) | Done | `internal/execution` tests with real git repositories |
| Capsules: managed, worktree | Done, tested | `internal/capsule`, `internal/execution` |
| Capsules: Docker, WSL2 | **Partial**: wrapping + validation unit-tested; never run end to end | `internal/capsule` tests |
| Execution Recorder (provenance, bounds, redaction, crash recovery, orphans) | Done | `internal/execution`, `internal/redact` tests |
| File change intelligence | Done | `internal/files` tests (secret exclusion) |
| Verification engine | Done | `internal/verification` tests |
| Run Receipts: export, import, verify, inspect, HTML, signing | Done | `internal/receipts` tests |
| Receipt comparison | Done (CLI, API, desktop) | `internal/receipts`, Vitest, Playwright screenshots |
| Reproduction classes and replay | Done on the recording machine; **cross-machine replay not exercised** | `internal/receipts` tests |
| PDF reports | **Not implemented** (HTML only) | n/a |
| Self-hosted server (SQLite, auth, roles, rate limit, audit, OpenAPI, loopback default) | Done | `internal/server` tests, built binary smoke test |
| Server on PostgreSQL | **Not implemented** | n/a |
| Server web UI | **Not implemented** (API + CLI only) | n/a |
| Dockerfile / compose | **Written, not built or run** locally (no Docker engine); CI job builds and probes it | `deployments/`, `.github/workflows/ci.yml` |
| CLI-first commands | Done | `cmd/runtide` tests, `tests/scenario` |
| MCP server (3 permission levels) | Done, tested with the Go SDK client and a real stdio handshake; **not run from Claude Code or Codex** | `internal/mcpserver` tests |
| Desktop pages (Overview, Projects, Workspaces, Executions, Live Activity, Verification, Receipts, Compare, Services, Stacks, Environment, Integrations, Settings, plus the 0.1 machine pages) | Done | 33 Vitest tests, 7 Playwright tests, real screenshots |
| Distributions: installer, portable, CLI zip, server zip | Built; installer **not** install-tested on a clean machine for 0.2 | `build/package.ps1` run |
| Documentation | Done (this folder) | |

Test results are in [TESTING.md](TESTING.md). Nothing was committed, staged or pushed by the assistant that built this.

### Known limitations and open items (0.2)

1. Docker/WSL isolation, the Docker deployment, Claude Code and Codex integrations are untested end to end (see above).
2. Receipts are tamper-evident, not tamper-proof; redaction is pattern based. Review receipts before sharing.
3. Replay supports Windows-recorded receipts only and needs a local git checkout containing the recorded commit.
4. The sidebar scrolls on small windows (the last items need scrolling at 800 px height).
5. Unsigned binaries; product name not legally cleared.
6. `C:untide-demo` (screenshot demo data) must be deleted by the owner; the assistant's safety checks blocked deleting a root-level directory.

---

## 0.1: infrastructure control center
Status of the 15 phases of the original 0.1 mission, and an honest list of what is verified versus not.

| Phase | Status | Evidence |
| --- | --- | --- |
| 1 Architecture | Done | [ARCHITECTURE.md](ARCHITECTURE.md) |
| 2 Core infrastructure (config, SQLite, migrations, redaction, paths) | Done | `internal/config`, `database`, `redact` tests |
| 3 Windows process/network integration | Done | `internal/platform/windows`, `process`, `ports` tests |
| 4 Supervisor and services | Done | `internal/supervisor` tests (graceful, force, tree, restart limits) |
| 5 Workspace engine | Done | `internal/workspaces` tests |
| 6 Logs, resources, health | Done | `internal/logs`, `resources`, `health` tests |
| 7 Environment inspector and Doctor | Done | `internal/tools`, `envfile`, `diagnostics` tests |
| 8 Docker and WSL | Done (Docker verified via canned output) | `internal/docker`, `wsl` tests |
| 9 CLI | Done | `cmd/runtide` tests + manual smoke run with a real 2-service workspace |
| 10 Desktop frontend | Done | 27 Vitest tests, 6 Playwright tests, real-window screenshot |
| 11 Persistence verification | Done | backup/restore/corruption/prune tests |
| 12 Security review | Done (self-review) | [SECURITY_MODEL.md](SECURITY_MODEL.md); no external audit |
| 13 Automated testing | Done | [TESTING.md](TESTING.md) |
| 14 Packaging and installer | Done on build machine | portable zip + Inno Setup installer; install/upgrade/uninstall tested silently |
| 15 Documentation and QA | Done | this folder |

## Verified manually on the build machine

* Two-service workspace via the CLI: dependency order, health gating, port attribution to managed services, log redaction,
  restart, stop. Killing the daemon process abruptly freed the service ports (Job Object cleanup).
* Desktop app launches, renders real data (screenshot) and its bridge works.

## Known limitations and open items

1. **Not verified**: clean Windows install/VM, Windows 10, tray icon and notifications visually, start-with-Windows toggling
   (the registry entry was deliberately not created on the dev machine), attach-to-existing-daemon behaviour of the app,
   single-instance focus, CI workflows.
2. Per-interface network throughput is not collected.
3. Graceful stop reaches console processes only (others are terminated after the grace period).
4. Command-line reading uses the x64 PEB layout; 32-bit target processes may show an empty command line.
5. Port ownership of a service is attributed through the parent/child chain; processes re-parented away from the service
   (daemonised) are not attributed.
6. Binaries are unsigned. No auto-update exists (by design for now).
7. The Wails tray uses a custom Win32 implementation; it has no unit tests.
8. Wails WebView2 remote debugging could not be enabled (the loader overrides the environment variable), so end-to-end tests
   use the `tests/e2ehost` bridge in Edge rather than the native shell.
9. `./...` includes a Go file inside `frontend/node_modules`; commands and CI use explicit package paths.

## Name check

A quick web search found no software, repository, package or trademark named "runtide" (results were unrelated). This is
**not** legal clearance: check USPTO/EUIPO, npm, PyPI, crates.io and GitHub directly before launching, and decide on the
repository location. The name was not changed.

## Recommended next steps

Code signing, a clean-VM install test, winget/Scoop manifests, Linux/macOS platform layers, network throughput counters,
Compose-aware workspace services, signed release workflow, accessibility audit with a screen reader.
