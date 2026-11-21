# Security review (0.9.0)

This is a self-review by the authors of the code, performed by reading it and attacking it with tests. It is **not** an external
audit or a certification, and it does not claim RUNTIDE is secure. It lists what was examined, what was found, what was fixed and
what remains.

## Scope and method

Every area below was examined by reading the implementation and, where possible, by writing a failing test first. The tests are
in the repository and run in CI. The Go race detector was run over the server, store, verification, execution, receipts, capsule,
supervisor, MCP and files packages.

## Threat model

| Actor | Can | Cannot (by design) |
| --- | --- | --- |
| **Project content** (a repository you open, imported workspace files) | influence configuration you review, contain symlinks or junctions, contain secret-looking files | run anything before you approve its exact configuration |
| **A receipt from someone else** | be malformed, oversized, tampered, signed by an unknown key | execute anything on import or verification; be extracted to disk |
| **A coding agent over MCP** | read evidence, create workspaces, start *approved* workspaces, verify, export (by tier) | approve commands, create or edit verification profiles, replay receipts, exceed its tier |
| **A local process of the same user** | read `api.json` and call the local engine API | be stopped by RUNTIDE: this is the same trust level as running `runtide.exe` |
| **A network client of the server** | call the documented API with valid credentials | reach any unauthenticated administrative function or make the server run commands |
| **Another user on the same machine** | nothing beyond their own Windows permissions | (RUNTIDE adds no protection against an administrator or the same user) |

Out of scope: malicious code running as you (it can already do what RUNTIDE can), kernel-level attackers, supply-chain attacks
on dependencies, and anything requiring physical access.

## Findings

| # | Area | Finding | Severity | Status |
| --- | --- | --- | --- | --- |
| 1 | Server | Two concurrent uploads of the same receipt could both pass a check-then-insert and the loser failed with HTTP 500; the artifact rename could also fail on Windows under contention | Medium (availability) | **Fixed**, covered by `TestConcurrentUsersDoNotCorruptState` |
| 2 | Path traversal | `ResolveDir` was lexical only, so a symbolic link or **junction** inside a workspace (for example one committed to a repository) let verification file checks, command working directories, receipt `--artifact` embedding and `run diff` read paths outside the workspace | High (information disclosure into receipts) | **Fixed** with `config.ResolveInside` (resolves the real path with the Win32 API, because `filepath.EvalSymlinks` does not follow junctions); tests for each sink |
| 3 | Docker status | `docker info` exits 0 without a server when Docker Desktop is stopped, so RUNTIDE reported a running Docker engine | Medium (truthfulness of isolation) | **Fixed**, covered by a regression test |
| 4 | Packaging | The server image build would have failed: the server imported Windows-only packages through `internal/app` and `receipts` | Medium (deliverable did not work) | **Fixed** by splitting the platform-independent `receiptcore` and `internal/version`; the Linux binary is built and run in a test |
| 5 | Test | An assertion on interleaved stdout/stderr lines in the capsule tests was order dependent (flaky, present in 0.2.0) | Low | **Fixed** |
| 6 | Verification state | Receipts distinguished only "signed / not signed"; an invalid signature and an absent one were not explicit states | Low (clarity) | **Fixed**: `integrity` and `signatureState` are separate fields everywhere |

No other critical or high-severity issue was found by this review. That statement is limited to what was examined.

## Areas examined, with the evidence

| Topic | Result | Evidence |
| --- | --- | --- |
| Command injection through arguments | Arguments are passed as a vector; shell metacharacters (ampersand, pipe, semicolon, quotes, percent, caret, `$(...)` and backticks) arrive as literal text. A `.cmd` batch target is also safe (Go escapes arguments for `cmd.exe`) | `TestArgumentsAreNeverInterpretedByAShell`, `TestBatchFileTargetsCannotBeUsedForInjection` |
| Shell use | RUNTIDE never builds a shell command line. Docker and WSL wrappers pass the argument vector (`docker run … image exe args`, `wsl.exe --exec exe args`); environment values reach containers through the client's environment, not the command line | `internal/capsule` tests |
| Path traversal (lexical) | `..`, absolute, UNC and drive-letter paths are rejected in configuration | `internal/config` tests |
| Path traversal (links) | see finding 2 | `TestResolveInsideRejectsLexicalAndJunctionEscapes` and sink tests |
| Archive handling | Receipts are verified in memory, never extracted. Limits on entries, sizes and compression ratio; unsafe names rejected; undeclared files rejected | `TestZipBombIsRejected`, tamper tests, server upload tests |
| Receipt tampering | Any changed, added or removed file fails integrity | `TestTamperingIsDetected`, `TestIntegrityAndSignatureStatesAreDistinct` |
| Signatures | Ed25519 over the manifest hash. A forged signature by another key verifies as "valid but not trusted"; a signature that does not match is invalid and fails the receipt | `TestSigning` |
| Secrets in receipts and logs | Environment values never stored; secret files never read; arguments masked; output redacted; HTML report escapes content. The server request log never contains bodies, tokens or query strings | receipts tests, `TestRequestLogIsStructuredAndNeverContainsSecrets` |
| Imported configuration | Nothing runs before the exact configuration is approved; editing revokes approval; importing or verifying a receipt never executes; replay needs approval | `TestFullExecutionLifecycleWithRealProcesses`, replay tests |
| Process termination | Orphans are terminated only if pid **and** creation time match the recorded launch, after confirmation; protected processes are refused | `TestRecoveryMarksInterruptedAndOrphanCleanupIsSafe`, `internal/process` tests |
| Server authentication and authorization | Bearer tokens stored as hashes, argon2id passwords, login rate limits, per-route access level, project roles, last-owner protection | `TestAuthenticationAndHardening`, `TestRolesProjectsAndIsolationBetweenUsers` |
| Server exposure | Loopback only unless `--allow-remote`; remote plain HTTP refused without TLS or a proxy acknowledgement; no unauthenticated administrative route | `TestBindPolicy`, `TestOpenAPIMatchesRoutesAndHealth` |
| MCP permission enforcement | Tools above the configured tier are not registered; agents cannot approve, replay, or write profiles | `TestPermissionLevelsControlWhichToolsExist`, `TestAgentWorkflowRequiresHumanApprovalAndReturnsRealEvidence` |
| Unsafe defaults | Clean allow-listed environment, ad-hoc commands off, no replay without approval, execution evidence is never deleted unless retention is configured | execution tests, settings defaults |
| Isolation honesty | A missing Docker or WSL runtime fails the execution; there is no fallback to a weaker level | `TestWSLIsolationNeverFallsBackToNativeExecution`, `TestDockerIsolationNeverFallsBackToNativeExecution` |

## Known limitations (security relevant)

* **Local API trust.** Any process running as your user can read `api.json` and call the engine, including `ws.approve`.
  Approval protects against agents and imported content, not against malware already running as you.
* **Profiles and CLI.** Verification profiles contain commands. Humans create them (desktop or CLI). An agent that is allowed to run
  shell commands can also run `runtide verification save`; the MCP tools themselves cannot. Do not give an untrusted agent shell
  access to your RUNTIDE installation.
* **Redaction is best effort.** Unusual secret formats can pass through. Review receipts before sharing.
* **Isolation is not a sandbox.** Managed and worktree levels do not restrict files or network. Docker is only as strong as its
  configuration. WSL2 mounts Windows drives.
* **Destructive-operation detection in replay is a heuristic** and cannot prove a command safe.
* **Symlink containment applies to the 0.2 execution features** (verification, task workspaces, artifacts, diffs). The 0.1 stack
  feature (`working_directory`, `env_file`) still uses lexical containment.
* **Binaries are unsigned.** Verify the SHA-256 sums you receive out of band.
* **No external audit** has been performed. The server has not been tested against a malicious network client beyond the
  tests listed above (no fuzzing campaign).
