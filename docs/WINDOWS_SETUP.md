# Windows setup

A checklist for getting a Windows 10/11 machine ready for every RUNTIDE feature. Nothing here needs administrator rights unless
stated.

## Required

* Windows 10 22H2+ or Windows 11, x64.
* **WebView2 runtime** for the desktop app (preinstalled on Windows 11 and current Windows 10).
* Install RUNTIDE: [INSTALLATION.md](INSTALLATION.md).

## Per feature

| You want | You need |
| --- | --- |
| Managed isolation (level 1) | nothing |
| Worktree isolation (level 2) | **Git for Windows** on `PATH` (`git --version`); the project should be a git repository for a real worktree, otherwise a filtered copy is used |
| Docker isolation (level 3) | **Docker Desktop** running its Linux engine; the user must be able to run `docker` (membership in `docker-users` may require an administrator to set up once) |
| WSL isolation (level 4) | WSL2 and an installed distribution (`wsl --install -d Ubuntu`; needs administrator rights and a reboot **once**) |
| Verification pipelines | whatever tools your checks call (`node`, `npm`, `dotnet`, …) on `PATH` |
| Self-hosted server | `runtide-server.exe` (included) or Docker for the container image |
| MCP agents | the agent's own installation, see [MCP_INTEGRATION.md](MCP_INTEGRATION.md) |

Run `runtide doctor` to see which of these are present; it is read-only unless you pass `--fix`.

## Things worth knowing

* **Antivirus / Defender**: RUNTIDE's binaries are unsigned, so SmartScreen may warn. RUNTIDE starts child processes, uses Job
  Objects and reads other processes' command lines; some security products flag that behaviour. Allow-list the install folder if
  needed. RUNTIDE never disables or changes security settings.
* **Elevation**: RUNTIDE never elevates silently. Processes of other users or protected processes are shown read-only and can't be
  terminated from RUNTIDE; run it elevated yourself only if you need that.
* **Long paths and OneDrive**: keep projects and worktrees out of OneDrive-synchronised folders (file locks make deletion and
  hashing unreliable) and enable Windows long paths if your repositories contain deep trees.
* **Line endings**: worktrees follow your git `core.autocrlf` setting. A receipt's file hashes reflect what is on disk.
* **Data location**: `%LOCALAPPDATA%\Runtide`, or `RUNTIDE_HOME`.
