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
| 3 | `doctor` found at least one error-level problem |
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
| `workspaces` | List workspaces. |
| `workspace import <file.yaml>` | Validate and register (shows the commands; runs nothing). `--root`, `--replace`, `-y`. |
| `workspace up <name> [service...]` | Show commands and ask for authorisation the first time (`-y`), then start in dependency order with progress. |
| `workspace down <name>` | Stop in reverse dependency order. |
| `workspace restart <name>` | Down then up. |
| `workspace status <name>` | Services by start layer with state and health. |
| `workspace export <name> [-o file]` | Print or save the YAML. |
| `workspace delete <name>` | Remove the definition (confirmation; project files untouched). |
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
