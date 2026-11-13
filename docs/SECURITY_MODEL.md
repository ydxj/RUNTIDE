# Security model

RUNTIDE manages processes and reads machine state, so it is designed to do the least that is useful and to ask
before doing anything consequential.

## Trust boundaries

| Boundary | Protection |
| --- | --- |
| Workspace files (imported, untrusted) | Strict YAML (unknown fields rejected), name/env-var validation, working directories must stay inside the project root (absolute, UNC, drive-letter and `..` rejected), dependency validation, size-limited, **no execution without approval**. |
| Approval | Bound to a SHA-256 of definition + root; editing revokes it; required for `workspace up` and for starting any workspace service. |
| Process execution | `CreateProcess` with an argument vector; no shell interpolation. Health-check commands and Docker calls use argument vectors as well; container IDs are validated so they cannot be parsed as flags. |
| Local API | Binds `127.0.0.1` on a random port; 256-bit random bearer token (constant-time compare) stored in `api.json` (mode 0600 in `%LOCALAPPDATA%\Runtide`); requests with an `Origin` header (browsers) or a non-loopback `Host` header (DNS rebinding) are rejected; bodies capped at 1 MiB; unknown JSON fields rejected. The Wails desktop binding does not use the network at all. |
| Privileges | Never runs elevated by default and never self-elevates. Access-denied results are reported as such. No installer-created services. |
| Secrets | Redacted in logs before storage and display (key/value patterns, bearer tokens, JWTs, AWS/GitHub/Slack/OpenAI-style keys, URL credentials, plus user patterns). Env inspection masks values of sensitive keys, never serialises them, never uploads anything. The application makes no outbound network requests. |
| Files | `.env.example` generation previews first, backs up any existing file (`*.runtide-backup-<time>`) and writes atomically. Doctor fixes can only touch paths inside registered workspace roots and never overwrite. |
| Database | Parameterised queries only; `LIKE` wildcards escaped; foreign keys on; integrity check on open; corruption is quarantined (`.corrupt-<time>`) not deleted; backups via `VACUUM INTO`. |

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
