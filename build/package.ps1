<#
.SYNOPSIS
  Builds RUNTIDE release artifacts into .\dist:
    runtide.exe                      CLI (also the background engine and `runtide server` launcher)
    runtide-mcp.exe                  MCP server for coding agents
    runtide-server.exe               self-hosted receipt server
    RUNTIDE-Desktop.exe              desktop app (Wails)
    RUNTIDE-<ver>-portable.zip       desktop + CLI + MCP, no installer
    RUNTIDE-<ver>-cli.zip            CLI + MCP only
    RUNTIDE-Server-<ver>.zip         server binary + Dockerfile + compose files
    RUNTIDE-<ver>-setup.exe          installer (needs Inno Setup 6: ISCC.exe)
  Nothing is signed or published.
#>
param(
  [string]$Version = "0.9.1",
  [switch]$SkipInstaller
)
$ErrorActionPreference = 'Stop'
$root = Resolve-Path "$PSScriptRoot\.."
Set-Location $root
$ld = "-s -w -X github.com/runtide/runtide/internal/version.Version=$Version"

New-Item -ItemType Directory -Force dist | Out-Null
Remove-Item dist\* -Recurse -Force -ErrorAction SilentlyContinue

foreach ($b in @(@('runtide', 'cmd\runtide'), @('runtide-mcp', 'cmd\runtide-mcp'), @('runtide-server', 'cmd\runtide-server'))) {
  Write-Host "== $($b[0])"
  go build -trimpath -ldflags $ld -o "dist\$($b[0]).exe" ".\$($b[1])"
  if ($LASTEXITCODE) { throw "$($b[0]) build failed" }
}

Write-Host "== Desktop (Wails)"
wails build -clean -platform windows/amd64 -ldflags $ld
if ($LASTEXITCODE) { throw "wails build failed" }
Copy-Item build\bin\RUNTIDE-Desktop.exe dist\RUNTIDE-Desktop.exe

function New-Zip($name, $files, $extraDirs) {
  $stage = Join-Path $env:TEMP "runtide-$name-$Version"
  Remove-Item $stage -Recurse -Force -ErrorAction SilentlyContinue
  New-Item -ItemType Directory $stage | Out-Null
  Copy-Item $files $stage
  foreach ($d in $extraDirs) { Copy-Item $d (Join-Path $stage (Split-Path $d -Leaf)) -Recurse }
  Compress-Archive -Path "$stage\*" -DestinationPath "dist\$name.zip" -Force
}
Write-Host "== Zips"
New-Zip "RUNTIDE-$Version-portable" @('dist\RUNTIDE-Desktop.exe', 'dist\runtide.exe', 'dist\runtide-mcp.exe', 'LICENSE', 'README.md') @()
New-Zip "RUNTIDE-$Version-cli" @('dist\runtide.exe', 'dist\runtide-mcp.exe', 'LICENSE', 'README.md') @()
New-Zip "RUNTIDE-Server-$Version" @('dist\runtide-server.exe', 'LICENSE', 'docs\SELF_HOSTING.md') @('deployments')

if (-not $SkipInstaller) {
  $iscc = (Get-Command ISCC.exe -ErrorAction SilentlyContinue).Source
  if (-not $iscc) {
    foreach ($p in "$env:LOCALAPPDATA\Programs\Inno Setup 6\ISCC.exe", "${env:ProgramFiles(x86)}\Inno Setup 6\ISCC.exe", "$env:ProgramFiles\Inno Setup 6\ISCC.exe") {
      if (Test-Path $p) { $iscc = $p; break }
    }
  }
  if (-not $iscc) { throw "Inno Setup 6 (ISCC.exe) not found. Install it or pass -SkipInstaller." }
  Write-Host "== Installer ($iscc)"
  & $iscc "/DAppVersion=$Version" build\installer.iss
  if ($LASTEXITCODE) { throw "installer build failed" }
}

Get-ChildItem dist | Select-Object Name, @{n='MB';e={[math]::Round($_.Length/1MB,1)}}
Get-FileHash dist\* -Algorithm SHA256 | ForEach-Object { "$($_.Hash.ToLower())  $(Split-Path $_.Path -Leaf)" } | Set-Content dist\SHA256SUMS.txt
