# install.ps1 — Download and install the VibeWarden CLI on Windows.
#
# Usage:
#   irm vibewarden.dev/install.ps1 | iex
#
# Options:
#   $env:VERSION = "v0.1.0"      Install a specific version
#   $env:INSTALL_DIR = "C:\bin"   Where to put the binary (default: current dir)

$ErrorActionPreference = "Stop"

$Repo = "vibewarden/vibewarden"
$BinaryName = "vibew.exe"

function Write-Log($msg) { Write-Host "[vibewarden] $msg" -ForegroundColor Cyan }
function Write-Ok($msg)  { Write-Host "[vibewarden] $msg" -ForegroundColor Green }
function Write-Fail($msg) { Write-Host "[vibewarden] $msg" -ForegroundColor Red; exit 1 }

# --- Detect architecture ---
$Arch = if ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture -eq "Arm64") { "arm64" } else { "amd64" }

# --- Resolve version ---
if (-not $env:VERSION) {
    Write-Log "Resolving latest version..."
    $release = Invoke-RestMethod -Uri "https://api.github.com/repos/$Repo/releases/latest"
    $Version = $release.tag_name
    Write-Log "Latest version: $Version"
} else {
    $Version = $env:VERSION
    Write-Log "Using specified version: $Version"
}

$CleanVersion = $Version.TrimStart("v")
$Archive = "vibewarden_${CleanVersion}_windows_${Arch}.zip"
$BaseUrl = "https://github.com/$Repo/releases/download/$Version"

# --- Install directory ---
$InstallDir = if ($env:INSTALL_DIR) { $env:INSTALL_DIR } else { "." }

# --- Download ---
$TmpDir = New-TemporaryFile | ForEach-Object { Remove-Item $_; New-Item -ItemType Directory -Path $_ }

Write-Log "Downloading $Archive..."
Invoke-WebRequest -Uri "$BaseUrl/$Archive" -OutFile "$TmpDir\$Archive"
Invoke-WebRequest -Uri "$BaseUrl/checksums.txt" -OutFile "$TmpDir\checksums.txt"

# --- Verify checksum ---
$ExpectedLine = Get-Content "$TmpDir\checksums.txt" | Where-Object { $_ -match $Archive }
if ($ExpectedLine) {
    $Expected = ($ExpectedLine -split "\s+")[0]
    $Actual = (Get-FileHash -Algorithm SHA256 "$TmpDir\$Archive").Hash.ToLower()
    if ($Actual -ne $Expected) {
        Write-Fail "Checksum mismatch!`n  expected: $Expected`n  actual:   $Actual"
    }
    Write-Log "Checksum verified"
} else {
    Write-Log "Warning: no checksum found for $Archive"
}

# --- Extract ---
Write-Log "Extracting..."
Expand-Archive -Path "$TmpDir\$Archive" -DestinationPath "$TmpDir\extracted" -Force

# --- Install ---
$Dest = Join-Path $InstallDir $BinaryName
Copy-Item "$TmpDir\extracted\$BinaryName" $Dest -Force

# --- Cleanup ---
Remove-Item -Recurse -Force $TmpDir

Write-Ok "Installed to $Dest"
Write-Host ""
Write-Ok "Get started:"
Write-Host "  vibew init --upstream 3000"
Write-Host "  vibew dev"
Write-Host ""
Write-Ok "Docs: https://vibewarden.dev/docs/"
