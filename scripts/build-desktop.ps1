<#
.SYNOPSIS
  Build shareme desktop app on Windows.
.DESCRIPTION
  Calls Wails build with the specified platform.
  Default: windows/amd64.
.PARAMETER Platform
  Wails platform string (windows/amd64, windows/arm64).
.EXAMPLE
  .\scripts\build-desktop.ps1 -Platform windows/amd64
  .\scripts\build-desktop.ps1 windows/arm64
#>
param(
    [string]$Platform = "windows/amd64"
)

$ErrorActionPreference = "Stop"
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$BackendDir = Resolve-Path "$ScriptDir/../backend"

Push-Location $BackendDir
try {
    Write-Host "=== Building shareme desktop for $Platform ==="
    & go run github.com/wailsapp/wails/v2/cmd/wails@v2.12.0 build -platform $Platform
    if ($LASTEXITCODE -ne 0) {
        throw "wails build failed (exit code $LASTEXITCODE)"
    }
    Write-Host "=== Build complete: backend\build\bin\ ==="
} finally {
    Pop-Location
}
