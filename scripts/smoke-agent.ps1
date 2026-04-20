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
$binaryPath = Join-Path $repoRoot "dist\\message-share-agent.exe"

if (-not $SkipBuild) {
    & (Join-Path $scriptDir "build-agent.ps1")
}

if (-not (Test-Path $binaryPath)) {
    throw "Agent binary not found: $binaryPath"
}

$smokeRoot = Join-Path $repoRoot ".tmp\\smoke-agent"
$timestamp = Get-Date -Format "yyyyMMddHHmmss"
$runDir = Join-Path $smokeRoot ("run-" + $timestamp)
$runtimeDir = Join-Path $runDir "runtime"
$homeDir = Join-Path $runDir "home"
$basePort = Get-Random -Minimum 52000 -Maximum 58000
$agentTcpPort = $basePort
$acceleratedDataPort = $basePort + 1
$discoveryUdpPort = $basePort + 2
$localHttpPort = $basePort + 3

New-Item -ItemType Directory -Force -Path $runtimeDir | Out-Null
New-Item -ItemType Directory -Force -Path $homeDir | Out-Null

$previousUserProfile = $env:USERPROFILE
$previousHome = $env:HOME
$previousDataDir = $env:MESSAGE_SHARE_DATA_DIR
$previousAgentTcpPort = $env:MESSAGE_SHARE_AGENT_TCP_PORT
$previousLocalHttpPort = $env:MESSAGE_SHARE_LOCAL_HTTP_PORT
$previousAcceleratedDataPort = $env:MESSAGE_SHARE_ACCELERATED_DATA_PORT
$previousDiscoveryUdpPort = $env:MESSAGE_SHARE_DISCOVERY_UDP_PORT
$previousDiscoveryListenAddr = $env:MESSAGE_SHARE_DISCOVERY_LISTEN_ADDR
$previousDiscoveryBroadcastAddr = $env:MESSAGE_SHARE_DISCOVERY_BROADCAST_ADDR
$process = $null

try {
    $env:USERPROFILE = $homeDir
    $env:HOME = $homeDir
    $env:MESSAGE_SHARE_DATA_DIR = $runtimeDir
    $env:MESSAGE_SHARE_AGENT_TCP_PORT = $agentTcpPort
    $env:MESSAGE_SHARE_LOCAL_HTTP_PORT = $localHttpPort
    $env:MESSAGE_SHARE_ACCELERATED_DATA_PORT = $acceleratedDataPort
    $env:MESSAGE_SHARE_DISCOVERY_UDP_PORT = $discoveryUdpPort
    $env:MESSAGE_SHARE_DISCOVERY_LISTEN_ADDR = "127.0.0.1:$discoveryUdpPort"
    $env:MESSAGE_SHARE_DISCOVERY_BROADCAST_ADDR = "127.0.0.1:$discoveryUdpPort"

    $process = Start-Process -FilePath $binaryPath -WorkingDirectory $runDir -PassThru -WindowStyle Hidden
    $configPath = Join-Path $runtimeDir "config.json"
    $bootstrapUrl = "http://127.0.0.1:$localHttpPort/api/bootstrap"
    $rootUrl = "http://127.0.0.1:$localHttpPort/"
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    $runtimeReady = $false
    $bootstrapReady = $false
    $rootReady = $false

    while ((Get-Date) -lt $deadline) {
        if (-not $runtimeReady -and (Test-Path $configPath)) {
            $runtimeReady = $true
        }

        if (-not $bootstrapReady) {
            try {
                $bootstrapResponse = Invoke-WebRequest -UseBasicParsing -Uri $bootstrapUrl
                if ($bootstrapResponse.StatusCode -eq 200) {
                    $payload = $bootstrapResponse.Content | ConvertFrom-Json
                    if ($null -ne $payload.localDeviceName -and $payload.localDeviceName -ne "") {
                        $bootstrapReady = $true
                    }
                }
            }
            catch {
            }
        }

        if (-not $rootReady) {
            try {
                $rootResponse = Invoke-WebRequest -UseBasicParsing -Uri $rootUrl
                if ($rootResponse.StatusCode -eq 200) {
                    $rootReady = $true
                }
            }
            catch {
            }
        }

        if ($runtimeReady -and $bootstrapReady -and $rootReady) {
            break
        }

        $process.Refresh()
        if ($process.HasExited) {
            break
        }
        Start-Sleep -Milliseconds 250
    }

    $process.Refresh()
    if (-not $runtimeReady) {
        if ($process.HasExited) {
            throw ("Agent exited before runtime dir initialization, exit code {0}" -f $process.ExitCode)
        }
        throw ("Agent did not initialize runtime dir within {0}s: {1}" -f $TimeoutSeconds, $configPath)
    }
    if (-not $bootstrapReady) {
        if ($process.HasExited) {
            throw ("Agent exited before localhost bootstrap became ready, exit code {0}" -f $process.ExitCode)
        }
        throw ("Agent localhost bootstrap was not ready within {0}s: {1}" -f $TimeoutSeconds, $bootstrapUrl)
    }
    if (-not $rootReady) {
        if ($process.HasExited) {
            throw ("Agent exited before localhost root page became ready, exit code {0}" -f $process.ExitCode)
        }
        throw ("Agent localhost root page was not ready within {0}s: {1}" -f $TimeoutSeconds, $rootUrl)
    }

    Start-Sleep -Milliseconds $StabilityMilliseconds
    $process.Refresh()
    if ($process.HasExited) {
        throw ("Agent exited during post-ready stability window, exit code {0}" -f $process.ExitCode)
    }

    Write-Host "Agent smoke passed: localhost Web UI and bootstrap API are reachable."
}
finally {
    if ($process -and -not $process.HasExited) {
        Stop-Process -Id $process.Id -Force
        $process.WaitForExit()
    }
    $env:USERPROFILE = $previousUserProfile
    $env:HOME = $previousHome
    $env:MESSAGE_SHARE_DATA_DIR = $previousDataDir
    $env:MESSAGE_SHARE_AGENT_TCP_PORT = $previousAgentTcpPort
    $env:MESSAGE_SHARE_LOCAL_HTTP_PORT = $previousLocalHttpPort
    $env:MESSAGE_SHARE_ACCELERATED_DATA_PORT = $previousAcceleratedDataPort
    $env:MESSAGE_SHARE_DISCOVERY_UDP_PORT = $previousDiscoveryUdpPort
    $env:MESSAGE_SHARE_DISCOVERY_LISTEN_ADDR = $previousDiscoveryListenAddr
    $env:MESSAGE_SHARE_DISCOVERY_BROADCAST_ADDR = $previousDiscoveryBroadcastAddr
}
