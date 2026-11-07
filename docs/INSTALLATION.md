# Installation

Requirements: Windows 10 22H2+ or Windows 11 (x64) and the Microsoft WebView2 runtime (preinstalled on Windows 11
and current Windows 10; otherwise install the Evergreen runtime from Microsoft). No administrator rights are needed.

## Installer (per-user)

Run `RUNTIDE-<version>-setup.exe`. It installs to `%LOCALAPPDATA%\Programs\RUNTIDE` by default (a machine-wide
install is available through the privileges dialog), creates a Start-menu entry, optionally a desktop shortcut and
optionally adds the `runtide` command to your **user** PATH (both unticked by default). It does not touch security
settings, install services or enable start-with-Windows.

* **Upgrade**: run a newer installer over the old one. Running RUNTIDE is closed first. Your data in
  `%LOCALAPPDATA%\Runtide` (database, logs, backups) is preserved.
* **Uninstall**: Settings > Apps, or `unins000.exe`. It stops the background engine, removes files, the optional PATH
  entry and the optional start-with-Windows entry. User data is kept; delete `%LOCALAPPDATA%\Runtide` for a full wipe.
* **Silent**: `RUNTIDE-<version>-setup.exe /VERYSILENT /SUPPRESSMSGBOXES /NORESTART /DIR="C:\Tools\RUNTIDE"`.

**Tested** (`build/tools/test-installer.ps1`, run against the 0.9.0 and 0.2.0 installers for the 0.9.1 upgrade on the build machine, into a temporary
directory with an isolated data folder, 28 checks): fresh silent install, installed files, uninstall registration, the installed
CLI and MCP server running (`mcp check`), the desktop app opening a window and staying up, uninstall (files and registration
removed, data kept), and an **upgrade from the real 0.2.0 installer** with the recorded project, execution, output and a
`pre-migration-v2-…` backup all present afterwards. **Not tested**: a clean virtual machine, Windows 10, and the upgrade of a
real user profile. Binaries are unsigned (SmartScreen may warn).

## Portable

Unzip `RUNTIDE-<version>-portable.zip` anywhere. `RUNTIDE-Desktop.exe` is the app, `runtide.exe` the CLI, `runtide-mcp.exe` the MCP server for coding agents. Data still
lives in `%LOCALAPPDATA%\Runtide`; set the `RUNTIDE_HOME` environment variable to keep it elsewhere (for example next to
the executables on a USB stick).

## CLI only

`RUNTIDE-<version>-cli.zip` contains `runtide.exe` and `runtide-mcp.exe`. `runtide.exe` is standalone. Optional shell completion: `runtide completion powershell | Out-String | Invoke-Expression`.

## Server

`RUNTIDE-Server-<version>.zip` contains `runtide-server.exe`, `deployments/` (Dockerfile, compose file, `.env.example`) and
[SELF_HOSTING.md](SELF_HOSTING.md). The server is optional and loopback-only until you configure it. Other prerequisites per
feature (Git, Docker, WSL): [WINDOWS_SETUP.md](WINDOWS_SETUP.md).

## Uninstalling

Settings → Apps → RUNTIDE, or `unins000.exe` in the install folder (`/VERYSILENT` for silent). It stops the background engine and
removes the program files and the optional PATH/autostart entries. **Your data is kept** (`%LOCALAPPDATA%\Runtide`: database,
receipts, backups). Delete that folder yourself for a full wipe; export any receipts you want to keep first.

## Verifying downloads

`SHA256SUMS.txt` lists the SHA-256 of every artifact: `Get-FileHash .\RUNTIDE-0.9.1-setup.exe -Algorithm SHA256`. Binaries are
not code-signed, so compare the hash with one you obtained from a source you trust.

## Upgrading from 0.1 or 0.2

Install over the old version. On first start RUNTIDE writes `backups\pre-migration-v1-<time>.db` before migrating; your stacks,
services and history are kept. The 0.1 CLI group `workspace` is now `stack` (`runtide stack up …`); `workspace` manages task
workspaces. See [DATABASE.md](DATABASE.md).

## Build from source

See [CONTRIBUTING.md](../CONTRIBUTING.md) and `build/package.ps1` (needs Go, Node 22, the Wails CLI and Inno Setup 6).
