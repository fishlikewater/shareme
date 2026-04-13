$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8

$scriptPath = Get-Variable -Name PSCommandPath -ValueOnly -ErrorAction SilentlyContinue
if (-not $scriptPath) {
    $scriptPath = $MyInvocation.MyCommand.Definition
}
if (-not $scriptPath) {
    throw "无法确定脚本路径。"
}
$scriptDir = Split-Path -Parent $scriptPath
$repoRoot = Split-Path -Parent $scriptDir
$backendDir = Join-Path $repoRoot "backend"
$frontendDir = Join-Path $repoRoot "frontend"
$goCacheDir = Join-Path $repoRoot ".cache\\go-build"
$npmCacheDir = Join-Path $repoRoot ".cache\\npm"
$previousGoCache = $env:GOCACHE
$previousGoTelemetry = $env:GOTELEMETRY
$previousNpmCache = $env:npm_config_cache

try {
    Push-Location $backendDir
    try {
        $env:GOCACHE = $goCacheDir
        $env:GOTELEMETRY = "off"
        go test -p 1 ./...

        if ($LASTEXITCODE -ne 0) {
            throw ("后端测试失败，退出码：{0}" -f $LASTEXITCODE)
        }
    }
    finally {
        Pop-Location
    }

    Push-Location $frontendDir
    try {
        $env:npm_config_cache = $npmCacheDir
        npm test

        if ($LASTEXITCODE -ne 0) {
            throw ("前端测试失败，退出码：{0}" -f $LASTEXITCODE)
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
