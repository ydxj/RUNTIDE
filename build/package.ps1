<#
.SYNOPSIS
  Builds RUNTIDE release artifacts into .\dist:
    runtide.exe                      standalone CLI
    RUNTIDE-Desktop.exe              desktop app (Wails)
    RUNTIDE-<ver>-portable.zip
    RUNTIDE-<ver>-setup.exe  installer (needs Inno Setup 6: ISCC.exe)
  Nothing is signed or published.
#>
param(
  [string]$Version = "0.1.0",
  [switch]$SkipInstaller
)
$ErrorActionPreference = 'Stop'
$root = Resolve-Path "$PSScriptRoot\.."
Set-Location $root
$ld = "-s -w -X github.com/runtide/runtide/internal/app.Version=$Version"

New-Item -ItemType Directory -Force dist | Out-Null
Remove-Item dist\* -Recurse -Force -ErrorAction SilentlyContinue

Write-Host "== CLI"
go build -ldflags $ld -o dist\runtide.exe .\cmd\runtide
if ($LASTEXITCODE) { throw "CLI build failed" }

Write-Host "== Desktop (Wails)"
wails build -clean -platform windows/amd64 -ldflags $ld
if ($LASTEXITCODE) { throw "wails build failed" }
Copy-Item build\bin\RUNTIDE-Desktop.exe dist\RUNTIDE-Desktop.exe

Write-Host "== Portable zip"
$stage = Join-Path $env:TEMP "runtide-portable-$Version"
Remove-Item $stage -Recurse -Force -ErrorAction SilentlyContinue
New-Item -ItemType Directory $stage | Out-Null
Copy-Item dist\RUNTIDE-Desktop.exe, dist\runtide.exe, LICENSE, README.md $stage
Compress-Archive -Path "$stage\*" -DestinationPath "dist\RUNTIDE-$Version-portable.zip" -Force

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
