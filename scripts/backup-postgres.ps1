[CmdletBinding()]
param(
    [string]$DatabaseUrl = $env:TRAILSEAL_POSTGRES_URL,
    [string]$OutputDir = (Join-Path $PSScriptRoot "backups"),
    [string]$Label = ""
)

$ErrorActionPreference = "Stop"

if ([string]::IsNullOrWhiteSpace($DatabaseUrl)) {
    throw "Database URL is required. Set TRAILSEAL_POSTGRES_URL or pass -DatabaseUrl."
}

$pgDump = Get-Command pg_dump -ErrorAction SilentlyContinue
if (-not $pgDump) {
    throw "pg_dump was not found in PATH. Install PostgreSQL client tools first."
}

New-Item -ItemType Directory -Force -Path $OutputDir | Out-Null

$timestamp = Get-Date -Format "yyyyMMdd-HHmmss"
$suffix = if ([string]::IsNullOrWhiteSpace($Label)) { "" } else { "-$Label" }
$backupFile = Join-Path $OutputDir "trailseal-$timestamp$suffix.dump"

Write-Host "Creating PostgreSQL backup..."
Write-Host "Output: $backupFile"

& $pgDump.Source `
    "--dbname=$DatabaseUrl" `
    "--format=custom" `
    "--file=$backupFile" `
    "--no-owner" `
    "--verbose"

if ($LASTEXITCODE -ne 0) {
    throw "pg_dump failed with exit code $LASTEXITCODE."
}

if (-not (Test-Path $backupFile) -or ((Get-Item $backupFile).Length -eq 0)) {
    throw "Backup file was not created or is empty."
}

Write-Host "Backup completed successfully."
Write-Host "File: $backupFile"
Write-Host "Size: $([math]::Round((Get-Item $backupFile).Length / 1MB, 2)) MB"

$backupFile