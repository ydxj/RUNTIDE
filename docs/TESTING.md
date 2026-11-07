# Testing

Nothing here requires administrator rights, and no test terminates a process it did not start.

## Layers

| Layer | Command | Notes |
| --- | --- | --- |
| Go unit + Windows integration + scenario | `go test ./cmd/... ./internal/... ./tests/e2ehost/... ./tests/scenario/... -count=1` | Real Win32 calls: own process listing, sockets opened by the test, Job Objects, real child processes. Mocks are limited to the Docker CLI output in `internal/docker` tests. |
| Static analysis | `gofmt -l`, `go vet`, `golangci-lint run` (see CONTRIBUTING for package lists) | Config in `.golangci.yml`. |
| Frontend | `cd frontend; npm run typecheck; npm run lint; npm run format:check; npm test` | Vitest + React Testing Library with a fake engine transport (tests only). TypeScript strict. |
| End-to-end | `go build -o dist\e2ehost.exe .\tests\e2ehost; go build -o dist\runtide.exe .\cmd\runtide; cd tests\e2e; npm ci; npx playwright test` (specs: `app`, `execution`, `management`) | Playwright drives the built UI in Microsoft Edge against the **real engine** through `tests/e2ehost` (a bridge replacing the Wails binding): creates a workspace through the UI, authorises it, starts two real Node servers in order, checks ports with the OS, reads redacted logs, stops and verifies the ports closed, and talks to the same engine through the CLI. |
| Screenshots | `SCREENSHOTS=1 npx playwright test screenshots.spec.ts` | Regenerates `docs/screenshots` from a real demo workspace. `build/tools/window-screenshot.ps1` captures the native window. |

Key Go tests: supervisor (graceful stop exit code 0 proves CTRL_BREAK delivery, stubborn child force-killed, grandchild dies
with the job, restart policy gives up after exactly `max_retries`, backoff), workspaces (dependency order, reverse stop,
approval gate, rollback, traversal rejection), ports (IPv4/IPv6/UDP ownership of sockets opened by the test), process
(protection rules, own child terminated, self refused), API (token, Origin and Host rejection, loopback bind), database
(migrations, corruption recovery, backup/restore, prune), logs (bounded ring, truncation, slow-consumer drop, LIKE escaping),
redaction, health state machine, env files, diagnostics fixes (never overwrite, never outside roots), CLI (exit codes, JSON,
confirmation).

## Results of the last full run (0.9.0; Windows 11 Pro 10.0.26200, Go 1.27.3, Node 24.14)

| Check | Result |
| --- | --- |
| `gofmt`, `go vet`, `golangci-lint` | clean; 0 issues |
| Go tests (`./cmd ./internal ./tests/e2ehost ./tests/scenario ./build .`) | **183 passed, 0 failed, 1 skipped**, 29 packages |
| The one skipped test | `TestDockerIsolationRunsCommandsInsideAContainer`: Docker Desktop was not running, so **no container was run** |
| Race detector (`go test -race`, Git Bash) | clean on server, store, verification, execution, receipts, receiptcore, capsule, supervisor, mcpserver, files, config |
| Frontend: `tsc --noEmit`, ESLint (0 warnings), Prettier | clean |
| Vitest | **41 passed** (9 files) |
| Playwright (real engine through the `e2ehost` bridge, Microsoft Edge) | **12 passed**; the screenshots spec is skipped unless `SCREENSHOTS=1` and passed separately |
| Installer (`build/tools/test-installer.ps1`) | **28 checks, 0 failed**: fresh install, files, registration, CLI and MCP from the install folder, desktop app window, uninstall, upgrade from the **real 0.2.0 installer** with project, execution and output kept and a pre-migration backup written |
| Linux server | built with `GOOS=linux CGO_ENABLED=0` and run **inside WSL2 Ubuntu** against a receipt made on Windows (part of the Go suite) |
| MCP server | `runtide mcp check` on the built binary: 9 / 14 / 18 tools at `read` / `controlled` / `sensitive`; handshake and read-only calls succeed |
| WSL2 isolation | a real execution inside Ubuntu on WSL2 (part of the Go suite) |

Race detector note: run it from Git Bash with `CGO_ENABLED=1` and `C:\msys64\ucrt64\bin` on `PATH`. From PowerShell on the build machine
ThreadSanitizer failed to start (an address-space allocation error) before running any test, so those runs prove nothing.

What the 0.9 tests added, by area: **security** (hostile arguments, `.cmd` batch targets, junction escapes in verification and
receipt artifacts, log secrecy), **concurrency** (six users, duplicate uploads), **isolation** (WSL2 real run, no-fallback for WSL
and Docker, strict Docker readiness), **receipts** (explicit integrity/signature states, outcome classification, pre-flight,
non-null lists, no phantom entries after comparison), **projects and profiles** (store, engine, CLI, MCP, desktop), **retention**
(prune semantics), **UI** (edit/archive, profiles, replay approval gating, isolation panel, settings, and a real-data pre-flight
that would have caught a crash found during development).

## Not covered / could not be executed here

* Clean-VM install, Windows 10, and any machine without WebView2.
* Tray icon, balloon notifications, minimise-to-tray and start-with-Windows are implemented but were not asserted by
  automated tests (they depend on the interactive desktop; the native app was only verified to launch and render).
* Docker and WSL **isolation** (levels 3 and 4) were exercised through command-wrapping and validation unit tests only; no container
  or WSL execution was run. The server Dockerfile/compose were not built (no Docker engine running); CI is configured to build them.
* PostgreSQL, PDF reports and a server web UI are not implemented.
* Claude Code and Codex were not run against the MCP server.
* Docker daemon operations in 0.1 were exercised against canned CLI output; WSL status/Doctor ran against the real machine only.
* Performance was not benchmarked; no performance numbers are claimed.
* CI workflows were written but not executed (there is no remote repository yet).

## Other repeatable checks

* `powershell -File build\tools\test-installer.ps1 -New dist\RUNTIDE-<ver>-setup.exe -Old <older installer>`: installer fresh/upgrade/uninstall in a temporary directory with an isolated data folder (refuses to run if RUNTIDE is installed).
* `runtide mcp check --level read|controlled|sensitive`: exercises the MCP server from the CLI.
* `runtide diagnostics isolation`: which isolation levels can run on this machine.
* `tests/linux/server-smoke.sh`: the Linux server smoke test (driven by the Go scenario test through WSL).

## Gotchas

* Use explicit package lists: `./...` also matches `frontend/node_modules/flatted/golang`.
* Test helpers re-execute the test binary as a fake dev server (`RUNTIDE_HELPER=...` in `TestMain`).

## Regenerating the README demo

`DEMO=1 DEMO_FRAMES=<empty dir> npx playwright test demo.spec.ts` (in `tests/e2e`, with `RUNTIDE_E2E_HOST` pointing at a built
`e2ehost.exe`) records the run, verify, receipt and replay flow from the real app; then
`go run ./build/tools/mkgif -manifest <dir>/manifest.json -out docs/screenshots/demo.gif -speed 1.25` builds the GIF.
