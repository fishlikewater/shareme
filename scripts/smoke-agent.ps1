param(
    [string]$AgentPath = "",
    [string]$BaseUrl = "http://127.0.0.1:19180",
    [int]$AgentTCPPort = 19080,
    [int]$DiscoveryUDPPort = 19081,
    [int]$AcceleratedDataPort = 19082,
    [int]$TimeoutSeconds = 30
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8

$scriptPath = Get-Variable -Name PSCommandPath -ValueOnly -ErrorAction SilentlyContinue
if (-not $scriptPath) {
    $scriptPath = $MyInvocation.MyCommand.Definition
}
if (-not $scriptPath) {
    throw "Unable to determine script path."
}

$scriptDir = Split-Path -Parent $scriptPath
$repoRoot = Split-Path -Parent $scriptDir
if ([string]::IsNullOrWhiteSpace($AgentPath)) {
    $AgentPath = Join-Path $repoRoot "backend\message-share-agent.exe"
}
if (-not (Test-Path $AgentPath)) {
    throw "Agent executable not found: $AgentPath"
}

$baseUri = [System.Uri]$BaseUrl
$localApiAddr = $baseUri.Authority
$smokeRoot = Join-Path $env:TEMP "MessageShareSmoke"
$smokeStamp = Get-Date -Format "yyyyMMddHHmmss"
$dataDir = Join-Path $smokeRoot "run-$smokeStamp"
$downloadDir = Join-Path $dataDir "downloads"
New-Item -ItemType Directory -Path $downloadDir -Force | Out-Null

$previousLocalAPIAddr = $env:MESSAGE_SHARE_LOCAL_API_ADDR
$previousAgentTCPPort = $env:MESSAGE_SHARE_AGENT_TCP_PORT
$previousDiscoveryUDPPort = $env:MESSAGE_SHARE_DISCOVERY_UDP_PORT
$previousAcceleratedDataPort = $env:MESSAGE_SHARE_ACCELERATED_DATA_PORT
$previousDataDir = $env:MESSAGE_SHARE_DATA_DIR
$previousDownloadDir = $env:MESSAGE_SHARE_DOWNLOAD_DIR
$previousDeviceName = $env:MESSAGE_SHARE_DEVICE_NAME

$process = $null
try {
    $resolvedAgentPath = [System.IO.Path]::GetFullPath($AgentPath)
    $env:MESSAGE_SHARE_LOCAL_API_ADDR = $localApiAddr
    $env:MESSAGE_SHARE_AGENT_TCP_PORT = $AgentTCPPort.ToString()
    $env:MESSAGE_SHARE_DISCOVERY_UDP_PORT = $DiscoveryUDPPort.ToString()
    $env:MESSAGE_SHARE_ACCELERATED_DATA_PORT = $AcceleratedDataPort.ToString()
    $env:MESSAGE_SHARE_DATA_DIR = $dataDir
    $env:MESSAGE_SHARE_DOWNLOAD_DIR = $downloadDir
    $env:MESSAGE_SHARE_DEVICE_NAME = "Smoke Agent"

    $startInfo = New-Object System.Diagnostics.ProcessStartInfo
    $startInfo.FileName = $resolvedAgentPath
    $startInfo.WorkingDirectory = Split-Path -Parent $resolvedAgentPath
    $startInfo.UseShellExecute = $false
    $startInfo.RedirectStandardOutput = $true
    $startInfo.RedirectStandardError = $true

    $process = [System.Diagnostics.Process]::Start($startInfo)
    if (-not $process) {
        throw "Failed to start agent process."
    }

    $health = $null
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    while ((Get-Date) -lt $deadline) {
        Start-Sleep -Milliseconds 500
        try {
            $health = Invoke-RestMethod -Uri ($BaseUrl + "/api/health")
            if ($health) {
                break
            }
        }
        catch {
            $health = $null
        }
    }
    if (-not $health) {
        throw "Agent did not become healthy within the timeout."
    }

    $bootstrap = Invoke-RestMethod -Uri ($BaseUrl + "/api/bootstrap")
    $uiResponse = Invoke-WebRequest -Uri ($BaseUrl + "/") -UseBasicParsing
    if ($uiResponse.StatusCode -ne 200) {
        throw ("Web UI root returned non-200 status: {0}" -f $uiResponse.StatusCode)
    }
    if ($uiResponse.Content -notmatch '<div id="root"></div>' -and $uiResponse.Content -notmatch "LAN P2P Share") {
        throw "Web UI root did not return embedded page content."
    }
    $assetMatches = [regex]::Matches($uiResponse.Content, '(?:src|href)="(?<path>/assets/[^"]+)"')
    if ($assetMatches.Count -eq 0) {
        throw "Web UI root did not reference embedded asset files."
    }

    $assetResponses = @()
    foreach ($assetMatch in $assetMatches) {
        $assetPath = $assetMatch.Groups["path"].Value
        $assetResponse = Invoke-WebRequest -Uri ($BaseUrl + $assetPath) -UseBasicParsing
        if ($assetResponse.StatusCode -ne 200) {
            throw ("Web UI asset returned non-200 status: {0} {1}" -f $assetResponse.StatusCode, $assetPath)
        }
        if ($assetResponse.Content.Length -le 0) {
            throw ("Web UI asset returned empty content: {0}" -f $assetPath)
        }
        $assetResponses += [PSCustomObject]@{
            path   = $assetPath
            status = $assetResponse.StatusCode
            length = $assetResponse.Content.Length
        }
    }

    Write-Host "Health response:"
    $health | ConvertTo-Json -Depth 8
    Write-Host "Bootstrap response:"
    $bootstrap | ConvertTo-Json -Depth 8
    Write-Host "Web UI response:"
    Write-Host ("status {0}, content length {1}" -f $uiResponse.StatusCode, $uiResponse.Content.Length)
    Write-Host "Web UI assets:"
    $assetResponses | ConvertTo-Json -Depth 8
}
finally {
    $env:MESSAGE_SHARE_LOCAL_API_ADDR = $previousLocalAPIAddr
    $env:MESSAGE_SHARE_AGENT_TCP_PORT = $previousAgentTCPPort
    $env:MESSAGE_SHARE_DISCOVERY_UDP_PORT = $previousDiscoveryUDPPort
    $env:MESSAGE_SHARE_ACCELERATED_DATA_PORT = $previousAcceleratedDataPort
    $env:MESSAGE_SHARE_DATA_DIR = $previousDataDir
    $env:MESSAGE_SHARE_DOWNLOAD_DIR = $previousDownloadDir
    $env:MESSAGE_SHARE_DEVICE_NAME = $previousDeviceName

    if ($process) {
        if (-not $process.HasExited) {
            try {
                $null = $process.CloseMainWindow()
            }
            catch {
            }
            Start-Sleep -Seconds 1
        }
        if (-not $process.HasExited) {
            $process.Kill()
            $process.WaitForExit()
        }

        $stdout = $process.StandardOutput.ReadToEnd().Trim()
        $stderr = $process.StandardError.ReadToEnd().Trim()
        if ($stdout) {
            Write-Host "Agent stdout:"
            Write-Host $stdout
        }
        if ($stderr) {
            Write-Host "Agent stderr:"
            Write-Host $stderr
        }
        $process.Dispose()
    }
}
