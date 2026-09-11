# tizen-cert installer for Windows.
# Usage: irm https://tizen-cert-cli.vercel.app/install.ps1 | iex
$ErrorActionPreference = "Stop"

$repo = "gifflet/tizen-cert-cli"
$dir = Join-Path $env:LOCALAPPDATA "Programs\tizen-cert"
$exe = Join-Path $dir "tizen-cert.exe"
$url = "https://github.com/$repo/releases/latest/download/tizen-cert-windows-amd64.exe"

New-Item -ItemType Directory -Force -Path $dir | Out-Null
Write-Host "Downloading tizen-cert (latest)..."
Invoke-WebRequest -Uri $url -OutFile $exe

# Add the install dir to the user PATH when missing.
$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($userPath -notlike "*$dir*") {
    [Environment]::SetEnvironmentVariable("Path", "$userPath;$dir", "User")
    $env:Path = "$env:Path;$dir"
    Write-Host "Added $dir to your user PATH (restart other terminals to pick it up)."
}

$version = & $exe --version
Write-Host "Installed: $exe ($version)"
