<#
.SYNOPSIS
  Exercises the installer silently: fresh install, application startup, upgrade from an older
  installer with user data preserved, and uninstall. Everything happens in a temporary directory with an
  isolated data folder (RUNTIDE_HOME); the real %LOCALAPPDATA%\Runtide is never touched.
  The per-user uninstall registration (HKCU) is created and removed by the installer itself.
  Refuses to run if RUNTIDE is already installed.
.EXAMPLE
  powershell -File build\tools\test-installer.ps1 -New dist\RUNTIDE-0.9.1-setup.exe -Old C:\path\RUNTIDE-0.2.0-setup.exe
#>
param(
  [Parameter(Mandatory)] [string]$New,
  [string]$Old = "",
  [switch]$SameSchema   # the old version already has the current database schema (no migration, so no backup is expected)
)
$ErrorActionPreference = 'Stop'
$results = [ordered]@{}
function Check($name, [bool]$ok, $detail = "") {
  $script:results[$name] = [pscustomobject]@{ ok = $ok; detail = $detail }
  $mark = if ($ok) { "PASS" } else { "FAIL" }
  Write-Host ("{0}  {1}  {2}" -f $mark, $name, $detail)
}
function InstallEntry {
  Get-ItemProperty 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall\*', 'HKLM:\Software\Microsoft\Windows\CurrentVersion\Uninstall\*' -ErrorAction SilentlyContinue |
    Where-Object { $_.DisplayName -like 'RUNTIDE*' } | Select-Object -First 1
}
function Run($exe, $arguments, $timeoutSec = 180) {
  $p = Start-Process -FilePath $exe -ArgumentList $arguments -PassThru -Wait -WindowStyle Hidden
  return $p.ExitCode
}

if (InstallEntry) { throw "RUNTIDE is already installed on this machine; refusing to run the installer test." }
$New = (Resolve-Path $New).Path
$base = Join-Path $env:TEMP ("runtide-installer-test-" + [guid]::NewGuid().ToString('N').Substring(0, 8))
$app = Join-Path $base "app"
$data = Join-Path $base "data"
$proj = Join-Path $base "project"
New-Item -ItemType Directory $base, $data, $proj | Out-Null
$env:RUNTIDE_HOME = $data
$silent = @("/VERYSILENT", "/SUPPRESSMSGBOXES", "/NORESTART", "/DIR=`"$app`"")
try {
  # ---- A: fresh install of the new version
  $code = Run $New $silent
  Check "fresh install exits 0" ($code -eq 0) "exit $code"
  foreach ($f in "RUNTIDE-Desktop.exe", "runtide.exe", "runtide-mcp.exe", "unins000.exe", "README.md", "LICENSE") {
    Check "installed file $f" (Test-Path (Join-Path $app $f))
  }
  $entry = InstallEntry
  Check "uninstall registration created" ($null -ne $entry) ($entry.DisplayVersion)
  $ver = & (Join-Path $app "runtide.exe") version
  Check "installed CLI reports a version" ($ver -match "runtide \d+\.\d+\.\d+") $ver
  $mcp = & (Join-Path $app "runtide-mcp.exe") --version
  Check "installed MCP server runs" ($mcp -match "runtide-mcp") $mcp
  $chk = & (Join-Path $app "runtide.exe") mcp check --level read 2>&1 | Out-String
  Check "installed 'mcp check' handshakes" ($chk -match "handshake ok")
  & (Join-Path $app "runtide.exe") daemon stop | Out-Null

  # the desktop app starts, shows a window and stays up
  $d = Start-Process (Join-Path $app "RUNTIDE-Desktop.exe") -PassThru
  $deadline = (Get-Date).AddSeconds(30)
  while ((Get-Date) -lt $deadline -and $d.MainWindowHandle -eq 0) { Start-Sleep -Milliseconds 300; $d.Refresh() }
  Check "desktop app opens a window" ($d.MainWindowHandle -ne 0)
  Start-Sleep 3
  Check "desktop app still running after 3s" (-not $d.HasExited)
  Stop-Process -Id $d.Id -Force -ErrorAction SilentlyContinue
  Start-Sleep 2
  & (Join-Path $app "runtide.exe") daemon stop | Out-Null

  $code = Run (Join-Path $app "unins000.exe") @("/VERYSILENT", "/SUPPRESSMSGBOXES", "/NORESTART")
  Start-Sleep 3
  Check "uninstall exits 0" ($code -eq 0) "exit $code"
  Check "uninstall removed the program files" (-not (Test-Path (Join-Path $app "runtide.exe")))
  Check "uninstall removed the registration" ($null -eq (InstallEntry))
  Check "uninstall kept user data" (Test-Path $data)

  # ---- B: upgrade from an older version keeps user data
  if ($Old) {
    $Old = (Resolve-Path $Old).Path
    Remove-Item $data -Recurse -Force; New-Item -ItemType Directory $data | Out-Null
    $code = Run $Old $silent
    Check "old version installs" ($code -eq 0) "exit $code"
    $oldCli = Join-Path $app "runtide.exe"
    $oldVer = & $oldCli version
    Write-Host "   old version: $oldVer"
    & $oldCli project create upg --root $proj | Out-Null
    "version: 1`ntask: upgrade check`nstart:`n  - {name: hi, command: cmd, args: ['/c', 'echo before-upgrade']}`n" | Set-Content (Join-Path $base "w.yaml") -Encoding ascii
    & $oldCli workspace create w --project upg --config (Join-Path $base "w.yaml") --yes | Out-Null
    & $oldCli run create --workspace upg/w | Out-Null
    $id = ((& $oldCli run list --json) | ConvertFrom-Json)[0].id
    & $oldCli run start $id --yes | Out-Null
    & $oldCli run finish $id | Out-Null
    & $oldCli daemon stop | Out-Null
    Start-Sleep 2
    Check "old version recorded an execution" ($id -match "^ex_") $id

    $code = Run $New $silent
    Check "upgrade install over the old version exits 0" ($code -eq 0) "exit $code"
    $newCli = Join-Path $app "runtide.exe"
    $newVer = & $newCli version
    Check "upgraded CLI reports the new version" ($newVer -ne $oldVer) "$oldVer -> $newVer"
    $plist = & $newCli project list
    Check "project survived the upgrade" (($plist | Out-String) -match "upg")
    $status = & $newCli run status $id | Out-String
    Check "execution history survived the upgrade" ($status -match "COMPLETED")
    $logs = & $newCli run logs $id | Out-String
    Check "recorded output survived the upgrade" ($logs -match "before-upgrade")
    $bk = Get-ChildItem (Join-Path $data "backups") -Filter "pre-migration-*" -ErrorAction SilentlyContinue
    if ($SameSchema) {
      Check "no migration needed, so no backup was written (same schema)" (($bk | Measure-Object).Count -eq 0)
    } else {
      Check "a pre-migration backup was written" (($bk | Measure-Object).Count -ge 1) (($bk | Select-Object -First 1).Name)
    }
    & $newCli daemon stop | Out-Null
    Start-Sleep 2
    $code = Run (Join-Path $app "unins000.exe") @("/VERYSILENT", "/SUPPRESSMSGBOXES", "/NORESTART")
    Start-Sleep 3
    Check "uninstall after upgrade exits 0" ($code -eq 0) "exit $code"
    Check "uninstall after upgrade kept the data" (Test-Path (Join-Path $data "runtide.db"))
    Check "no registration left behind" ($null -eq (InstallEntry))
  }
}
finally {
  $left = Get-Process | Where-Object { $_.ProcessName -match '^(runtide|RUNTIDE)' -and $_.Path -like "$base*" }
  $left | ForEach-Object { Stop-Process -Id $_.Id -Force -ErrorAction SilentlyContinue }
  Remove-Item Env:RUNTIDE_HOME -ErrorAction SilentlyContinue
  if ($InstallEntryLeft = InstallEntry) { Write-Host "WARNING: an installation entry is still present: $($InstallEntryLeft.DisplayName)" }
  Remove-Item $base -Recurse -Force -ErrorAction SilentlyContinue
}
$failed = @($results.GetEnumerator() | Where-Object { -not $_.Value.ok })
Write-Host ""
Write-Host ("{0} checks, {1} failed" -f $results.Count, $failed.Count)
if ($failed.Count) { exit 1 }
