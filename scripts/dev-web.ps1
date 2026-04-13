$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8

# 进入前端目录并启动本地开发服务器。
$scriptPath = Get-Variable -Name PSCommandPath -ValueOnly -ErrorAction SilentlyContinue
if (-not $scriptPath) {
    $scriptPath = $MyInvocation.MyCommand.Definition
}
if (-not $scriptPath) {
    throw "无法确定脚本路径。"
}
$scriptDir = Split-Path -Parent $scriptPath
$repoRoot = Split-Path -Parent $scriptDir
$frontendDir = Join-Path $repoRoot "frontend"
$nodeModulesDir = Join-Path $frontendDir "node_modules"
$npmCacheDir = Join-Path $repoRoot ".cache\\npm"
$previousNpmCache = $env:npm_config_cache
$previousViteApiBase = $env:VITE_MESSAGE_SHARE_LOCAL_API_BASE_URL
$previousViteWsBase = $env:VITE_MESSAGE_SHARE_LOCAL_API_WS_BASE_URL

Push-Location $frontendDir
try {
    $env:npm_config_cache = $npmCacheDir
    if ($env:MESSAGE_SHARE_LOCAL_API_ADDR) {
        $env:VITE_MESSAGE_SHARE_LOCAL_API_BASE_URL = "http://$($env:MESSAGE_SHARE_LOCAL_API_ADDR)"
        $env:VITE_MESSAGE_SHARE_LOCAL_API_WS_BASE_URL = "ws://$($env:MESSAGE_SHARE_LOCAL_API_ADDR)"
    }

    if (-not (Test-Path $nodeModulesDir)) {
        npm install

        if ($LASTEXITCODE -ne 0) {
            throw ("前端依赖安装失败，退出码：{0}" -f $LASTEXITCODE)
        }
    }

    npm run dev -- --host 127.0.0.1

    if ($LASTEXITCODE -ne 0) {
        throw ("前端开发服务器启动失败，退出码：{0}" -f $LASTEXITCODE)
    }
}
finally {
    $env:npm_config_cache = $previousNpmCache
    $env:VITE_MESSAGE_SHARE_LOCAL_API_BASE_URL = $previousViteApiBase
    $env:VITE_MESSAGE_SHARE_LOCAL_API_WS_BASE_URL = $previousViteWsBase
    Pop-Location
}
