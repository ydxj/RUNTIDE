# Development progress

Status of the 15 phases of the original mission, and an honest list of what is verified versus not.

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
