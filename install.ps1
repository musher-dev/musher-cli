# Install script for Musher CLI
# Usage: irm https://get.musher.dev/install.ps1 | iex
#   or:  .\install.ps1 [-Version 0.3.2] [-InstallDir C:\path] [-Yes]
#
# Options:
#   -Version VERSION   Install a specific version (default: latest)
#   -InstallDir DIR    Install to DIR (default: $env:LOCALAPPDATA\musher\bin)
#   -Yes               Skip confirmation prompts
#   -Help              Show this help message

param(
    [string]$Version = "",
    [string]$InstallDir = "",
    [switch]$Yes,
    [switch]$Help
)

$ErrorActionPreference = "Stop"

$Repo = "musher-dev/musher-cli"
$Binary = "musher"
$BaseUrl = "https://github.com/$Repo"

# ── Helpers ──────────────────────────────────────────────────────────────────

function Write-Err {
    param([string]$Message)
    Write-Host "Error: $Message" -ForegroundColor Red
    exit 1
}

function Write-Bold {
    param([string]$Message)
    if ($Host.UI.SupportsVirtualTerminal) {
        Write-Host "`e[1m$Message`e[0m"
    } else {
        Write-Host $Message
    }
}

function Write-Green {
    param([string]$Message)
    Write-Host $Message -ForegroundColor Green
}

function Write-Yellow {
    param([string]$Message)
    Write-Host $Message -ForegroundColor Yellow
}

function Show-Usage {
    Write-Host @"
Install Musher CLI

Usage:
  install.ps1 [options]

Options:
  -Version VERSION   Install a specific version (default: latest)
  -InstallDir DIR    Install to DIR (default: `$env:LOCALAPPDATA\musher\bin)
  -Yes               Skip confirmation prompts
  -Help              Show this help message

Examples:
  irm https://get.musher.dev/install.ps1 | iex
  .\install.ps1 -Version 0.3.2
  .\install.ps1 -InstallDir C:\tools\musher
"@
}

# ── Platform detection ───────────────────────────────────────────────────────

function Get-Arch {
    $arch = $env:PROCESSOR_ARCHITECTURE
    switch ($arch) {
        "AMD64" { return "amd64" }
        "ARM64" { return "arm64" }
        default { Write-Err "Unsupported architecture: $arch" }
    }
}

# ── Version resolution ───────────────────────────────────────────────────────

function Resolve-LatestVersion {
    try {
        $response = Invoke-WebRequest -Uri "$BaseUrl/releases/latest" `
            -MaximumRedirection 0 `
            -ErrorAction SilentlyContinue `
            -UseBasicParsing 2>$null
    } catch {
        $response = $_.Exception.Response
    }

    if ($null -eq $response) {
        Write-Err "Failed to resolve latest version. Check $BaseUrl/releases"
    }

    $location = ""
    if ($response.Headers -and $response.Headers["Location"]) {
        $location = $response.Headers["Location"]
        if ($location -is [array]) { $location = $location[0] }
    }

    if (-not $location) {
        # PowerShell 5.1: try the final URL from a followed redirect
        try {
            $followed = Invoke-WebRequest -Uri "$BaseUrl/releases/latest" `
                -UseBasicParsing -ErrorAction Stop
            $location = $followed.BaseResponse.ResponseUri.AbsoluteUri
            if (-not $location) {
                $location = $followed.BaseResponse.RequestMessage.RequestUri.AbsoluteUri
            }
        } catch {
            Write-Err "Failed to resolve latest version. Check $BaseUrl/releases"
        }
    }

    if (-not $location) {
        Write-Err "Failed to resolve latest version. Check $BaseUrl/releases"
    }

    $tag = $location.Split("/")[-1]
    if (-not $tag) {
        Write-Err "Could not parse version from redirect URL: $location"
    }
    return $tag
}

# ── Checksum verification ────────────────────────────────────────────────────

function Test-Checksum {
    param(
        [string]$File,
        [string]$ChecksumsFile,
        [string]$ArchiveName
    )

    $checksums = Get-Content $ChecksumsFile
    $expected = ""
    foreach ($line in $checksums) {
        $parts = $line -split '\s+'
        if ($parts.Length -ge 2 -and $parts[1] -eq $ArchiveName) {
            $expected = $parts[0]
            break
        }
    }

    if (-not $expected) {
        Write-Err "Archive '$ArchiveName' not found in checksums file"
    }

    $actual = (Get-FileHash -Path $File -Algorithm SHA256).Hash.ToLower()
    if ($actual -ne $expected.ToLower()) {
        Write-Err @"
Checksum mismatch!
  Expected: $expected
  Actual:   $actual
"@
    }
}

# ── PATH helpers ─────────────────────────────────────────────────────────────

function Test-InPath {
    param([string]$Dir)
    $paths = [Environment]::GetEnvironmentVariable("Path", "User") -split ";"
    foreach ($p in $paths) {
        if ($p.TrimEnd("\") -eq $Dir.TrimEnd("\")) { return $true }
    }
    return $false
}

function Add-ToPath {
    param([string]$Dir)
    $current = [Environment]::GetEnvironmentVariable("Path", "User")
    if ($current) {
        $newPath = "$current;$Dir"
    } else {
        $newPath = $Dir
    }
    [Environment]::SetEnvironmentVariable("Path", $newPath, "User")
}

# ── Main ─────────────────────────────────────────────────────────────────────

function Main {
    if ($Help) {
        Show-Usage
        return
    }

    $Arch = Get-Arch
    if (-not $InstallDir) {
        $InstallDir = Join-Path $env:LOCALAPPDATA "musher\bin"
    }

    Write-Bold "Musher CLI Installer"
    Write-Host ""

    if ($Version) {
        $Version = $Version.TrimStart("v")
        $Tag = "v$Version"
    } else {
        Write-Host "Resolving latest version..."
        $Tag = Resolve-LatestVersion
        $Version = $Tag.TrimStart("v")
    }

    $ArchiveName = "${Binary}_${Version}_windows_${Arch}.zip"
    $ArchiveUrl = "$BaseUrl/releases/download/$Tag/$ArchiveName"
    $ChecksumsUrl = "$BaseUrl/releases/download/$Tag/checksums.txt"

    Write-Host "  Version:  $Tag"
    Write-Host "  Platform: windows/$Arch"
    Write-Host "  Target:   $InstallDir\$Binary.exe"
    Write-Host ""

    if (-not $Yes -and [Environment]::UserInteractive) {
        $reply = Read-Host "Proceed with installation? [Y/n]"
        if ($reply -match "^[nN]") {
            Write-Host "Aborted."
            return
        }
    }

    $TmpDir = Join-Path ([System.IO.Path]::GetTempPath()) "musher-install-$([System.Guid]::NewGuid().ToString('N').Substring(0,8))"
    New-Item -ItemType Directory -Path $TmpDir -Force | Out-Null

    try {
        Write-Host "Downloading $ArchiveName..."
        Invoke-WebRequest -Uri $ArchiveUrl -OutFile (Join-Path $TmpDir $ArchiveName) -UseBasicParsing

        Write-Host "Downloading checksums..."
        Invoke-WebRequest -Uri $ChecksumsUrl -OutFile (Join-Path $TmpDir "checksums.txt") -UseBasicParsing

        Write-Host "Verifying checksum..."
        Test-Checksum -File (Join-Path $TmpDir $ArchiveName) `
            -ChecksumsFile (Join-Path $TmpDir "checksums.txt") `
            -ArchiveName $ArchiveName

        Write-Host "Extracting..."
        Expand-Archive -Path (Join-Path $TmpDir $ArchiveName) -DestinationPath $TmpDir -Force

        if (-not (Test-Path $InstallDir)) {
            New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
        }
        Copy-Item -Path (Join-Path $TmpDir "$Binary.exe") -Destination (Join-Path $InstallDir "$Binary.exe") -Force

        Write-Host ""
        Write-Green "Successfully installed musher $Tag to $InstallDir\$Binary.exe"
        Write-Host ""

        if (-not (Test-InPath $InstallDir)) {
            Write-Yellow "Warning: $InstallDir is not in your PATH."
            Write-Host ""

            if (-not $Yes -and [Environment]::UserInteractive) {
                $reply = Read-Host "Add $InstallDir to your user PATH? [Y/n]"
                if ($reply -notmatch "^[nN]") {
                    Add-ToPath $InstallDir
                    Write-Green "Added to PATH. Restart your terminal for the change to take effect."
                    Write-Host ""
                }
            } else {
                Write-Host "Add it to your PATH:"
                Write-Host "  `$env:Path = `"$InstallDir;`$env:Path`""
                Write-Host ""
                Write-Host "To persist across sessions:"
                Write-Host "  [Environment]::SetEnvironmentVariable('Path', `"$InstallDir;`" + [Environment]::GetEnvironmentVariable('Path', 'User'), 'User')"
            }
        }

        Write-Host "Get started:"
        Write-Host "  musher --help"
        Write-Host "  musher login"
    } finally {
        Remove-Item -Path $TmpDir -Recurse -Force -ErrorAction SilentlyContinue
    }
}

Main
