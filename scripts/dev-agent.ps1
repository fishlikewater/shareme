$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8

# 进入后端目录并启动本地代理。
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
$goCacheDir = Join-Path $repoRoot ".cache\\go-build"

Push-Location $backendDir
try {
    $env:GOCACHE = $goCacheDir
    $env:GOTELEMETRY = "off"
    go run ./cmd/message-share-agent

    if ($LASTEXITCODE -ne 0) {
        throw ("本地代理启动失败，退出码：{0}" -f $LASTEXITCODE)
    }
}
finally {
    Pop-Location
}
