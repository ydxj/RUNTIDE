<#
.SYNOPSIS
  Regression check for the "RUNTIDE opens a lot of console windows" bug: starts the detached background engine
  (with an isolated data folder), asks it to run helper programs (tool version probes, Docker, WSL, doctor), and counts the
  console windows (classic console or Windows Terminal) that appear while it does. A correct build shows 0.
.DESCRIPTION
  Needs an interactive desktop session (it looks at visible windows). It touches no real data: RUNTIDE_HOME points at a
  temporary folder that is deleted afterwards. Any console or terminal window you open yourself during the run is counted too.
.EXAMPLE
  powershell -File build\tools\test-no-console-windows.ps1 -Cli dist\runtide.exe -Label current
#>
param([Parameter(Mandatory)][string]$Cli, [Parameter(Mandatory)][string]$Label)
$ErrorActionPreference = 'Continue'
Add-Type @"
using System;
using System.Collections.Generic;
using System.Runtime.InteropServices;
using System.Text;
public static class Win {
  public delegate bool EnumProc(IntPtr h, IntPtr l);
  [DllImport("user32.dll")] static extern bool EnumWindows(EnumProc p, IntPtr l);
  [DllImport("user32.dll")] static extern bool IsWindowVisible(IntPtr h);
  [DllImport("user32.dll")] static extern int GetClassName(IntPtr h, StringBuilder s, int n);
  [DllImport("user32.dll")] static extern int GetWindowText(IntPtr h, StringBuilder s, int n);
  [DllImport("user32.dll")] static extern uint GetWindowThreadProcessId(IntPtr h, out uint pid);
  public static List<string> Snapshot() {
    var r = new List<string>();
    EnumWindows((h, l) => {
      if (!IsWindowVisible(h)) return true;
      var c = new StringBuilder(256); GetClassName(h, c, 256);
      var cls = c.ToString();
      if (cls == "ConsoleWindowClass" || cls == "CASCADIA_HOSTING_WINDOW_CLASS") {
        var t = new StringBuilder(256); GetWindowText(h, t, 256);
        uint pid; GetWindowThreadProcessId(h, out pid);
        r.Add(h.ToInt64() + "|" + cls + "|" + t + "|" + pid);
      }
      return true;
    }, IntPtr.Zero);
    return r;
  }
}
"@
$home1 = Join-Path $env:TEMP ("rt-winwatch-" + [guid]::NewGuid().ToString('N').Substring(0, 8))
New-Item -ItemType Directory -Force $home1 | Out-Null
$env:RUNTIDE_HOME = $home1
$baseline = [System.Collections.Generic.HashSet[string]]::new()
foreach ($w in [Win]::Snapshot()) { [void]$baseline.Add($w.Split('|')[0]) }
$seen = [System.Collections.Generic.Dictionary[string, string]]::new()
$sw = [Diagnostics.Stopwatch]::StartNew()
function Watch([int]$ms) {
  $end = $sw.ElapsedMilliseconds + $ms
  while ($sw.ElapsedMilliseconds -lt $end) {
    foreach ($w in [Win]::Snapshot()) {
      $k = $w.Split('|')[0]
      if (-not $baseline.Contains($k) -and -not $seen.ContainsKey($k)) { $seen[$k] = $w }
    }
    Start-Sleep -Milliseconds 10
  }
}
# The engine is started detached (as the desktop app and the CLI do) and then asked to probe tools, Docker, WSL and run the doctor.
$null = & $Cli daemon start 2>&1
Watch 1500
foreach ($c in @(@('tools'), @('docker'), @('wsl'), @('doctor'), @('diagnostics', 'isolation'), @('status'))) {
  $job = Start-Process -FilePath $Cli -ArgumentList $c -WindowStyle Hidden -PassThru -RedirectStandardOutput "$home1\out.txt" -RedirectStandardError "$home1\err.txt"
  while (-not $job.HasExited) { Watch 25 }
  Watch 200
}
$null = & $Cli daemon stop 2>&1
Watch 500
"[$Label] new visible console windows while the engine ran helper programs: $($seen.Count)"
$seen.Values | Select-Object -First 8 | ForEach-Object { "   window: $_" }
Remove-Item $home1 -Recurse -Force -ErrorAction SilentlyContinue
