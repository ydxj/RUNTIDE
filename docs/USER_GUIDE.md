# User guide

How the desktop application is organised and what each part is for. The CLI offers the same operations
([CLI_REFERENCE.md](CLI_REFERENCE.md)); every number shown comes from data stored on your machine.

## The idea

A developer, or an AI agent, **executes a task** in a controlled workspace. RUNTIDE records what actually happened, **verifies**
the result against checks you define, and produces a **Run Receipt**: portable evidence you can inspect, compare and share. It is
infrastructure around coding work, not an assistant.

The sidebar has two groups: **Tasks** (the execution platform) and **Machine** (the local infrastructure tools from RUNTIDE 0.1).

## Tasks

### Overview
Active and recent executions, executions that failed or were interrupted, how many are verified, **project health** (the latest
execution of each project), **recent verification outcomes** and **receipt activity**, next to measured CPU, memory and disks.
Nothing here is sample data: with nothing recorded, the panels say so.

### Projects
Register a directory, edit its description and **defaults** (isolation level for new workspaces, verification profile used when a
workspace defines none), open its execution history, or **archive** it. Archiving hides a project and blocks new workspaces; its
workspaces, executions and receipts are kept, and **Restore** brings it back. A project with a running execution cannot be archived.

### Workspaces
The environment of one task. Creating one shows exactly what will be created and warns what its isolation level does *not*
protect. Isolation levels: **managed** (process tree and environment only), **worktree** (separate git worktree or filtered copy),
**docker**, **wsl** ([EXECUTION_CAPSULES.md](EXECUTION_CAPSULES.md)). A workspace is **approved** by you for its exact
configuration; edits revoke approval. From here you can start an execution, edit the configuration, reopen a finished workspace
and remove the directory RUNTIDE created.

### Executions
Every run with its state, verification verdict and isolation. Opening one shows tabs:

* **Overview**: state, timing, errors, the commands and what they did.
* **Timeline**: chronological events (created, environment prepared, process started, output captured, files changed,
  verification, exit, receipt generated), each tagged *observed*, *reported* or *inferred*; filter, search, jump to the next failure.
* **Commands** and **Logs**: exit codes, durations, redacted output (bounded; truncation is stated).
* **Processes**, **Services**, **File Changes** (secret files are listed as excluded, never read), **Verification**, **Environment**
  (variable names only), **Receipt** (export with options).

Actions: **Verify** (optionally with a stored profile), finish, cancel (stops everything it started), export.

### Live Activity
Everything in flight, streaming output and events.

### Verification
Recent verdicts per execution and the **verification profiles**: reusable sets of checks (command exit code and output, HTTP,
TCP, file existence/content, services healthy). A profile is validated when you save it, and can be applied to any execution or
set as a project default. A task is *verified* only when every required check ran and passed
([VERIFICATION.md](VERIFICATION.md)).

### Receipts
List, **inspect**, export, **import** and compare receipts, and generate HTML reports. Inspecting shows two separate results:
**integrity** (do the files match the manifest) and **signature** (*verified*, *unavailable* or *invalid*, plus whether you trust
the signer). **Replay pre-flight** lists dependencies (tools, secrets, source checkout), potentially destructive operations and
the permissions a replay needs; nothing runs until you review and approve, and the outcome is *reproduced*, *partially
reproduced*, *could not reproduce* or *metadata only* ([REPRODUCTION.md](REPRODUCTION.md)). Importing never executes anything.

### Compare
Pick two executions or receipts to see verdict changes, new failures, command and file differences and environment drift. Equal
evidence does not prove two runs were equivalent.

## Machine

**Services** and **Stacks** (named groups of services started in dependency order), **Environment** (detected tools and which
isolation levels can run *right now*, from real probes of Git, Docker and WSL2), **Integrations** (MCP setup, receipt signing and
trusted signers, self-hosted server), **Processes**, **Ports**, **Service logs**, **Resources**, **Containers**, **Doctor**,
and **Settings**.

### Settings
Appearance, log retention, secret redaction, Windows integration, backups, and **Execution evidence**: how long to keep executions
(default: forever), the stored output per command and events per execution, and a deliberate, counted, confirmed deletion of old
executions. Receipt files you exported are never deleted by retention.

## Keyboard
`Ctrl+K` command palette, `Ctrl+1…9` the first nine pages.

## Principles you can rely on

* **Nothing executes without your approval** of the exact configuration, and imported receipts and workspace files never execute.
* **Isolation is described truthfully** and never downgraded: if Docker or WSL2 is missing, the execution fails and says why.
* **Secrets stay out**: environment values are never stored, secret files are never read, output is redacted (best effort).
* **Evidence is bounded and honest**: limits are stated; a receipt never claims more than was recorded.
