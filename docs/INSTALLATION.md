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

Verified on this project's build machine: silent install, running `runtide.exe` from the install folder, in-place
upgrade, and uninstall (all files and the uninstall registration removed, data folder kept). The installer has **not**
been tested on a separate clean Windows installation or a VM, and binaries are unsigned (SmartScreen may warn).

## Portable

Unzip `RUNTIDE-<version>-portable.zip` anywhere. `RUNTIDE-Desktop.exe` is the app, `runtide.exe` the CLI. Data still
lives in `%LOCALAPPDATA%\Runtide`; set the `RUNTIDE_HOME` environment variable to keep it elsewhere (for example next to
the executables on a USB stick).

## CLI only

`runtide.exe` is standalone. Optional shell completion: `runtide completion powershell | Out-String | Invoke-Expression`.

## Build from source

See [CONTRIBUTING.md](../CONTRIBUTING.md) and `build/package.ps1` (needs Go, Node 22, the Wails CLI and Inno Setup 6).
