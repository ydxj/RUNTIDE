# Execution recording

Every execution is recorded by the **Execution Recorder**: an append-only timeline of events plus the output of every command,
stored in the local SQLite database. The recording is the evidence a receipt is built from.

## Provenance

Each event carries a `source`:

* `observed`: RUNTIDE saw it happen (a process started, a port opened, a file hash changed, a check passed).
* `reported`: something told RUNTIDE (an agent's note, a command a caller says it ran).
* `inferred`: derived from observations (for example a finding that a service died because a dependency failed).

The UI and receipts show the source on every event; evidence is never upgraded.

## What is recorded

* Lifecycle: `execution.created`, `workspace.prepared`, `state.changed`, `execution.completed|failed|cancelled|interrupted`.
* Commands: `command.started|finished`, arguments (secret-looking values masked, flagged `redacted`), exit code, duration,
  working directory, bytes of stdout/stderr, whether output was truncated or the command timed out or was cancelled.
* Processes: `process.started` (pid, creation time), `process.terminated`, managed-process table used for orphan detection.
* Services: start, health transitions (`service.healthy`/`service.unhealthy`), stop.
* Verification: `test.started`, `verification.passed|failed|skipped|timed_out`, `test.completed`.
* Files: `files.changed` summary (see below).

## Bounds

| Limit | Default |
| --- | --- |
| Stored output per command | 8 MiB |
| Stored lines per command | 100 000 |
| Events per execution | 100 000 |

Past a limit RUNTIDE keeps counting but stops storing, marks the command `truncated`, and writes a single event saying the limit
was reached. Failures to persist an event are returned to the caller: evidence is never dropped silently.

## Redaction

Output lines, event data and command arguments pass through the redactor before they are stored or sent to the UI. It masks
`KEY=value` pairs with secret-looking names, bearer tokens, URL credentials, private-key blocks and common token formats, plus
any extra patterns in Settings. Redaction is best effort: it cannot recognise every secret, which is why receipts additionally
omit environment values and secret files altogether.

## File change intelligence

On start RUNTIDE snapshots a baseline of the workspace (paths, sizes, SHA-256) and compares it at the end: **added, modified,
deleted, renamed**. Rules:

* `.git`, `node_modules`, `dist`, `build`, `target`, virtualenvs and caches are skipped.
* Secret-looking files (`.env`, `*.pem`, `*.key`, `id_rsa*`, `credentials*`, `*.kdbx`, `.npmrc`, …) are listed as *excluded* with
  no hash and no content, never as ordinary changes.
* Text diffs of modified files are available on demand (`runtide run diff <id> <path>`); they are redacted and size-bounded and
  are included in a receipt only when you choose to.

## Crash handling

If RUNTIDE stops while an execution runs, the next engine start marks it `INTERRUPTED` (evidence is kept and can be exported).
Processes that outlived RUNTIDE are only **reported**: `runtide run orphans` lists them and `runtide run cleanup` terminates the
ones whose pid **and** creation time match the recorded process, after confirmation. Unverified entries are never touched.

## Viewing it

Desktop: **Executions → (execution) → Timeline / Commands / Logs / Processes / Services / File Changes**, and **Live Activity**
for everything in flight. CLI: `runtide run events|logs|files|diff|status <id>`.
