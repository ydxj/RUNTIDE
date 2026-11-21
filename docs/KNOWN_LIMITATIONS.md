# Known limitations (0.9.0)

What RUNTIDE does not do, what has not been verified, and why this release is **0.9.0** rather than 1.0.0. See also
[SECURITY_REVIEW.md](SECURITY_REVIEW.md#known-limitations-security-relevant).

## Why this is not 1.0.0

The 1.0.0 criteria in [V1_IMPLEMENTATION_PLAN.md](V1_IMPLEMENTATION_PLAN.md) are not all met:

| Criterion | State |
| --- | --- |
| Critical workflows work (desktop and CLI) | Met: Playwright and scenario tests |
| Data persistence and migration validated | Met for 0.1 → current (test database migration with backup); crash recovery tested |
| Security review completed, no known critical/high open | Met as a self-review; no external audit |
| Release build succeeds | Met |
| **Installer behaviour tested** | **Partly**: see "Installer" below |
| Documentation matches implementation | Met to the best of the authors' checking |
| **Isolation levels 3 (Docker) tested for real** | **Not met**: Docker Desktop was not running on the build machine. Docker execution was never run end to end |
| **MCP validated with a real client** | **Not met**: Claude Code and Codex were not available; validated with the Go SDK client and a real stdio handshake only |
| **Server container image built and run** | **Not met**: no Docker engine; the Linux binary the image contains was built and run in WSL2 instead |

When those three items have been exercised for real, the version can move to 1.0.0 without code changes.

## Not implemented

* PostgreSQL for the self-hosted server (SQLite only; fine for a team-sized server, not for heavy concurrent write loads).
* A web UI for the server (API, CLI and the desktop app's receipt import only).
* PDF reports (the HTML report prints to PDF from a browser).
* The server does not run executions. This is by design: it stores, verifies, serves and compares receipts.
* A Linux or macOS desktop app or CLI. The server builds for Linux; the engine, capsules and desktop are Windows only.

## Not verified end to end

* **Docker isolation** (level 3): command construction and validation are unit-tested; the "never falls back" behaviour is tested;
  a real container run is skipped when no engine is running and was skipped on the build machine.
* **WSL2 isolation** (level 4) *was* run for real on the build machine (Ubuntu on WSL2). Other distributions were not.
* **Claude Code, Codex** and other MCP products.
* **Cross-machine replay**: a receipt recorded on one machine and replayed on another. Same-machine replay is tested.
* Clean-VM installation and Windows 10 (the build machine is Windows 11).
* Behaviour with a signed (Authenticode) installer: binaries are **unsigned**, SmartScreen will warn.

### Installer
The installer is built and its contents verified. Install, upgrade and uninstall are exercised silently into a temporary directory
on the build machine ([TESTING.md](TESTING.md)); a clean virtual machine and the upgrade of a *real* user profile were not tested.

## Behaviour you should know about

* **Isolation is not a sandbox.** Managed and worktree levels do not restrict files or network; WSL2 mounts Windows drives;
  Docker is only as strong as its configuration ([EXECUTION_CAPSULES.md](EXECUTION_CAPSULES.md)).
* **Redaction is pattern based** and cannot recognise every secret. Receipts omit environment values and secret files, but you
  should still review a receipt before sharing it.
* **Receipts are tamper-evident, not tamper-proof**, and a valid signature proves key possession, not trustworthiness.
* **Replay supports Windows-recorded receipts** and needs a local git checkout containing the recorded commit; its
  destructive-operation warnings are heuristic.
* **Local engine API**: any process running as your user can use it (including approving workspaces). RUNTIDE does not defend
  against malware that already runs as you.
* **Worktree isolation** needs a git repository with at least one commit; non-git projects get a filtered copy, and very large
  directories are refused.
* **Symlink/junction containment** covers verification, task workspaces, artifacts and diffs; the 0.1 stack feature still checks
  paths lexically.
* **Graceful stop** (CTRL_BREAK) reaches console processes only; GUI processes are terminated after the grace period.
* **Output limits**: output beyond the configured per-command limit is counted but not stored (the command is marked truncated).
* **Execution retention** is off by default; evidence grows until you delete it (Settings, or `runtide run prune`).
* The desktop sidebar scrolls on windows shorter than about 900 px.
