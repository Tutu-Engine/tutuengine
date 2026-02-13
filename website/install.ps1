# ─────────────────────────────────────────────────────────────────────────────
# TuTu Installer — Windows PowerShell
# Enterprise-grade installer with retry, verification, and waiting mechanisms.
#
# Usage: irm tutuengine.tech/install.ps1 | iex
#
# Environment variables:
#   $env:TUTU_VERSION      Override version (e.g., "v0.2.0")
#   $env:TUTU_INSTALL_DIR  Override install directory
#   $env:TUTU_HOME         Override TuTu home directory
# ─────────────────────────────────────────────────────────────────────────────
$ErrorActionPreference = "Stop"

$repo = "Tutu-Engine/tutuengine"
$binary = "tutu.exe"
$maxRetries = 3
$retryDelay = 2

Write-Host ""
Write-Host "  ████████╗██╗   ██╗████████╗██╗   ██╗" -ForegroundColor Magenta
Write-Host "  ╚══██╔══╝██║   ██║╚══██╔══╝██║   ██║" -ForegroundColor Magenta
Write-Host "     ██║   ██║   ██║   ██║   ██║   ██║" -ForegroundColor Magenta
Write-Host "     ██║   ╚██████╔╝   ██║   ╚██████╔╝" -ForegroundColor Magenta
Write-Host "     ╚═╝    ╚═════╝    ╚═╝    ╚═════╝" -ForegroundColor Magenta
Write-Host ""
Write-Host "  The Local-First AI Runtime" -ForegroundColor White
Write-Host ""
Write-Host "  Installing TuTu for Windows..." -ForegroundColor Cyan
Write-Host ""

# ─── Platform Detection ─────────────────────────────────────────────────────
$arch = if ([Environment]::Is64BitOperatingSystem) { "amd64" } else { "386" }
# Detect ARM64 Windows
if ($env:PROCESSOR_ARCHITECTURE -eq "ARM64" -or $env:PROCESSOR_IDENTIFIER -match "ARM") {
    $arch = "arm64"
}

# ─── Install Directory ──────────────────────────────────────────────────────
$installDir = if ($env:TUTU_INSTALL_DIR) { $env:TUTU_INSTALL_DIR } else { "$env:LOCALAPPDATA\TuTu\bin" }
if (-not (Test-Path $installDir)) {
    New-Item -ItemType Directory -Path $installDir -Force | Out-Null
}

# ─── TuTu Home ──────────────────────────────────────────────────────────────
$tutuHome = if ($env:TUTU_HOME) { $env:TUTU_HOME } else { "$env:USERPROFILE\.tutu" }
foreach ($subDir in @("bin", "models", "keys")) {
    $sub = Join-Path $tutuHome $subDir
    if (-not (Test-Path $sub)) { New-Item -ItemType Directory -Path $sub -Force | Out-Null }
}

# ─── Version Resolution with Retry ──────────────────────────────────────────
$version = $env:TUTU_VERSION
if (-not $version) {
    [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12 -bor [Net.SecurityProtocolType]::Tls13

    for ($attempt = 1; $attempt -le $maxRetries; $attempt++) {
        try {
            $release = Invoke-RestMethod "https://api.github.com/repos/$repo/releases/latest" `
                -Headers @{"User-Agent"="TuTu-Installer/2.0"} -TimeoutSec 15 -ErrorAction Stop
            $version = $release.tag_name
            Write-Host "  Latest version: $version" -ForegroundColor Green
            break
        }
        catch {
            if ($attempt -lt $maxRetries) {
                $delay = $retryDelay * $attempt
                Write-Host "  Attempt $attempt/$maxRetries failed. Retrying in ${delay}s..." -ForegroundColor Yellow
                Start-Sleep -Seconds $delay
            }
        }
    }
    if (-not $version) {
        $version = "v0.1.0"
        Write-Host "  Using default version: $version" -ForegroundColor Yellow
    }
} else {
    Write-Host "  Using specified version: $version" -ForegroundColor Green
}

# ─── Check Existing Installation ────────────────────────────────────────────
$existingPath = Get-Command tutu -ErrorAction SilentlyContinue
if ($existingPath) {
    $existingVersion = & $existingPath.Source --version 2>$null
    Write-Host "  Existing: $existingVersion" -ForegroundColor Gray
}

# ─── Download with Retry & Exponential Backoff ──────────────────────────────
$url = "https://github.com/$repo/releases/download/$version/tutu-windows-$arch.exe"
$tmpFile = Join-Path $env:TEMP "tutu-download-$([guid]::NewGuid().ToString('N').Substring(0,8)).exe"

Write-Host "  Downloading $url..." -ForegroundColor Cyan

$downloadSuccess = $false
for ($attempt = 1; $attempt -le $maxRetries; $attempt++) {
    try {
        [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12 -bor [Net.SecurityProtocolType]::Tls13

        # Use WebClient for progress & speed
        $webClient = New-Object System.Net.WebClient
        $webClient.Headers.Add("User-Agent", "TuTu-Installer/2.0")
        $webClient.DownloadFile($url, $tmpFile)
        $downloadSuccess = $true
        break
    }
    catch {
        try {
            Invoke-WebRequest -Uri $url -OutFile $tmpFile -UseBasicParsing -TimeoutSec 120 `
                -Headers @{"User-Agent"="TuTu-Installer/2.0"}
            $downloadSuccess = $true
            break
        }
        catch {
            if ($attempt -lt $maxRetries) {
                $delay = $retryDelay * $attempt
                Write-Host "  Attempt $attempt/$maxRetries failed. Retrying in ${delay}s..." -ForegroundColor Yellow
                Start-Sleep -Seconds $delay
            }
        }
    }
}

if (-not $downloadSuccess) {
    Write-Host ""
    Write-Host "  Download failed after $maxRetries attempts." -ForegroundColor Red
    Write-Host ""
    Write-Host "  Build from source (requires Go 1.24+):" -ForegroundColor Cyan
    Write-Host "    git clone https://github.com/$repo.git"
    Write-Host "    cd tutuengine ; go build -o tutu.exe .\cmd\tutu"
    Write-Host ""
    exit 1
}

# ─── Multi-Layer Verification ───────────────────────────────────────────────
Write-Host "  Verifying download..." -ForegroundColor Cyan

# Layer 1: HTML check
$firstBytes = [System.IO.File]::ReadAllBytes($tmpFile) | Select-Object -First 100
$firstText = [System.Text.Encoding]::ASCII.GetString($firstBytes)
if ($firstText -match "<!DOCTYPE|<html|Not Found") {
    Remove-Item $tmpFile -Force -ErrorAction SilentlyContinue
    Write-Host "  Download failed — received HTML instead of binary." -ForegroundColor Red
    exit 1
}

# Layer 2: File size check (> 1MB)
$fileSize = (Get-Item $tmpFile).Length
if ($fileSize -lt 1048576) {
    Remove-Item $tmpFile -Force -ErrorAction SilentlyContinue
    Write-Host "  Downloaded file too small ($fileSize bytes)." -ForegroundColor Red
    exit 1
}
$sizeMB = [math]::Round($fileSize / 1048576, 1)
Write-Host "  Size: ${sizeMB} MB" -ForegroundColor Green

# Layer 3: PE header check (Windows executables start with "MZ")
$peHeader = [System.Text.Encoding]::ASCII.GetString($firstBytes[0..1])
if ($peHeader -ne "MZ") {
    Write-Host "  Warning: File does not appear to be a Windows executable." -ForegroundColor Yellow
}

# Layer 4: Execution test
try {
    $testOutput = & $tmpFile --version 2>$null
    if ($testOutput) {
        Write-Host "  Execution test: passed ($testOutput)" -ForegroundColor Green
    }
} catch {
    Write-Host "  Execution test: could not verify" -ForegroundColor Yellow
}

# ─── Install ─────────────────────────────────────────────────────────────────
$dest = Join-Path $installDir $binary
Move-Item -Path $tmpFile -Destination $dest -Force
Write-Host "  Installed to $dest" -ForegroundColor Green

# ─── PATH Management ────────────────────────────────────────────────────────
$currentPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($currentPath -notlike "*$installDir*") {
    [Environment]::SetEnvironmentVariable("Path", "$currentPath;$installDir", "User")
    $env:Path = "$env:Path;$installDir"
    Write-Host "  Added $installDir to PATH" -ForegroundColor Green
}

# ─── Verify (with waiting) ──────────────────────────────────────────────────
$verified = $false
for ($wait = 0; $wait -lt 5; $wait++) {
    if (Test-Path $dest) {
        try {
            $installedVersion = & $dest --version 2>$null
            if ($installedVersion) {
                $verified = $true
                break
            }
        } catch {}
    }
    Start-Sleep -Seconds 1
}

Write-Host ""
if ($verified) {
    Write-Host "  ═══════════════════════════════════════════" -ForegroundColor Green
    Write-Host "    TuTu installed successfully! ($installedVersion)" -ForegroundColor Green
    Write-Host "  ═══════════════════════════════════════════" -ForegroundColor Green
} else {
    Write-Host "  TuTu installed to $dest" -ForegroundColor Green
}
Write-Host ""
Write-Host "  Get started:" -ForegroundColor Cyan
Write-Host "    tutu run llama3.2        # Chat with Llama 3.2"
Write-Host "    tutu run phi3            # Chat with Phi-3"
Write-Host "    tutu run qwen2.5         # Chat with Qwen 2.5"
Write-Host "    tutu serve               # Start API server"
Write-Host "    tutu --help              # See all commands"
Write-Host ""
Write-Host "  API endpoints:" -ForegroundColor Cyan
Write-Host "    Ollama:   http://localhost:11434/api/chat"
Write-Host "    OpenAI:   http://localhost:11434/v1/chat/completions"
Write-Host "    MCP:      http://localhost:11434/mcp"
Write-Host ""
Write-Host "  Docs: https://tutuengine.tech/docs.html" -ForegroundColor Gray
Write-Host ""
Write-Host "  Restart your terminal for PATH changes to take effect." -ForegroundColor Yellow
