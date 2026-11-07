# Release notes

## 0.9.1 (release candidate for 1.0.0)

A fix release for 0.9.0. No features and no database changes (schema stays at version 3). Still a **release candidate**: Docker
task isolation and a real MCP client have not been exercised ([KNOWN_LIMITATIONS.md](KNOWN_LIMITATIONS.md)). Nothing has been
published by the build; artifacts are in `dist\`.

### Fixed
* **Server container could not start** when run as its non-root user: `mkdir /data/artifacts: permission denied`. The image now
  creates `/data` owned by uid 65532 and new volumes inherit it. Found by the container job in CI, which builds the image and
  checks the health endpoint on every push.
* **All 18 Dependabot alerts**: 16 were in development tooling (vitest 2 to 5, which brings vite 6.4.5; a scoped `minimatch`
  override for the linter) and 2 were `labstack/echo` (4.16.1 to 4.16.3), which reaches RUNTIDE only through the Wails dev server;
  RUNTIDE does not use Echo's rate limiter or reverse proxy. The shipped web bundle is byte-identical to 0.9.0.
* **CI** now passes on every job: a `.gitattributes` keeps Go files LF on the Windows runner (`gofmt` had flagged every file), and
  one end-to-end wait was lengthened for slow runners.

### Changed
* The README starts with a 20-second demo recorded from the real application (`tests/e2e/demo.spec.ts`, `build/tools/mkgif`).
* Version strings, the installer, the Dockerfile and the compose file say 0.9.1.

### Upgrading
Install over 0.9.0 or 0.2.0; your data is kept. See [INSTALLATION.md](INSTALLATION.md).

---

## 0.9.0 (release candidate for 1.0.0)

RUNTIDE 0.9.0 completes the execution and verification platform started in 0.2 and fixes defects found while hardening it. It is
a **release candidate**: see [KNOWN_LIMITATIONS.md](KNOWN_LIMITATIONS.md) for the three items that stand between it and 1.0.0
(real Docker run, a real MCP client, a built container image). Nothing has been published; artifacts are in `dist\`.

### Added
* **Reusable verification profiles**: create, edit, delete and apply in the desktop app, CLI (`runtide verification …`,
  `run verify --profile`) and MCP (`list_verification_profiles`, `run_verification` with a profile). Projects can name a
  default profile.
* **Project management**: edit description and defaults, archive and restore, execution history per project.
* **Explicit receipt trust states**: `integrity` (verified/failed) and `signatureState` (verified/unavailable/invalid) in the
  API, CLI, desktop and HTML reports.
* **Replay pre-flight** (dependencies, destructive operations, required permissions) and the four outcomes *reproduced*,
  *partially reproduced*, *could not reproduce*, *metadata only*; replay can be run from the desktop with an approval step.
* **Timeline events** `output.captured` and `receipt.generated`.
* **Execution retention and limits** (Settings and `runtide run prune`): default keeps everything.
* **Isolation availability** shown in the Environment page and `runtide diagnostics isolation`, from real probes.
* **CLI groups** `mcp` (`config`, `serve`, `check`) and `diagnostics`; `runtide run prune`.
* **Server**: structured JSON request log, `X-Request-Id`, concurrency-safe uploads, Linux build.
* Overview: project health, recent verification outcomes, receipt activity.
* Documentation: quick start, user guide, receipt specification, security review, known limitations.
* WSL2 isolation tested against a real distribution; Docker and WSL "never fall back" tests.

### Fixed
* Server: concurrent uploads of the same receipt could fail with HTTP 500.
* **Junctions and symlinks inside a workspace could be followed outside it** by verification file checks, receipt artifacts and
  diffs (high severity; see [SECURITY_REVIEW.md](SECURITY_REVIEW.md)).
* Docker was reported as running when Docker Desktop was stopped.
* The Docker image of the server could not have been built (Windows-only imports); the server now builds for Linux.
* Comparing two executions left **phantom entries in the receipt list** (temporary receipts were registered, then deleted), which then
  failed when opened. Comparison no longer registers anything, and databases that already contain such entries are cleaned on start.
* The replay pre-flight and receipt inspection could crash the desktop page when the engine returned `null` instead of an empty
  list; lists are now always arrays.
* A flaky capsule test (present since 0.2.0).

### Changed
* The receipt format code moved to the platform-independent package `receiptcore` (no change to the format, which stays at
  version 1). The product version lives in `internal/version` (build flag `-X …/internal/version.Version=`).
* Database schema version 3 (additive): project archive/settings columns and a `verification_profiles` table. A backup is written
  before the migration.

### Upgrading
Install over the previous version; your data is kept. The first start migrates the database after writing
`backups\pre-migration-v2-<time>.db`. See [INSTALLATION.md](INSTALLATION.md).

---

## 0.2.0
Execution and verification platform: task workspaces, capsules, recorder, verification pipelines, Run Receipts, replay, MCP server,
self-hosted server, new desktop pages. See [CHANGELOG.md](../CHANGELOG.md).

## 0.1.0
Local infrastructure control center: processes, ports, services, stacks, logs, health, environment, Doctor.
