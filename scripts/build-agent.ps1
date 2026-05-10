param(
    [string]$Output = "dist\\shareme-agent.exe"
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
$frontendDir = Join-Path $repoRoot "frontend"
$agentFrontendDir = Join-Path $backendDir "cmd\\shareme-agent\\frontend"
$agentFrontendDistDir = Join-Path $agentFrontendDir "dist"
$sourceFrontendDistDir = Join-Path $backendDir "frontend\\dist"
$goCacheDir = Join-Path $repoRoot ".cache\\go-build"
$npmCacheDir = Join-Path $repoRoot ".cache\\npm"
$previousGoCache = $env:GOCACHE
$previousGoTelemetry = $env:GOTELEMETRY
$previousNpmCache = $env:npm_config_cache

function Ensure-AgentFrontendAssets {
    Push-Location $frontendDir
    try {
        $env:npm_config_cache = $npmCacheDir
        if (-not (Test-Path (Join-Path $frontendDir "node_modules"))) {
            npm ci
            if ($LASTEXITCODE -ne 0) {
                throw ("Frontend dependency install failed with exit code {0}" -f $LASTEXITCODE)
            }
        }

        npm run build
        if ($LASTEXITCODE -ne 0) {
            throw ("Frontend build failed with exit code {0}" -f $LASTEXITCODE)
        }
    }
    finally {
        Pop-Location
    }

    Remove-Item -LiteralPath $agentFrontendDistDir -Recurse -Force -ErrorAction SilentlyContinue
    New-Item -ItemType Directory -Force -Path $agentFrontendDistDir | Out-Null
    Copy-Item -Recurse -Force (Join-Path $sourceFrontendDistDir "*") $agentFrontendDistDir
}

try {
    Ensure-AgentFrontendAssets

    $env:GOCACHE = $goCacheDir
    $env:GOTELEMETRY = "off"
    $outputPath = Join-Path $repoRoot $Output
    New-Item -ItemType Directory -Force -Path (Split-Path -Parent $outputPath) | Out-Null

    Push-Location $backendDir
    try {
        go build -o $outputPath ./cmd/shareme-agent
        if ($LASTEXITCODE -ne 0) {
            throw ("Agent build failed with exit code {0}" -f $LASTEXITCODE)
        }
    }
    finally {
        Pop-Location
    }
}
finally {
    $env:GOCACHE = $previousGoCache
    $env:GOTELEMETRY = $previousGoTelemetry
    $env:npm_config_cache = $previousNpmCache
}
