param(
    [Parameter(Mandatory = $true)]
    [string]$Domain,
    [int]$UpstreamPort = 8080,
    [string]$CaddyBinarySource = ".\caddy.exe",
    [string]$CaddyInstallDir = "C:\Program Files\Caddy",
    [string]$CaddyConfigDir = "C:\ProgramData\Caddy",
    [string]$ServiceName = "Caddy"
)

$ErrorActionPreference = "Stop"

function Step([string]$m) { Write-Host $m -ForegroundColor Yellow }
function Ok([string]$m) { Write-Host ("[OK] " + $m) -ForegroundColor Green }

function Install-CaddyIfMissing {
    param(
        [string]$TargetExe,
        [string]$SourceExe,
        [string]$InstallPath
    )

    if (Test-Path $TargetExe) { return }

    New-Item -ItemType Directory -Path $InstallPath -Force | Out-Null

    if (Test-Path $SourceExe) {
        Copy-Item $SourceExe $TargetExe -Force
        if (Test-Path $TargetExe) { return }
    }

    if (Get-Command winget -ErrorAction SilentlyContinue) {
        winget install --id CaddyServer.Caddy -e --accept-package-agreements --accept-source-agreements
        if (Test-Path $TargetExe) { return }
    }

    if (Get-Command choco -ErrorAction SilentlyContinue) {
        choco install caddy -y
        if (Test-Path $TargetExe) { return }
    }

    $caddyCmd = Get-Command caddy.exe -ErrorAction SilentlyContinue
    if ($caddyCmd -and (Test-Path $caddyCmd.Source)) {
        Copy-Item $caddyCmd.Source $TargetExe -Force
        if (Test-Path $TargetExe) { return }
    }

    $downloadUrl = "https://caddyserver.com/api/download?os=windows&arch=amd64"
    $tempExe = Join-Path $env:TEMP "nodax-caddy.exe"
    try {
        Invoke-WebRequest -Uri $downloadUrl -OutFile $tempExe -UseBasicParsing -TimeoutSec 180
        if (Test-Path $tempExe) {
            Copy-Item $tempExe $TargetExe -Force
        }
    } finally {
        if (Test-Path $tempExe) {
            Remove-Item $tempExe -Force -ErrorAction SilentlyContinue
        }
    }

    if (-not (Test-Path $TargetExe)) {
        throw "Caddy install failed. Place caddy.exe near script or install Caddy manually."
    }
}

Step "[1/7] Check administrator rights"
$isAdmin = ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
if (-not $isAdmin) { throw "Run PowerShell as Administrator" }
Ok "Administrator rights confirmed"

Step "[2/7] Prepare directories"
New-Item -ItemType Directory -Path $CaddyInstallDir -Force | Out-Null
New-Item -ItemType Directory -Path $CaddyConfigDir -Force | Out-Null
Ok "Directories are ready"

Step "[3/7] Install Caddy if missing"
$caddyExe = Join-Path $CaddyInstallDir "caddy.exe"
Install-CaddyIfMissing -TargetExe $caddyExe -SourceExe $CaddyBinarySource -InstallPath $CaddyInstallDir
Ok "Caddy binary: $caddyExe"

Step "[4/7] Generate Caddyfile"
$caddyfilePath = Join-Path $CaddyConfigDir "Caddyfile"
@(
    "$Domain {",
    "    encode gzip zstd",
    "",
    "    reverse_proxy 127.0.0.1:$UpstreamPort {",
    "        header_up X-Forwarded-Proto {scheme}",
    "        header_up X-Forwarded-For {remote_host}",
    "        header_up X-Forwarded-Host {host}",
    "    }",
    "}"
) | Set-Content $caddyfilePath -Encoding ascii
Ok "Config written: $caddyfilePath"

Step "[5/7] Validate config"
& $caddyExe validate --config $caddyfilePath
if ($LASTEXITCODE -ne 0) {
    throw "Caddy config validation failed"
}
Ok "Config is valid"

Step "[6/7] Create or update Caddy service"
$caddySvc = Get-Service -Name $ServiceName -ErrorAction SilentlyContinue
$caddyBinPath = "`"$caddyExe`" run --environ --config `"$caddyfilePath`""
if (-not $caddySvc) {
    New-Service -Name $ServiceName -BinaryPathName $caddyBinPath -DisplayName "Caddy" -StartupType Automatic
} else {
    sc.exe config $ServiceName binPath= $caddyBinPath start= auto | Out-Null
}

$caddySvc = Get-Service -Name $ServiceName -ErrorAction SilentlyContinue
if (-not $caddySvc) {
    throw "Service $ServiceName was not created"
}

try {
    Start-Service -Name $ServiceName
} catch {
    sc.exe qc $ServiceName | Out-Host
    sc.exe query $ServiceName | Out-Host
    throw
}
Ok "Service started: $ServiceName"

Step "[7/7] Final checks"
$svc = Get-Service $ServiceName
if ($svc.Status -ne 'Running') {
    throw "Service $ServiceName is not running"
}
Ok "Caddy is running"

Write-Host "\nDone" -ForegroundColor Cyan
Write-Host "Domain:   $Domain"
Write-Host "Upstream: 127.0.0.1:$UpstreamPort"
Write-Host "Config:   $caddyfilePath"
