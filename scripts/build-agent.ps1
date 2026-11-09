<#
.SYNOPSIS
  Build shareme-agent on Windows.
.DESCRIPTION
  Builds the shareme-agent Go binary with cross-compilation support.
  Default target: windows/amd64, CGO disabled.
.PARAMETER Output
  Output file path. Default: dist\shareme-agent.exe
.PARAMETER GOOS
  Target OS. Default: windows.
.PARAMETER GOARCH
  Target architecture. Default: amd64.
.EXAMPLE
  .\scripts\build-agent.ps1
  .\scripts\build-agent.ps1 -GOOS linux -GOARCH amd64 -Output dist\shareme-agent-linux-amd64
#>
param(
    [string]$Output = "",
    [string]$GOOS = "windows",
    [string]$GOARCH = "amd64"
)

$ErrorActionPreference = "Stop"
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$ProjectDir = Resolve-Path "$ScriptDir/.."
$BackendDir = "$ProjectDir/backend"

if ($Output -eq "") {
    $ext = if ($GOOS -eq "windows") { ".exe" } else { "" }
    $Output = "dist/shareme-agent${ext}"
}

Push-Location $BackendDir
try {
    $env:GOOS = $GOOS
    $env:GOARCH = $GOARCH
    $env:GOTELEMETRY = "off"
    $env:CGO_ENABLED = "0"

    Write-Host "=== Building shareme-agent for $GOOS/$GOARCH -> $Output ==="
    & go build -o "$ProjectDir/$Output" ./cmd/shareme-agent
    if ($LASTEXITCODE -ne 0) {
        throw "agent build failed (exit code $LASTEXITCODE)"
    }
} finally {
    Pop-Location
}
