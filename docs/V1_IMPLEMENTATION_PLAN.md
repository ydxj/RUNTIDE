# RUNTIDE 1.0.0 implementation plan

Baseline: 0.2.0 (commit `b5de88f`), about 34 000 lines (Go 24 600, TypeScript 9 400, tests included). Everything is committed; the
working tree was clean when this plan was written. This plan is based on reading the code and running the 0.2.0 suite
(158 Go tests, 33 Vitest, 8 Playwright, all passing), not on the 0.2.0 documentation.

**Version rule.** The product stays at `0.2.x` until the gates in section 6 pass. If they cannot all pass, the release is
`0.9.x` or `0.2.x` and the blockers are listed in [KNOWN_LIMITATIONS.md](KNOWN_LIMITATIONS.md).

## 1. Audit

### Complete and verified (keep, do not rewrite)
Execution lifecycle and state machine, recorder (bounds, redaction, provenance), managed and worktree capsules, file-change tracking
with secret exclusion, verification pipelines (6 check types, dependencies, statuses), receipt build/verify/sign/import/compare/
replay/HTML, orphan recovery, CLI groups, MCP server (3 tiers), self-hosted server (SQLite, argon2id, roles, audit, rate limits,
OpenAPI, bind policy), v1→v2 migration with pre-migration backup, 0.1 infrastructure features.

### Partially implemented
| Area | Gap |
| --- | --- |
| Projects | create/list only: no edit, archive, per-project settings, project detail with history |
| Verification | pipelines live inside each workspace configuration: no **reusable profiles**; no profile UI/CLI/MCP |
| Receipts | signature state is a single boolean-like block: needs the four explicit states (integrity verified / signature verified / unavailable / invalid) in API, CLI, UI, HTML |
| Reproduction | plan exists, but no pre-flight list of destructive operations and permissions, and result classes are not the required four outcomes (reproduced / partially / could not / metadata only) |
| Timeline | events lack explicit `output.captured` and `receipt.generated` |
| Settings | no execution limits, execution retention, storage, security, MCP or server panels |
| Environment page | does not show Docker/WSL2 availability or the resolved execution configuration |
| Retention | 0.1 logs are pruned; execution evidence is bounded per execution but **never pruned** |
| Server | stores and serves receipts; no structured request logging; "execution operations" are limited to stored-execution inspect/compare/delete (by design it never executes) |
| MCP | no verification-profile or receipt-verify tools; never run from a real client |
| CLI | no `mcp` or `diagnostics` command groups; config management limited to 0.1 settings; exit codes documented only partly |
| Docs | no quick start, user guide, known-limitations or release notes; receipt format is described but not specified field by field |

### Untested against real environments
WSL2 isolation (available on this machine and **testable**), Docker isolation (CLI present, **daemon not running**; needs the
owner to start Docker Desktop), Claude Code (not installed here), Codex (not installed), fresh/upgrade installer behaviour of the
0.2 installer, server container image, concurrent use of the server database.

### Security items to review explicitly
Argument handling for each capsule (managed, docker, wsl quoting), WSL path translation, archive handling on import, signature
verification and key trust, imported workspace configuration never executing without approval, MCP tier enforcement on the server
side (not only tool registration), server authorization on every route, secrets in logs/receipts/HTML reports, process termination
(pid reuse), unsafe defaults (inherit_env, ad-hoc commands).

## 2. Architecture decision

One shared Go core (`internal/execution`, `receipts`, `verification`, `store`) behind `Engine.Dispatch`. Desktop, CLI and MCP only
call it; this already holds and is enforced by tests. 1.0 adds **no new execution path**. New capabilities (profiles, project
update/archive, reproduction pre-flight, retention) are added once in the core and surfaced three times.

## 3. Phases

Each phase ends with its tests, a regression run, and an update to `DEVELOPMENT_PROGRESS.md`.

| Phase | Work | Depends on | Acceptance criteria |
| --- | --- | --- | --- |
| A | Audit, this plan | none | plan reviewed against code |
| B | Engine reliability: WSL2 real execution tests; isolation never silently downgraded (explicit error when Docker/WSL unavailable); Docker tests that run when the daemon is up and report *skipped: daemon unavailable*; timeouts/cancel/crash tests on every level that can run; orphan recovery re-verified | A | WSL2 execution test passes on this machine; unavailable runtime yields a clear error, never a fallback; no test is faked |
| C | Recorder and receipts: `output.captured` and `receipt.generated` events; four signature states end to end; execution retention and size limits with pruning; receipt schema spec; compatibility handling for future/older versions | B | tamper, wrong-key, missing-signature and unsupported-version tests; prune test keeps receipts and honours settings |
| D | Verification and comparison: reusable profiles (store, CRUD, apply to an execution, CLI, MCP, UI); reproduction pre-flight and the four outcome classes | C | a profile created once verifies two different executions; reproduction shows destructive operations and needs approval |
| E | Desktop: project edit/archive/settings/history; workspace isolation display; execution creation form; settings panels; environment page with Docker/WSL; every button wired; all stats from stored data | D | no control without behaviour; Vitest + Playwright cover each page's main action |
| F | CLI and MCP: consistent groups (`mcp`, `diagnostics`), documented exit codes, `--json` everywhere, MCP profile and receipt-verify tools, server-side tier checks, Claude Code test **if the client can be installed with the owner's permission** | D | CLI scenario test covers profiles; MCP tests cover every tool; Claude Code result recorded honestly (tested or blocked) |
| G | Server: structured logging, concurrency test (parallel uploads and reads), persistence test, container build/run **if Docker daemon is available** | C | race-free concurrent test; Docker result recorded honestly |
| H | Isolation validation: documented and tested truth table per level (what a command can and cannot see) | B | each statement in EXECUTION_CAPSULES.md backed by a test or marked untested |
| I | Security review document with findings and fixes; fuzz/negative tests for archives, paths and imports | all | findings fixed or listed; no critical/high open |
| J | Packaging; fresh install and upgrade test of the installer in an isolated directory; checksums; docs set; release notes | all | install, start, upgrade (data preserved) and uninstall verified silently |

## 4. Documentation deliverables
`README`, `INSTALLATION`, `QUICKSTART`, `USER_GUIDE`, `CLI_REFERENCE`, `MCP_INTEGRATION`, `CLAUDE_CODE_INTEGRATION`,
`SELF_HOSTING`, `ARCHITECTURE`, `SECURITY_MODEL` (+ threat model), `RUN_RECEIPT_SPEC`, `VERIFICATION`, `TROUBLESHOOTING`,
`KNOWN_LIMITATIONS`, `RELEASE_NOTES`. Each claim must match implemented behaviour.

## 5. Rules for this work
No git staging/commits/tags/pushes/releases. No deletion of user data. No tests that kill unrelated processes. No weakening
of security to pass a test. Unavailable integrations are reported as unavailable.

## 6. 1.0.0 gates
1. Core workflow (project → workspace → execute → observe → verify → receipt → inspect/compare) passes from desktop (Playwright) and CLI (scenario) on Windows.
2. Persistence verified: restart, crash recovery, 0.1→0.2→1.0 migration with backup.
3. Security review completed; no open critical/high findings.
4. Release build and **installer install/upgrade/uninstall** verified.
5. Documentation matches behaviour; limitations documented.
6. Isolation levels 3 and 4 either tested for real or **not claimed** as supported in the release notes.
7. MCP validated with a real client, or the release notes say it was validated with the SDK client only.

## 7. Outcome of this plan (updated at the end of the work)

| Phase | Result |
| --- | --- |
| A Audit | Done; the gaps in section 1 were the work list |
| B Engine reliability | WSL2 isolation tested against a real distribution; Docker and WSL no-fallback tests; Docker status false positive fixed; capsule flake fixed. **Docker run itself skipped: engine not running** |
| C Recorder and receipts | `output.captured` / `receipt.generated`; explicit integrity and signature states; retention and limits; receipt specification written |
| D Verification and comparison | Reusable profiles (store, CLI, MCP, desktop); replay pre-flight and four outcomes |
| E Desktop | Project edit/archive/defaults/history; profiles; replay approval flow; settings panels; isolation availability; Overview panels. Vitest and Playwright cover each |
| F CLI and MCP | `mcp` and `diagnostics` groups, `run prune`, `--profile`; MCP profile tools. **Claude Code not available to test with** |
| G Server | Structured logging, concurrency race fixed and tested, Linux build run in WSL2. **Container image not built: Docker engine not running** |
| H Isolation validation | Truth table written into EXECUTION_CAPSULES.md, each statement backed by a test or marked untested |
| I Security | Review done ([SECURITY_REVIEW.md](SECURITY_REVIEW.md)): one high-severity finding (links escaping the workspace) fixed |
| J Packaging | 0.9.0 artifacts built; installer fresh install / startup / upgrade from 0.2.0 / uninstall tested (28 checks) |

**Gate decision:** gates 1 to 5 are met. Gates 6 and 7 and the container part of gate 4 are not, so the version is **0.9.0**.
Moving to 1.0.0 requires only exercising Docker, a real MCP client and the container build; no code change is known to be needed.
