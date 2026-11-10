<#
  Launches RUNTIDE-Desktop.exe with an isolated data folder and saves a screenshot of the real native window.
  Usage: powershell -File build\tools\window-screenshot.ps1 -Out docs\screenshots\desktop-window.png
#>
param([string]$Exe = "$PSScriptRoot\..\bin\RUNTIDE-Desktop.exe", [string]$Out = "desktop-window.png", [string]$Keys = "^1")
Add-Type -AssemblyName System.Windows.Forms, System.Drawing
Add-Type @"
using System; using System.Runtime.InteropServices;
public class Win { 
  [DllImport("user32.dll")] public static extern bool SetProcessDPIAware();
  [DllImport("user32.dll")] public static extern bool SetForegroundWindow(IntPtr h);
  [DllImport("user32.dll")] public static extern bool PrintWindow(IntPtr h, IntPtr hdc, uint flags);
  [DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr h, out RECT r);
  [DllImport("dwmapi.dll")] public static extern int DwmGetWindowAttribute(IntPtr h, int a, out RECT r, int s);
  [StructLayout(LayoutKind.Sequential)] public struct RECT { public int L, T, R, B; }
}
"@
[Win]::SetProcessDPIAware() | Out-Null
$home_ = Join-Path $env:TEMP ("runtide-shot-" + [guid]::NewGuid().ToString('N').Substring(0,8))
$env:RUNTIDE_HOME = $home_
$p = Start-Process (Resolve-Path $Exe) -PassThru
try {
  $deadline = (Get-Date).AddSeconds(30)
  while ((Get-Date) -lt $deadline -and $p.MainWindowHandle -eq 0) { Start-Sleep -Milliseconds 300; $p.Refresh() }
  if ($p.MainWindowHandle -eq 0) { throw "no window appeared" }
  Start-Sleep -Seconds 3
  [Win]::SetForegroundWindow($p.MainWindowHandle) | Out-Null
  Start-Sleep -Milliseconds 600
  [System.Windows.Forms.SendKeys]::SendWait($Keys)
  Start-Sleep -Seconds 3
  $r = New-Object Win+RECT
  [Win]::DwmGetWindowAttribute($p.MainWindowHandle, 9, [ref]$r, [Runtime.InteropServices.Marshal]::SizeOf($r)) | Out-Null
  $w = $r.R - $r.L; $h = $r.B - $r.T
  $bmp = New-Object System.Drawing.Bitmap $w, $h
  $g = [System.Drawing.Graphics]::FromImage($bmp)
  # PrintWindow renders only this window (never other apps that overlap it).
  $hdc = $g.GetHdc()
  [Win]::PrintWindow($p.MainWindowHandle, $hdc, 2) | Out-Null
  $g.ReleaseHdc($hdc)
  $bmp.Save((Join-Path (Get-Location) $Out), [System.Drawing.Imaging.ImageFormat]::Png)
  "saved $Out ($w x $h)"
} finally {
  if (-not $p.HasExited) { Stop-Process -Id $p.Id -Force }
  Start-Sleep 1
}
