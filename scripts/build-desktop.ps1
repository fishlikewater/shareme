param(
    [string]$Platform = "windows/amd64",
    [string]$BuildTags = ""
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8

$scriptPath = Get-Variable -Name PSCommandPath -ValueOnly -ErrorAction SilentlyContinue
if (-not $scriptPath) {
    $scriptPath = $MyInvocation.MyCommand.Definition
}
if (-not $scriptPath) {
    throw "Unable to resolve script path."
}

$scriptDir = Split-Path -Parent $scriptPath
$repoRoot = Split-Path -Parent $scriptDir
$backendDir = Join-Path $repoRoot "backend"
$goCacheDir = Join-Path $repoRoot ".cache\\go-build"
$previousGoCache = $env:GOCACHE
$previousGoTelemetry = $env:GOTELEMETRY

Push-Location $backendDir
try {
    $env:GOCACHE = $goCacheDir
    $env:GOTELEMETRY = "off"
    $buildArgs = @("run", "github.com/wailsapp/wails/v2/cmd/wails@v2.12.0", "build", "-clean", "-platform", $Platform)
    if (-not [string]::IsNullOrWhiteSpace($BuildTags)) {
        $buildArgs += @("-tags", $BuildTags)
    }
    go @buildArgs

    if ($LASTEXITCODE -ne 0) {
        throw ("Desktop build failed with exit code {0}" -f $LASTEXITCODE)
    }
}
finally {
    Pop-Location
    $env:GOCACHE = $previousGoCache
    $env:GOTELEMETRY = $previousGoTelemetry
}
