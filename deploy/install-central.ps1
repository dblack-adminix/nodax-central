param(
    [switch]$SkipAdminCheck,
    [string]$ServiceName,
    [string]$DisplayName,
    [string]$InstallDir,
    [string]$DataDir,
    [int]$Port,
    [string]$BinarySource,
    [switch]$SetupCaddy,
    [string]$CaddyDomain,
    [string]$CaddyBinarySource,
    [string]$CaddyInstallDir,
    [string]$CaddyConfigDir,
    [Parameter(ValueFromRemainingArguments = $true)]
    [string[]]$ForwardArgs
)

$ErrorActionPreference = "Stop"

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path

$platform = [System.Environment]::OSVersion.Platform
$isWinPS = $platform -eq [System.PlatformID]::Win32NT
$isWindowsHost = $isWinPS -or (($null -ne $IsWindows) -and $IsWindows)
$isLinuxHost = (($null -ne $IsLinux) -and $IsLinux) -or ($platform -eq [System.PlatformID]::Unix)
$cleanArgs = @()
if ($ForwardArgs) {
    $cleanArgs = @($ForwardArgs | Where-Object { -not [string]::IsNullOrWhiteSpace($_) })
}

if ($isWindowsHost) {
    if (-not $SkipAdminCheck) {
        $isAdmin = ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
        if (-not $isAdmin) {
            throw "Run PowerShell as Administrator or use -SkipAdminCheck"
        }
    }

    $target = Join-Path $scriptDir "deploy-windows.ps1"
    if (-not (Test-Path $target)) {
        throw "Missing script: $target"
    }

    Write-Host "Detected Windows. Running deploy-windows.ps1" -ForegroundColor Cyan
    $winArgs = @{}
    foreach ($name in @(
        'ServiceName',
        'DisplayName',
        'InstallDir',
        'DataDir',
        'Port',
        'BinarySource',
        'CaddyDomain',
        'CaddyBinarySource',
        'CaddyInstallDir',
        'CaddyConfigDir'
    )) {
        if ($PSBoundParameters.ContainsKey($name)) {
            $winArgs[$name] = $PSBoundParameters[$name]
        }
    }
    if ($PSBoundParameters.ContainsKey('SetupCaddy') -and $SetupCaddy) {
        $winArgs['SetupCaddy'] = $true
    }

    & $target @winArgs @cleanArgs
    exit $LASTEXITCODE
}

if ($isLinuxHost) {
    $target = Join-Path $scriptDir "deploy-linux.sh"
    if (-not (Test-Path $target)) {
        throw "Missing script: $target"
    }

    Write-Host "Detected Linux. Running deploy-linux.sh" -ForegroundColor Cyan

    if (Get-Command bash -ErrorAction SilentlyContinue) {
        & bash $target @cleanArgs
    } else {
        throw "bash is required to run deploy-linux.sh"
    }
    exit $LASTEXITCODE
}

throw "Unsupported OS. Use deploy-windows.ps1 or deploy-linux.sh manually."
