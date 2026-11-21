# CLI reference

`runtide.exe` is standalone: it works with the desktop app closed. Add `--json` to any command for
machine-readable output and `--no-color` (or set `NO_COLOR`) to disable color.

```powershell
runtide                       # same as `runtide status`
runtide completion powershell | Out-String | Invoke-Expression   # tab completion (also: bash, zsh, fish)
```

## Exit codes

| Code | Meaning |
| --- | --- |
| 0 | Success |
| 1 | Failure (message on stderr) or user cancelled a confirmation |
| 2 | Usage error (bad flag/argument) |
| 3 | A check found problems: `doctor` reported an error, `run verify` verdict is not `passed`, or `receipt verify` found the receipt invalid |
| 4 | The background engine could not be reached/started (`daemon status` also returns 4 when none is running) |

## Commands

| Command | Description |
| --- | --- |
| `version` | Print the version. |
| `status` | CPU, memory, disks, process/port counts, service and workspace summary, warnings. |
| `processes [filter]` (`ps`) | Table sorted by memory. `--tree`, `--cmdline`. CPU% is measured over a 0.4 s window. |
| `kill <pid>` | Terminate after confirmation (`-y` skips). Refuses protected processes; reports Windows' real answer. |
| `ports [filter]` | Listening TCP ports with owner, exposure and conflicts. `--udp` adds UDP bindings. |
| `services` | Managed services (standalone and workspace) with state, health, PID, uptime. |
| `start <service>` / `stop` / `restart` | Control a service. Use `workspace/service` if the bare name is ambiguous. Starts the background engine if needed. |
| `logs [service...]` | Recent captured output. `-f` follow, `-n` lines, `--level`, `--search`, `--history` (persisted). |
| `stacks` | List stacks (the 0.1 "workspaces": named groups of services). |
| `stack import <file.yaml>` | Validate and register (shows the commands; runs nothing). `--root`, `--replace`, `-y`. |
| `stack up <name> [service...]` | Show commands and ask for authorisation the first time (`-y`), then start in dependency order with progress. |
| `stack down <name>` | Stop in reverse dependency order. |
| `stack restart <name>` | Down then up. |
| `stack status <name>` | Services by start layer with state and health. |
| `stack export <name> [-o file]` | Print or save the YAML. |
| `stack delete <name>` | Remove the definition (confirmation; project files untouched). |
| `workspace history [name]` | Past start/stop outcomes. |
| `doctor` | Run diagnostics. `--fix <id>` previews then applies an opt-in fix (`-y` skips the question). |
| `env inspect [dir]` | Analyze `.env` files; values of sensitive keys are masked. |
| `env compare <a> <b>` | Differences between two env files (redacted). |
| `env example <file> [--write]` | Preview/generate `.env.example` (backs up an existing one). |
| `tools` | Installed developer tools with versions, locations, PATH status and detection method. |
| `docker` / `docker start|stop <name>` | Docker status and containers (optional integration). |
| `wsl` | WSL availability and distributions (read-only). |
| `config` | Show settings, data directory and database info. |
| `config set <key> <value>` | Change a setting. |
| `config path` / `backup` / `restore <file>` / `reset` | Data directory, backup, restore (engine must be stopped), reset (backup first, confirmation). |
| `daemon start|stop|status|run` | Control the background engine. `stop` gracefully stops every managed service. |

## Examples

```powershell
runtide ports 3000                        # who owns port 3000?
runtide ps node --cmdline                 # every node process with its command line
runtide workspace import .\runtide.yaml
runtide workspace up my-fullstack-project
runtide logs my-fullstack-project/backend -f --level error
runtide doctor --json | ConvertFrom-Json  # use in scripts
if ($LASTEXITCODE -eq 3) { "problems found" }
```

## Execution platform (0.2)

Stacks (`stack …`, above) group long-running services. The commands below record, verify and share **tasks**. All accept `--json`.
Commands that execute configured commands ask for approval unless `-y/--yes` is given; in a non-interactive shell without `--yes`
they refuse.

| Command | Description |
| --- | --- |
| `project create <name> [--root dir] [--description d]` | Register a project directory. |
| `project list [--all]` | List projects (`--all` includes archived ones). |
| `project show <project>` | Show a project with its settings. |
| `project update <project> [--description d] [--default-isolation level] [--default-profile name]` | Change description or defaults; flags not given are kept, `""` clears. |
| `project archive <project>` / `project unarchive <project>` | Hide / restore a project. Nothing is deleted; a project with a running execution cannot be archived. |
| `workspace create <name> --project p [--config file.yaml] [--isolation managed\|worktree\|docker\|wsl] [--dir d] [--allow-dirty]` | Show the plan, then create a task workspace. |
| `workspace list` / `inspect <ref>` | List, or show a workspace and the commands it would run. `ref` is an id or `project/name`. |
| `workspace approve <ref>` | Approve the *current* commands (any `configure` revokes it). |
| `workspace configure <ref> <file.yaml>` | Replace the configuration. |
| `workspace reopen <ref>` | Make a finished workspace READY again. |
| `workspace start <ref>` / `stop <ref>` | Create and start an execution / cancel the active one. |
| `workspace remove-dir <ref> [--force]` | Delete the worktree directory RUNTIDE created (never the project). |
| `workspace delete <ref>` | Delete the record (refused if executions exist). |
| `run create --workspace ref [task]` | Create an execution (CREATED). |
| `run start <id>` | Prepare, run setup/start commands and bring services up. |
| `run list [--workspace w] [--state S] [--limit n]` | List executions. |
| `run status <id>` | State, commands, services, verification, file changes. |
| `run logs <id> [--command name] [-f]` | Recorded (redacted) output. |
| `run events <id> [--type t] [--search s] [--severity s]` | The timeline. |
| `run exec <id> [--name n] -- cmd args…` | Ad-hoc recorded command (needs approval and `policy.allow_adhoc`). |
| `run verify <id> [--profile name]` | Run verification: the workspace's `verify` section, else the project's default profile, or the named profile. Exit code 3 unless the verdict is `passed`. |
| `run finish <id>` / `run cancel <id> [--reason r]` | Complete without verification / cancel and stop everything. |
| `run files <id>` / `run diff <id> <path>` | File changes / text diff of a modified file. |
| `run export <id> [-o file] [--include-diffs] [--artifact path] [--sign]` | Create a Run Receipt. |
| `run compare <a> <b>` | Compare two executions or receipts. |
| `run orphans` / `run cleanup [id]` | List / terminate verified orphaned processes. |
| `run prune --older-than <days> [--dry-run] [-y]` | Delete ended executions older than N days (counted and confirmed first; receipt files are kept). |
| `receipt list|inspect|verify|import|report|compare|keygen|push|replay` | See [RUN_RECEIPTS.md](RUN_RECEIPTS.md) and [REPRODUCTION.md](REPRODUCTION.md). |
| `server start [--addr a]` / `server status [--url u]` | Launch or probe the self-hosted server. |
| `verification list` / `show <name>` / `save <name> <file.yaml> [--description d]` / `delete <name>` | Reusable verification profiles (alias `profile`). Saving validates the pipeline. |
| `mcp config [--level l]` | Print the Claude Code registration command and generic MCP JSON. Changes nothing. |
| `mcp serve [--level l]` | Run the MCP server on stdio (what an MCP client launches). |
| `mcp check [--level l]` | Start the MCP server, connect as a client, list tools and make read-only calls. Reports what it tested. |
| `diagnostics isolation` | Which isolation levels can run here (Git, Docker engine, WSL2), from real probes. |
| `diagnostics run` | Same as `doctor`. |

`runtide-mcp.exe` and `runtide-server.exe` are separate binaries: [MCP_INTEGRATION.md](MCP_INTEGRATION.md),
[SELF_HOSTING.md](SELF_HOSTING.md).

## Scripting

* `--json` prints machine-readable output on every command; errors still go to stderr with a non-zero exit code.
* Commands never prompt when `--yes` is given. Without it, a non-interactive shell refuses actions that need confirmation
  instead of guessing.
* Exit codes: `0` success, `1` failure or declined confirmation, `2` usage error, `3` findings (verification not passed, receipt
  invalid, Doctor error), `4` the background engine could not be reached or started.
* `runtide receipt verify`, `run verify` and `diagnostics` are safe to use in CI: they change nothing except `run verify`, which
  records its results on the execution.
