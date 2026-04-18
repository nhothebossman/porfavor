$ErrorActionPreference = "Stop"

$Repo = "nhothebossman/porfavor"
$Bin  = "porfavor.exe"

# Detect architecture
$Arch = if ([System.Environment]::Is64BitOperatingSystem) {
    if ($env:PROCESSOR_ARCHITECTURE -eq "ARM64") { "arm64" } else { "amd64" }
} else {
    Write-Host "  Error: 32-bit Windows is not supported." -ForegroundColor Red
    exit 1
}

$FileName = "porfavor-windows-$Arch.exe"
$InstallDir = "$env:LOCALAPPDATA\porfavor"
$InstallPath = "$InstallDir\$Bin"

# Get latest release download URL
Write-Host ""
Write-Host "  Por Favor installer" -ForegroundColor Green
Write-Host "  ════════════════════════════════════" -ForegroundColor Green
Write-Host "  Arch:    $Arch" -ForegroundColor Green
Write-Host "  Target:  $InstallPath" -ForegroundColor Green
Write-Host ""

$ApiUrl = "https://api.github.com/repos/$Repo/releases/latest"
$Release = Invoke-RestMethod -Uri $ApiUrl -Headers @{ "User-Agent" = "porfavor-installer" }
$Asset = $Release.assets | Where-Object { $_.name -eq $FileName }

if (-not $Asset) {
    Write-Host "  Error: could not find $FileName in latest release." -ForegroundColor Red
    exit 1
}

$DownloadUrl = $Asset.browser_download_url

# Download
Write-Host "  Downloading $FileName..." -ForegroundColor Green
New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
Invoke-WebRequest -Uri $DownloadUrl -OutFile $InstallPath

# Add to PATH for current user if not already there
$UserPath = [System.Environment]::GetEnvironmentVariable("Path", "User")
if ($UserPath -notlike "*$InstallDir*") {
    [System.Environment]::SetEnvironmentVariable(
        "Path",
        "$UserPath;$InstallDir",
        "User"
    )
    Write-Host "  Added $InstallDir to PATH." -ForegroundColor Green
    Write-Host "  Restart your terminal for PATH to take effect." -ForegroundColor Green
}

Write-Host ""
Write-Host "  Installed successfully. Run: porfavor.exe" -ForegroundColor Green
Write-Host ""
