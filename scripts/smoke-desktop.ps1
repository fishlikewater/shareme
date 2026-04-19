param(
    [switch]$SkipBuild,
    [int]$TimeoutSeconds = 12,
    [int]$StabilityMilliseconds = 1000
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
$binaryPath = Join-Path $backendDir "build\\bin\\message-share.exe"

if (-not $SkipBuild) {
    & (Join-Path $scriptDir "build-desktop.ps1") -Platform "windows/amd64"
}

if (-not (Test-Path $binaryPath)) {
    throw "Desktop binary not found: $binaryPath"
}

$smokeRoot = Join-Path $repoRoot ".tmp\\smoke-desktop"
$timestamp = Get-Date -Format "yyyyMMddHHmmss"
$runDir = Join-Path $smokeRoot ("run-" + $timestamp)
$runtimeDir = Join-Path $runDir "runtime"
$homeDir = Join-Path $runDir "home"
$uiReadyMarker = Join-Path $runDir "ui-ready.marker"
$basePort = Get-Random -Minimum 52000 -Maximum 58000
$agentTcpPort = $basePort
$acceleratedDataPort = $basePort + 1
$discoveryUdpPort = $basePort + 2

New-Item -ItemType Directory -Force -Path $runtimeDir | Out-Null
New-Item -ItemType Directory -Force -Path $homeDir | Out-Null

$previousUserProfile = $env:USERPROFILE
$previousHome = $env:HOME
$previousDataDir = $env:MESSAGE_SHARE_DATA_DIR
$previousUIReadyMarker = $env:MESSAGE_SHARE_UI_READY_MARKER
$previousAgentTcpPort = $env:MESSAGE_SHARE_AGENT_TCP_PORT
$previousAcceleratedDataPort = $env:MESSAGE_SHARE_ACCELERATED_DATA_PORT
$previousDiscoveryUdpPort = $env:MESSAGE_SHARE_DISCOVERY_UDP_PORT
$process = $null

try {
    $env:USERPROFILE = $homeDir
    $env:HOME = $homeDir
    $env:MESSAGE_SHARE_DATA_DIR = $runtimeDir
    $env:MESSAGE_SHARE_UI_READY_MARKER = $uiReadyMarker
    $env:MESSAGE_SHARE_AGENT_TCP_PORT = $agentTcpPort
    $env:MESSAGE_SHARE_ACCELERATED_DATA_PORT = $acceleratedDataPort
    $env:MESSAGE_SHARE_DISCOVERY_UDP_PORT = $discoveryUdpPort

    $process = Start-Process -FilePath $binaryPath -WorkingDirectory $runDir -PassThru
    $configPath = Join-Path $runtimeDir "config.json"
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    $runtimeReady = $false
    $uiReady = $false

    while ((Get-Date) -lt $deadline) {
        if (Test-Path $configPath) {
            $runtimeReady = $true
        }
        if (Test-Path $uiReadyMarker) {
            $uiReady = $true
        }
        if ($runtimeReady -and $uiReady) {
            break
        }

        $process.Refresh()
        if ($process.HasExited) {
            break
        }

        Start-Sleep -Milliseconds 250
    }

    $process.Refresh()
    if (-not $runtimeReady -and (Test-Path $configPath)) {
        $runtimeReady = $true
    }
    if (-not $uiReady -and (Test-Path $uiReadyMarker)) {
        $uiReady = $true
    }

    if (-not $runtimeReady) {
        if ($process.HasExited) {
            throw ("Desktop app exited before runtime dir initialization, exit code {0}" -f $process.ExitCode)
        }
        throw ("Desktop app did not initialize runtime dir within {0}s: {1}" -f $TimeoutSeconds, $configPath)
    }
    if (-not $uiReady) {
        if ($process.HasExited) {
            throw ("Desktop app exited before main UI reported ready, exit code {0}" -f $process.ExitCode)
        }
        throw ("Desktop app did not report main UI ready within {0}s: {1}" -f $TimeoutSeconds, $uiReadyMarker)
    }
    $markerValues = @{}
    foreach ($line in Get-Content -LiteralPath $uiReadyMarker) {
        if ($line -match '^([^=]+)=(.*)$') {
            $markerValues[$matches[1]] = $matches[2]
        }
    }
    if (($markerValues["agentTcpPort"] -as [int]) -ne $agentTcpPort) {
        throw ("Desktop app did not honor smoke agent port override: expected {0}, got {1}" -f $agentTcpPort, $markerValues["agentTcpPort"])
    }
    if (($markerValues["acceleratedDataPort"] -as [int]) -ne $acceleratedDataPort) {
        throw ("Desktop app did not honor smoke accelerated port override: expected {0}, got {1}" -f $acceleratedDataPort, $markerValues["acceleratedDataPort"])
    }
    if (($markerValues["discoveryUdpPort"] -as [int]) -ne $discoveryUdpPort) {
        throw ("Desktop app did not honor smoke discovery port override: expected {0}, got {1}" -f $discoveryUdpPort, $markerValues["discoveryUdpPort"])
    }

    Start-Sleep -Milliseconds $StabilityMilliseconds
    $process.Refresh()
    if ($process.HasExited) {
        throw ("Desktop app exited during post-ready stability window, exit code {0}" -f $process.ExitCode)
    }

    if ($process.HasExited) {
        if ($process.ExitCode -ne 0) {
            throw ("Desktop app exited with failure during smoke test, exit code {0}" -f $process.ExitCode)
        }
        Write-Host "Desktop smoke passed: runtime dir initialized and main UI reported ready before app exited cleanly."
    }
    else {
        Write-Host "Desktop smoke passed: app started, runtime dir initialized, and main UI reported ready."
    }
}
finally {
    if ($process -and -not $process.HasExited) {
        Stop-Process -Id $process.Id -Force
        $process.WaitForExit()
    }
    $env:USERPROFILE = $previousUserProfile
    $env:HOME = $previousHome
    $env:MESSAGE_SHARE_DATA_DIR = $previousDataDir
    $env:MESSAGE_SHARE_UI_READY_MARKER = $previousUIReadyMarker
    $env:MESSAGE_SHARE_AGENT_TCP_PORT = $previousAgentTcpPort
    $env:MESSAGE_SHARE_ACCELERATED_DATA_PORT = $previousAcceleratedDataPort
    $env:MESSAGE_SHARE_DISCOVERY_UDP_PORT = $previousDiscoveryUdpPort
}
