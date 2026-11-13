# Windows integration

Everything below uses Win32 APIs directly (via `golang.org/x/sys/windows` and lazily loaded system DLLs); nothing
parses localized command output.

| Capability | API |
| --- | --- |
| Process enumeration | `CreateToolhelp32Snapshot` + `Process32First/Next`; per process `OpenProcess(QUERY_LIMITED_INFORMATION)`, `QueryFullProcessImageName`, `GetProcessTimes`, `GetProcessMemoryInfo` |
| Command line | `NtQueryInformationProcess` -> PEB -> `RTL_USER_PROCESS_PARAMETERS` via `ReadProcessMemory` (x64; needs `PROCESS_VM_READ`, silently empty for protected/elevated/other-user processes) |
| Ports | `GetExtendedTcpTable` (listener and all-states classes) and `GetExtendedUdpTable`, IPv4 and IPv6, owning PID |
| CPU / memory / disks | `GetSystemTimes`, `GlobalMemoryStatusEx`, `GetLogicalDrives`, `GetDriveType`, `GetDiskFreeSpaceEx`, `GetVolumeInformation`; per-process CPU from kernel+user time deltas |
| Termination | `OpenProcess(TERMINATE|SYNCHRONIZE)`, creation-time check against PID reuse, `TerminateProcess`, `WaitForSingleObject` to verify |
| Process trees | Job Objects: `CreateJobObject`, `SetInformationJobObject(KILL_ON_JOB_CLOSE)`, `AssignProcessToJobObject`, `TerminateJobObject`, `QueryInformationJobObject`; services start `CREATE_SUSPENDED` and are resumed with `NtResumeProcess` after assignment |
| Graceful stop | `AttachConsole` to the service's private hidden console (`CREATE_NO_WINDOW`) + `GenerateConsoleCtrlEvent(CTRL_BREAK)`, with a swallow handler so RUNTIDE does not interrupt itself; serialised by a mutex |
| Native services | Service Control Manager: `OpenSCManager`, `EnumServicesStatusExW`, `OpenService`, `StartService`, `ControlService` (read-only enumeration; control only on explicit request) |
| Tray + notifications | `Shell_NotifyIcon` (icon, context menu via `TrackPopupMenu`, balloon/toast via `NIF_INFO`), hidden window + message loop on a locked OS thread, `TaskbarCreated` re-registration |
| Start with Windows | `HKCU\Software\Microsoft\Windows\CurrentVersion\Run` value `RUNTIDE` (opt-in, per-user) |
| Elevation check | `Token.IsElevated` (shown in `version`; RUNTIDE never requests elevation) |

## Behaviours worth knowing

* Closing the window minimises to the tray when "Minimise to tray" is on (default). Use the tray menu's **Quit** to exit;
  managed services are stopped gracefully on exit.
* If the background engine (`runtide daemon`) is already running when you open the desktop app, the app attaches to it
  instead of starting a second engine.
* A second launch of the desktop app focuses the existing window (single-instance lock).
* WebView2: the app uses the system WebView2 runtime; it is not bundled.

## Not implemented

Per-interface network throughput (the 64-bit counter APIs were left out rather than shipping approximations),
ETW/Event Log reading (application logs are deliberately separate from the Windows Event Log), and administrator
elevation flows.
