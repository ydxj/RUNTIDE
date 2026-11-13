# Testing

Nothing here requires administrator rights, and no test terminates a process it did not start.

## Layers

| Layer | Command | Notes |
| --- | --- | --- |
| Go unit + Windows integration | `go test ./cmd/... ./internal/... -count=1` | Real Win32 calls: own process listing, sockets opened by the test, Job Objects, real child processes. Mocks are limited to the Docker CLI output in `internal/docker` tests. |
| Static analysis | `gofmt -l`, `go vet`, `golangci-lint run` (see CONTRIBUTING for package lists) | Config in `.golangci.yml`. |
| Frontend | `cd frontend; npm run typecheck; npm run lint; npm run format:check; npm test` | Vitest + React Testing Library with a fake engine transport (tests only). TypeScript strict. |
| End-to-end | `go build -o dist\e2ehost.exe .\tests\e2ehost; go build -o dist\runtide.exe .\cmd\runtide; cd tests\e2e; npm ci; npx playwright test` | Playwright drives the built UI in Microsoft Edge against the **real engine** through `tests/e2ehost` (a bridge replacing the Wails binding): creates a workspace through the UI, authorises it, starts two real Node servers in order, checks ports with the OS, reads redacted logs, stops and verifies the ports closed, and talks to the same engine through the CLI. |
| Screenshots | `SCREENSHOTS=1 npx playwright test screenshots.spec.ts` | Regenerates `docs/screenshots` from a real demo workspace. `build/tools/window-screenshot.ps1` captures the native window. |

Key Go tests: supervisor (graceful stop exit code 0 proves CTRL_BREAK delivery, stubborn child force-killed, grandchild dies
with the job, restart policy gives up after exactly `max_retries`, backoff), workspaces (dependency order, reverse stop,
approval gate, rollback, traversal rejection), ports (IPv4/IPv6/UDP ownership of sockets opened by the test), process
(protection rules, own child terminated, self refused), API (token, Origin and Host rejection, loopback bind), database
(migrations, corruption recovery, backup/restore, prune), logs (bounded ring, truncation, slow-consumer drop, LIKE escaping),
redaction, health state machine, env files, diagnostics fixes (never overwrite, never outside roots), CLI (exit codes, JSON,
confirmation).

## Results of the last full run (Windows 11 Pro 10.0.26200, Go 1.27.3, Node 24.14)

* `gofmt`: clean. `go vet`: clean. `golangci-lint`: 0 issues.
* Go: 89 tests (including subtests) across 19 packages, 0 failures.
* Frontend: `tsc --noEmit`, ESLint (0 warnings) and Prettier clean; Vitest 7 files, 27 tests passed.
* Playwright: 6 passed (screenshots spec skipped unless `SCREENSHOTS=1`).
* Installer: silent install, CLI run from the install dir, in-place upgrade and uninstall succeeded on the build machine.

## Not covered / could not be executed here

* Clean-VM install, Windows 10, and any machine without WebView2.
* Tray icon, balloon notifications, minimise-to-tray and start-with-Windows are implemented but were not asserted by
  automated tests (they depend on the interactive desktop; the native app was only verified to launch and render).
* Docker daemon operations were exercised against canned CLI output; WSL status/Doctor ran against the real machine only.
* Performance was not benchmarked; no performance numbers are claimed.
* CI workflows were written but not executed (there is no remote repository yet).

## Gotchas

* Use explicit package lists: `./...` also matches `frontend/node_modules/flatted/golang`.
* Test helpers re-execute the test binary as a fake dev server (`RUNTIDE_HELPER=...` in `TestMain`).
