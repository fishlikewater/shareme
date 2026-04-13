$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8

# 构建 Windows 代理可执行文件。
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
$frontendDistDir = Join-Path $frontendDir "dist"
$embeddedWebDir = Join-Path $backendDir "internal\\webui\\dist"
$outputPath = Join-Path $backendDir "message-share-agent.exe"
$goCacheDir = Join-Path $repoRoot ".cache\\go-build"
$npmCacheDir = Join-Path $repoRoot ".cache\\npm"
$previousGoos = $env:GOOS
$previousGoarch = $env:GOARCH
$previousGoCache = $env:GOCACHE
$previousGoTelemetry = $env:GOTELEMETRY
$previousNpmCache = $env:npm_config_cache
$previousViteApiBase = $env:VITE_MESSAGE_SHARE_LOCAL_API_BASE_URL
$previousViteWsBase = $env:VITE_MESSAGE_SHARE_LOCAL_API_WS_BASE_URL

Push-Location $frontendDir
try {
    $env:npm_config_cache = $npmCacheDir
    Remove-Item Env:VITE_MESSAGE_SHARE_LOCAL_API_BASE_URL -ErrorAction SilentlyContinue
    Remove-Item Env:VITE_MESSAGE_SHARE_LOCAL_API_WS_BASE_URL -ErrorAction SilentlyContinue

    npm ci

    if ($LASTEXITCODE -ne 0) {
        throw ("前端依赖安装失败，退出码：{0}" -f $LASTEXITCODE)
    }

    npm run build

    if ($LASTEXITCODE -ne 0) {
        throw ("前端构建失败，退出码：{0}" -f $LASTEXITCODE)
    }
}
finally {
    $env:npm_config_cache = $previousNpmCache
    $env:VITE_MESSAGE_SHARE_LOCAL_API_BASE_URL = $previousViteApiBase
    $env:VITE_MESSAGE_SHARE_LOCAL_API_WS_BASE_URL = $previousViteWsBase
    Pop-Location
}

if (-not (Test-Path $frontendDistDir)) {
    throw "前端构建产物不存在，无法嵌入代理程序。"
}

$resolvedBackendDir = [System.IO.Path]::GetFullPath($backendDir)
$resolvedEmbeddedWebDir = [System.IO.Path]::GetFullPath($embeddedWebDir)
$expectedEmbeddedWebDir = [System.IO.Path]::GetFullPath((Join-Path $backendDir "internal\\webui\\dist"))
if ($resolvedEmbeddedWebDir -ne $expectedEmbeddedWebDir) {
    throw "嵌入目录解析异常，已停止清理。"
}
if (-not $resolvedEmbeddedWebDir.StartsWith($resolvedBackendDir + [System.IO.Path]::DirectorySeparatorChar)) {
    throw "嵌入目录不在后端目录内，已停止清理。"
}

New-Item -ItemType Directory -Path $resolvedEmbeddedWebDir -Force | Out-Null
Get-ChildItem $resolvedEmbeddedWebDir -Force | Where-Object { $_.Name -ne ".keep" } | Remove-Item -Recurse -Force
Copy-Item (Join-Path $frontendDistDir "*") $resolvedEmbeddedWebDir -Recurse -Force

Push-Location $backendDir
try {
    $env:GOOS = "windows"
    $env:GOARCH = "amd64"
    $env:GOCACHE = $goCacheDir
    $env:GOTELEMETRY = "off"
    go build -o $outputPath ./cmd/message-share-agent

    if ($LASTEXITCODE -ne 0) {
        throw ("代理构建失败，退出码：{0}" -f $LASTEXITCODE)
    }
}
finally {
    $env:GOOS = $previousGoos
    $env:GOARCH = $previousGoarch
    $env:GOCACHE = $previousGoCache
    $env:GOTELEMETRY = $previousGoTelemetry
    Pop-Location
}

Write-Host "已生成代理程序：$outputPath"
