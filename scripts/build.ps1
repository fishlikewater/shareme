<#
.SYNOPSIS
  Unified build script for shareme -- builds desktop + agent and packages for release.
.DESCRIPTION
  Two modes:
    - All-targets mode (default): builds desktop for DesktopPlatforms, agent for all 6 targets,
      packages for DesktopPlatforms. Use for local release prep.
    - Single-platform CI mode (-Platform): builds desktop + agent + package for one platform only.
      Use in GitHub Actions matrix jobs.
.PARAMETER Platform
  Single platform for CI mode. When set, ignores -DesktopPlatforms, -DesktopOnly, -AgentOnly.
  Values: windows/amd64, windows/arm64, linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, darwin/universal.
.PARAMETER DesktopOnly
  Build desktop only, skip agent. (all-targets mode only)
.PARAMETER AgentOnly
  Build agent only, skip desktop. (all-targets mode only)
.PARAMETER SkipPackage
  Build but skip packaging step.
.PARAMETER DesktopPlatforms
  Comma-separated desktop platforms (all-targets mode only, default: windows/amd64).
.EXAMPLE
  # Local: build everything
  .\scripts\build.ps1

  # Local: build desktop only
  .\scripts\build.ps1 -DesktopOnly

  # CI: single platform
  .\scripts\build.ps1 -Platform windows/amd64
  .\scripts\build.ps1 -Platform linux/arm64
#>

param(
    [string]$Platform = "",
    [switch]$DesktopOnly,
    [switch]$AgentOnly,
    [switch]$SkipPackage,
    [string]$DesktopPlatforms = "windows/amd64"
)

$ErrorActionPreference = "Stop"
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$ProjectDir = Resolve-Path "$ScriptDir/.."

$AgentTargets = @(
    @{ GOOS = "windows"; GOARCH = "amd64" }
    @{ GOOS = "windows"; GOARCH = "arm64" }
    @{ GOOS = "linux";   GOARCH = "amd64" }
    @{ GOOS = "linux";   GOARCH = "arm64" }
    @{ GOOS = "darwin";  GOARCH = "amd64" }
    @{ GOOS = "darwin";  GOARCH = "arm64" }
)

# Determine mode
$CIMode = $Platform -ne ""
if ($CIMode) {
    $DesktopOnly = $false
    $AgentOnly = $false
}

$BuildAll = -not $DesktopOnly -and -not $AgentOnly

function Invoke-BuildDesktop {
    param([string]$P)
    & "$ScriptDir/build-desktop.ps1" -Platform $P
}

function Invoke-BuildAgent {
    param([string]$Goos, [string]$Goarch, [string]$Output = "")
    if ($Output -eq "") {
        $ext = if ($Goos -eq "windows") { ".exe" } else { "" }
        $Output = "dist/shareme-agent-${Goos}-${Goarch}${ext}"
    }
    & "$ScriptDir/build-agent.ps1" -GOOS $Goos -GOARCH $Goarch -Output $Output
}

function Invoke-CIBuild {
    param([string]$P)

    Write-Host "`n========== CI Build: $P =========="

    # Desktop
    Write-Host "`n--- Desktop ---"
    Invoke-BuildDesktop -P $P

    # Agent
    Write-Host "`n--- Agent ---"
    $bits = $P -split '/'
    $os = $bits[0]
    $arch = $bits[1]

    if ($P -eq "darwin/universal") {
        Invoke-BuildAgent -Goos "darwin" -Goarch "amd64" -Output "dist/shareme-agent-darwin-amd64"
        Invoke-BuildAgent -Goos "darwin" -Goarch "arm64" -Output "dist/shareme-agent-darwin-arm64"
    } else {
        Invoke-BuildAgent -Goos $os -Goarch $arch
    }

    # Package
    if (-not $SkipPackage) {
        Write-Host "`n--- Package ---"
        Invoke-Package -P $P
    }
}

function Invoke-Package {
    param([string]$P)

    $platformMap = @{
        "windows/amd64"    = @{ Name = "shareme-windows-amd64";    Ext = ".zip";    Desktop = "backend\build\bin\shareme.exe"; Agent = "dist\shareme-agent-windows-amd64.exe" }
        "windows/arm64"    = @{ Name = "shareme-windows-arm64";    Ext = ".zip";    Desktop = "backend\build\bin\shareme.exe"; Agent = "dist\shareme-agent-windows-arm64.exe" }
        "linux/amd64"      = @{ Name = "shareme-linux-amd64";      Ext = ".tar.gz"; Desktop = "backend\build\bin\shareme";    Agent = "dist\shareme-agent-linux-amd64" }
        "linux/arm64"      = @{ Name = "shareme-linux-arm64";      Ext = ".tar.gz"; Desktop = "backend\build\bin\shareme";    Agent = "dist\shareme-agent-linux-arm64" }
        "darwin/amd64"     = @{ Name = "shareme-darwin-amd64";     Ext = ".zip";    Desktop = "backend\build\bin\shareme.app"; Agent = "" }
        "darwin/arm64"     = @{ Name = "shareme-darwin-arm64";     Ext = ".zip";    Desktop = "backend\build\bin\shareme.app"; Agent = "" }
        "darwin/universal" = @{ Name = "shareme-darwin-universal"; Ext = ".zip";    Desktop = "backend\build\bin\shareme.app"; Agent = "" }
    }

    $info = $platformMap[$P]
    if (-not $info) {
        Write-Warning "Unknown platform for packaging: $P"
        return
    }

    $releaseDir = "$ProjectDir\dist\release"
    $pkgDir = "$ProjectDir\dist\package\$($info.Name)"

    New-Item -ItemType Directory -Force -Path $releaseDir, $pkgDir | Out-Null

    if (Test-Path $info.Desktop) {
        Copy-Item -Recurse -Force -LiteralPath $info.Desktop -Destination "$pkgDir\"
    } else {
        Write-Warning "Desktop artifact not found: $($info.Desktop)"
    }

    if ($P -eq "darwin/universal") {
        $amd64 = "$ProjectDir\dist\shareme-agent-darwin-amd64"
        $arm64 = "$ProjectDir\dist\shareme-agent-darwin-arm64"
        $universal = "$pkgDir\shareme-agent"
        if ((Test-Path $amd64) -and (Test-Path $arm64)) {
            Write-Host "Creating universal agent via lipo ..."
            & lipo -create -output $universal $amd64 $arm64
            if ($LASTEXITCODE -ne 0) { throw "lipo failed" }
        } else {
            Write-Warning "Universal agent skipped: missing darwin/amd64 or darwin/arm64 agent"
        }
    } elseif ($P -eq "darwin/amd64") {
        Copy-Item -Force "$ProjectDir\dist\shareme-agent-darwin-amd64" -Destination "$pkgDir\shareme-agent"
    } elseif ($P -eq "darwin/arm64") {
        Copy-Item -Force "$ProjectDir\dist\shareme-agent-darwin-arm64" -Destination "$pkgDir\shareme-agent"
    } else {
        if (Test-Path $info.Agent) {
            Copy-Item -Force -LiteralPath $info.Agent -Destination "$pkgDir\"
        } else {
            Write-Warning "Agent artifact not found: $($info.Agent)"
        }
    }

    $archive = "$releaseDir\$($info.Name)$($info.Ext)"
    Write-Host "Packaging $archive ..."

    if ($P.StartsWith("linux")) {
        & tar -czf $archive -C $pkgDir .
        if ($LASTEXITCODE -ne 0) { throw "tar failed" }
    } else {
        Compress-Archive -Path "$pkgDir\*" -DestinationPath $archive -Force
    }

    Write-Host "Package created: $archive"
}

# --- Main ---

if ($CIMode) {
    Invoke-CIBuild -P $Platform
    Write-Host "`n========== Done: dist\release\ =========="
    exit 0
}

# All-targets mode (local dev)
$platforms = $DesktopPlatforms -split ',' | ForEach-Object { $_.Trim() } | Where-Object { $_ }

if ($BuildAll -or $DesktopOnly) {
    Write-Host "`n========== Building Desktop =========="
    foreach ($p in $platforms) {
        Invoke-BuildDesktop -P $p
    }
}

if ($BuildAll -or $AgentOnly) {
    Write-Host "`n========== Building Agent =========="
    foreach ($t in $AgentTargets) {
        Invoke-BuildAgent -Goos $t.GOOS -Goarch $t.GOARCH
    }
}

if (-not $SkipPackage) {
    Write-Host "`n========== Packaging =========="
    foreach ($p in $platforms) {
        Invoke-Package -P $p
    }
    Write-Host "`n========== Release assets in dist\release\ =========="
}
