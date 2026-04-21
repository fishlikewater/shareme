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
$crossBuildDir = Join-Path $repoRoot ".tmp\\cross-build"
$previousGoCache = $env:GOCACHE
$previousGoTelemetry = $env:GOTELEMETRY
$previousNpmCache = $env:npm_config_cache
$previousGoOS = $env:GOOS
$previousGoArch = $env:GOARCH

function Ensure-BackendFrontendFallback {
    $fallbackDir = Join-Path $backendDir "frontend"
    $fallbackIndexPath = Join-Path $fallbackDir "index.html"

    if (Test-Path $fallbackIndexPath) {
        return
    }

    New-Item -ItemType Directory -Force -Path $fallbackDir | Out-Null
    @"
<!doctype html>
<html lang="zh-CN">
  <head>
    <meta charset="utf-8" />
    <title>Message Share</title>
  </head>
  <body>
    <!-- 构建兜底页：当前端 dist 尚未生成时，保证桌面宿主仍可完成编译并给出明确提示。 -->
    <div id="root">Message Share desktop assets are not built yet.</div>
  </body>
</html>
"@ | Set-Content -LiteralPath $fallbackIndexPath -Encoding UTF8
}

function Invoke-CrossBuildSanityCheck {
    param(
        [Parameter(Mandatory = $true)]
        [string]$TargetGoOS,
        [Parameter(Mandatory = $true)]
        [string]$TargetGoArch
    )

    $env:GOOS = $TargetGoOS
    $env:GOARCH = $TargetGoArch

    $configTestPath = Join-Path $crossBuildDir ("config-{0}-{1}.test" -f $TargetGoOS, $TargetGoArch)
    $desktopBinaryPath = Join-Path $crossBuildDir ("message-share-{0}-{1}" -f $TargetGoOS, $TargetGoArch)

    go test -c -o $configTestPath ./internal/config
    if ($LASTEXITCODE -ne 0) {
        throw ("跨平台配置测试编译失败（{0}/{1}），退出码：{2}" -f $TargetGoOS, $TargetGoArch, $LASTEXITCODE)
    }

    go build -o $desktopBinaryPath .
    if ($LASTEXITCODE -ne 0) {
        throw ("桌面主程序跨平台编译失败（{0}/{1}），退出码：{2}" -f $TargetGoOS, $TargetGoArch, $LASTEXITCODE)
    }
}

try {
    Ensure-BackendFrontendFallback

    Push-Location $backendDir
    try {
        $env:GOCACHE = $goCacheDir
        $env:GOTELEMETRY = "off"
        go test -count=1 -p 1 ./...

        if ($LASTEXITCODE -ne 0) {
            throw ("后端测试失败，退出码：{0}" -f $LASTEXITCODE)
        }

        New-Item -ItemType Directory -Force -Path $crossBuildDir | Out-Null
        Invoke-CrossBuildSanityCheck -TargetGoOS "linux" -TargetGoArch "amd64"
        Invoke-CrossBuildSanityCheck -TargetGoOS "darwin" -TargetGoArch "amd64"
        Invoke-CrossBuildSanityCheck -TargetGoOS "darwin" -TargetGoArch "arm64"
        $env:GOOS = $null
        $env:GOARCH = $null
    }
    finally {
        Pop-Location
    }

    Push-Location $frontendDir
    try {
        $env:npm_config_cache = $npmCacheDir
        npm ci

        if ($LASTEXITCODE -ne 0) {
            throw ("前端依赖安装失败，退出码：{0}" -f $LASTEXITCODE)
        }

        npm test

        if ($LASTEXITCODE -ne 0) {
            throw ("前端测试失败，退出码：{0}" -f $LASTEXITCODE)
        }
    }
    finally {
        Pop-Location
    }

    & "$repoRoot\scripts\build-desktop.ps1" -Platform "windows/amd64"
    if ($LASTEXITCODE -ne 0) {
        throw ("桌面构建失败，退出码：{0}" -f $LASTEXITCODE)
    }

    & "$repoRoot\scripts\smoke-desktop.ps1" -SkipBuild
    if ($LASTEXITCODE -ne 0) {
        throw ("桌面 smoke 失败，退出码：{0}" -f $LASTEXITCODE)
    }

    & "$repoRoot\scripts\build-agent.ps1"
    if ($LASTEXITCODE -ne 0) {
        throw ("Agent 构建失败，退出码：{0}" -f $LASTEXITCODE)
    }

    & "$repoRoot\scripts\smoke-agent.ps1" -SkipBuild
    if ($LASTEXITCODE -ne 0) {
        throw ("Agent smoke 失败，退出码：{0}" -f $LASTEXITCODE)
    }
}
finally {
    $env:GOCACHE = $previousGoCache
    $env:GOTELEMETRY = $previousGoTelemetry
    $env:npm_config_cache = $previousNpmCache
    $env:GOOS = $previousGoOS
    $env:GOARCH = $previousGoArch
}
