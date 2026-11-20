# Security model

RUNTIDE manages processes and reads machine state, so it is designed to do the least that is useful and to ask
before doing anything consequential.

## Trust boundaries

| Boundary | Protection |
| --- | --- |
| Workspace files (imported, untrusted) | Strict YAML (unknown fields rejected), name/env-var validation, working directories must stay inside the project root (absolute, UNC, drive-letter and `..` rejected), dependency validation, size-limited, **no execution without approval**. |
| Approval | Bound to a SHA-256 of definition + root; editing revokes it; required for `stack up` and for starting any workspace service. |
| Process execution | `CreateProcess` with an argument vector; no shell interpolation. Health-check commands and Docker calls use argument vectors as well; container IDs are validated so they cannot be parsed as flags. |
| Local API | Binds `127.0.0.1` on a random port; 256-bit random bearer token (constant-time compare) stored in `api.json` (mode 0600 in `%LOCALAPPDATA%\Runtide`); requests with an `Origin` header (browsers) or a non-loopback `Host` header (DNS rebinding) are rejected; bodies capped at 1 MiB; unknown JSON fields rejected. The Wails desktop binding does not use the network at all. |
| Privileges | Never runs elevated by default and never self-elevates. Access-denied results are reported as such. No installer-created services. |
| Secrets | Redacted in logs before storage and display (key/value patterns, bearer tokens, JWTs, AWS/GitHub/Slack/OpenAI-style keys, URL credentials, plus user patterns). Env inspection masks values of sensitive keys, never serialises them, never uploads anything. The application makes no outbound network requests. |
| Files | `.env.example` generation previews first, backs up any existing file (`*.runtide-backup-<time>`) and writes atomically. Doctor fixes can only touch paths inside registered workspace roots and never overwrite. |
| Database | Parameterised queries only; `LIKE` wildcards escaped; foreign keys on; integrity check on open; corruption is quarantined (`.corrupt-<time>`) not deleted; backups via `VACUUM INTO`. |

## Execution platform (0.2)

| Boundary | Protection |
| --- | --- |
| Approval | A task workspace's commands run only after **you** approve the exact configuration (SHA-256 of config + directory); any edit revokes it. Agents, MCP and imports cannot approve. |
| Receipts | Evidence only: environment *values* are never stored, secret-looking files are never read or listed with content, absolute paths are scrubbed, diffs and artifacts are opt-in. Verification is in-memory with size/entry limits and unsafe-name rejection. Signatures prove key possession only; trust is an explicit fingerprint list. |
| Replay | Needs explicit approval of the displayed steps; runs in a new worktree at the recorded commit, never in your working tree; refuses when secrets were withheld, the commit is absent, or the receipt was recorded elsewhere than Windows. |
| Ad-hoc commands | Disabled unless the workspace policy sets `allow_adhoc`; `policy.allow`/`deny` globs apply. |
| Orphan cleanup | Terminates only processes whose pid **and** creation time match a recorded RUNTIDE launch, after confirmation. |
| Git | Worktrees on their own branch; the checked-out branch and uncommitted changes are never modified. |
| MCP | Permission level fixed by the human at registration; tools above it are not registered; every action is recorded. |
| Server | Loopback by default; remote needs TLS or an explicit proxy acknowledgement; argon2id passwords; hashed tokens with expiry; per-IP/user login limits and an API limit; role checks per project; audit log; no endpoint executes commands. |
| Isolation levels | Described honestly in [EXECUTION_CAPSULES.md](EXECUTION_CAPSULES.md): none of them is a sandbox for hostile code. |

## Safety rails on destructive actions

* **Process termination**: the engine re-reads the process, refuses protected ones (kernel, RUNTIDE itself, critical
  Windows processes, anything under `System32`/`SysWOW64`), guards against PID reuse by comparing creation times, and
  only reports success after `WaitForSingleObject` confirms the exit.
* **Native Windows services**: separate command, explicit `confirm`, core services (RpcSs, EventLog, Defender, ...)
  are refused, Windows decides whether the user may proceed.
* **Reset / restore**: confirmation required; a backup is created first; restore needs the engine stopped.
* **Startup with Windows**: opt-in per-user `HKCU\...\Run` value, removed on uninstall.

## Known limitations

* A process running as the same user can read the user's `api.json` and therefore call the local API. This is the
  same trust level as being able to run `runtide.exe` itself.
* Secret redaction is pattern based; unusual secret formats can pass through. Add custom patterns in Settings.
* `CTRL_BREAK` graceful stop only reaches console processes; GUI processes are terminated after the grace period.

## Reporting vulnerabilities

See [SECURITY.md](../SECURITY.md).
