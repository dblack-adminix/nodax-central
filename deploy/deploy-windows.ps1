param(
    [string]$ServiceName = "NODAXCentral",
    [string]$DisplayName = "NODAX Central",
    [string]$InstallDir = "C:\Program Files\NodaxCentral",
    [string]$DataDir = "C:\ProgramData\NodaxCentral",
    [int]$Port = 8080,
    [string]$BinarySource = ".\nodax-central.exe",
    [switch]$SetupCaddy,
    [string]$CaddyDomain = "",
    [string]$CaddyBinarySource = ".\caddy.exe",
    [string]$CaddyInstallDir = "C:\Program Files\Caddy",
    [string]$CaddyConfigDir = "C:\ProgramData\Caddy"
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

    if (Test-Path $TargetExe) {
        return
    }

    New-Item -ItemType Directory -Path $InstallPath -Force | Out-Null

    if (Test-Path $SourceExe) {
        Copy-Item $SourceExe $TargetExe -Force
        return
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

    # Fallback: direct download from official Caddy download API
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
    if (Test-Path $TargetExe) { return }

    throw "Caddy install failed. Place caddy.exe next to script or install Caddy manually."
}

Step "[1/9] Check administrator rights"
$isAdmin = ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
if (-not $isAdmin) { throw "Run PowerShell as Administrator" }
Ok "Administrator rights confirmed"

Step "[2/9] Check binary"
if (-not (Test-Path $BinarySource)) { throw "Binary not found: $BinarySource" }

Step "[3/9] Prepare directories"
$backupDir = Join-Path $DataDir "backups"
New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
New-Item -ItemType Directory -Path $DataDir -Force | Out-Null
New-Item -ItemType Directory -Path $backupDir -Force | Out-Null
Ok "Directories are ready"

Step "[4/9] Stop service (if exists)"
$svc = Get-Service -Name $ServiceName -ErrorAction SilentlyContinue
if ($svc -and $svc.Status -ne 'Stopped') {
    Stop-Service -Name $ServiceName -Force
    $svc.WaitForStatus('Stopped', [TimeSpan]::FromSeconds(30))
}

Step "[5/9] Backup database (if exists)"
$dbCandidates = @(
    (Join-Path $DataDir "nodax-central.db"),
    (Join-Path $DataDir "nodax-central.sqlite")
)
$stamp = Get-Date -Format "yyyyMMdd-HHmmss"
foreach ($db in $dbCandidates) {
    if (Test-Path $db) {
        Copy-Item $db (Join-Path $backupDir ("{0}.{1}.bak" -f (Split-Path $db -Leaf), $stamp)) -Force
    }
}
Ok "Backup phase completed"

Step "[6/9] Install binary"
$targetExe = Join-Path $InstallDir "nodax-central.exe"
Copy-Item $BinarySource $targetExe -Force
Ok "Binary updated: $targetExe"

Step "[7/9] Set port environment variable"
[Environment]::SetEnvironmentVariable("NODAX_CENTRAL_PORT", "$Port", "Machine")
Ok "NODAX_CENTRAL_PORT=$Port"

Step "[8/9] Create/update service"
$existingSvc = Get-Service -Name $ServiceName -ErrorAction SilentlyContinue
if (-not $existingSvc) {
    sc.exe create $ServiceName binPath= "`"$targetExe`"" start= auto DisplayName= "`"$DisplayName`"" | Out-Null
} else {
    sc.exe config $ServiceName binPath= "`"$targetExe`"" start= auto displayname= "`"$DisplayName`"" | Out-Null
}
sc.exe description $ServiceName "`"NODAX Central service`"" | Out-Null
sc.exe failure $ServiceName reset= 86400 actions= restart/60000/restart/60000/restart/60000 | Out-Null
Start-Service -Name $ServiceName
Ok "Service started: $ServiceName"

Step "[9/11] Smoke-check"
Start-Sleep -Seconds 2
$resp = Invoke-WebRequest -Uri "http://127.0.0.1:$Port/" -UseBasicParsing -TimeoutSec 10
if ($resp.StatusCode -lt 200 -or $resp.StatusCode -ge 400) {
    throw "Smoke check failed, HTTP $($resp.StatusCode)"
}
Ok "Smoke check passed: http://127.0.0.1:$Port/"

if ($SetupCaddy) {
    Step "[10/11] Setup Caddy reverse proxy"
    if ([string]::IsNullOrWhiteSpace($CaddyDomain)) {
        throw "CaddyDomain is required when -SetupCaddy is used"
    }

    New-Item -ItemType Directory -Path $CaddyInstallDir -Force | Out-Null
    New-Item -ItemType Directory -Path $CaddyConfigDir -Force | Out-Null

    $caddyExe = Join-Path $CaddyInstallDir "caddy.exe"
    Install-CaddyIfMissing -TargetExe $caddyExe -SourceExe $CaddyBinarySource -InstallPath $CaddyInstallDir

    $caddyfilePath = Join-Path $CaddyConfigDir "Caddyfile"
    @(
        "$CaddyDomain {",
        "    encode gzip zstd",
        "",
        "    reverse_proxy 127.0.0.1:$Port {",
        "        header_up X-Forwarded-Proto {scheme}",
        "        header_up X-Forwarded-For {remote_host}",
        "        header_up X-Forwarded-Host {host}",
        "    }",
        "}"
    ) | Set-Content $caddyfilePath -Encoding ascii

    $caddySvc = Get-Service -Name "Caddy" -ErrorAction SilentlyContinue
    $caddyBinPath = "`"$caddyExe`" run --environ --config `"$caddyfilePath`""
    if (-not $caddySvc) {
        sc.exe create Caddy binPath= $caddyBinPath start= auto | Out-Null
    } else {
        sc.exe config Caddy binPath= $caddyBinPath start= auto | Out-Null
    }
    Start-Service Caddy
    Ok "Caddy configured and started"

    Step "[11/11] Check Caddy service"
    $caddyStatus = (Get-Service -Name Caddy).Status
    if ($caddyStatus -ne 'Running') {
        throw "Caddy service is not running"
    }
    Ok "Caddy service is running"
}

Write-Host "\nDeploy completed" -ForegroundColor Cyan
Write-Host "Service: $ServiceName"
Write-Host "Binary:  $targetExe"
Write-Host "DataDir: $DataDir"
if ($SetupCaddy) {
    Write-Host "Caddy:   enabled ($CaddyDomain)"
}
