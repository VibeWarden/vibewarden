# vibew.ps1 — zero-install wrapper for the VibeWarden CLI (Windows PowerShell)
#
# Usage: irm https://vibewarden.dev/vibew.ps1 | iex
#   Or: copy this file into your project and commit it.
#
# The script:
#   1. Reads the required version from .vibewarden-version (or queries the
#      GitHub API for the latest release).
#   2. Downloads vibewarden_<version>_windows_amd64.zip from GitHub Releases.
#   3. Downloads the checksum file and verifies the SHA256 hash.
#   4. Extracts the binary from the archive.
#   5. Caches the binary at %USERPROFILE%\.vibewarden\bin\vibewarden-<version>.exe
#   6. Forwards all arguments and preserves the exit code.
#
# Environment variables:
#   GITHUB_TOKEN — optional; avoids GitHub API rate limits.

[CmdletBinding()]
param(
    [Parameter(ValueFromRemainingArguments = $true)]
    [string[]]$PassThruArgs
)

$ErrorActionPreference = 'Stop'

$Repo       = 'vibewarden/vibewarden'
$CacheDir   = Join-Path $env:USERPROFILE '.vibewarden\bin'
$VersionFile = '.vibewarden-version'

# ---------------------------------------------------------------------------
# Resolve-Version — read from .vibewarden-version or query GitHub API
# ---------------------------------------------------------------------------
function Resolve-Version {
    if (Test-Path $VersionFile) {
        return (Get-Content $VersionFile -Raw).Trim()
    }

    $headers = @{ 'Accept' = 'application/vnd.github+json' }
    if ($env:GITHUB_TOKEN) {
        $headers['Authorization'] = "Bearer $env:GITHUB_TOKEN"
    }

    $response = Invoke-RestMethod -Uri "https://api.github.com/repos/$Repo/releases/latest" `
        -Headers $headers -UseBasicParsing
    if (-not $response.tag_name) {
        throw 'Could not determine the latest VibeWarden version.'
    }
    return $response.tag_name
}

# ---------------------------------------------------------------------------
# Get-File — download a URL to a local path
# ---------------------------------------------------------------------------
function Get-File {
    param(
        [string]$Url,
        [string]$Dest
    )
    $headers = @{}
    if ($env:GITHUB_TOKEN) {
        $headers['Authorization'] = "Bearer $env:GITHUB_TOKEN"
    }
    Invoke-WebRequest -Uri $Url -OutFile $Dest -Headers $headers -UseBasicParsing
}

# ---------------------------------------------------------------------------
# Confirm-Checksum — verify SHA256 hash against a checksums file
# ---------------------------------------------------------------------------
function Confirm-Checksum {
    param(
        [string]$FilePath,
        [string]$ChecksumsPath
    )
    $filename = Split-Path $FilePath -Leaf
    $lines    = Get-Content $ChecksumsPath
    $expected = $null
    foreach ($line in $lines) {
        # Format: <sha256>  <filename>
        if ($line -match "^([0-9a-f]{64})\s+$([regex]::Escape($filename))$") {
            $expected = $Matches[1]
            break
        }
    }
    if (-not $expected) {
        throw "No checksum entry found for '$filename' in checksums file."
    }

    $actual = (Get-FileHash -Algorithm SHA256 -Path $FilePath).Hash.ToLower()
    if ($actual -ne $expected) {
        throw "Checksum mismatch for '$filename'.`n  expected: $expected`n  actual:   $actual"
    }
}

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------
$Version      = Resolve-Version
$CleanVersion = $Version -replace '^v', ''
$ArchiveName  = "vibewarden_${CleanVersion}_windows_amd64.zip"
$CachedBin    = Join-Path $CacheDir "vibewarden-$Version.exe"

if (-not (Test-Path $CachedBin)) {
    $BaseUrl      = "https://github.com/$Repo/releases/download/$Version"
    $ArchiveUrl   = "$BaseUrl/$ArchiveName"
    $ChecksumsUrl = "$BaseUrl/checksums.txt"

    $TmpDir = [System.IO.Path]::GetTempPath() + [System.IO.Path]::GetRandomFileName()
    New-Item -ItemType Directory -Path $TmpDir | Out-Null

    try {
        Write-Host "Downloading VibeWarden $Version (windows/amd64)..." -ForegroundColor Cyan
        Get-File -Url $ArchiveUrl   -Dest (Join-Path $TmpDir $ArchiveName)
        Get-File -Url $ChecksumsUrl -Dest (Join-Path $TmpDir 'checksums.txt')

        Confirm-Checksum -FilePath (Join-Path $TmpDir $ArchiveName) `
                         -ChecksumsPath (Join-Path $TmpDir 'checksums.txt')

        Expand-Archive -Path (Join-Path $TmpDir $ArchiveName) -DestinationPath $TmpDir -Force

        if (-not (Test-Path $CacheDir)) {
            New-Item -ItemType Directory -Path $CacheDir | Out-Null
        }
        Move-Item (Join-Path $TmpDir 'vibewarden.exe') $CachedBin
    } finally {
        Remove-Item $TmpDir -Recurse -Force -ErrorAction SilentlyContinue
    }
}

& $CachedBin @PassThruArgs
exit $LASTEXITCODE
