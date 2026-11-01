[CmdletBinding(SupportsShouldProcess)]
param(
    [Parameter(Mandatory = $true)]
    [string]$BackupFile,

    [string]$DatabaseUrl = $env:TRAILSEAL_POSTGRES_URL,

    [switch]$Force
)

$ErrorActionPreference = "Stop"

if (-not (Test-Path $BackupFile -PathType Leaf)) {
    throw "Backup file not found: $BackupFile"
}

if ([string]::IsNullOrWhiteSpace($DatabaseUrl)) {
    throw "Database URL is required. Set TRAILSEAL_POSTGRES_URL or pass -DatabaseUrl."
}

$pgRestore = Get-Command pg_restore -ErrorAction SilentlyContinue
if (-not $pgRestore) {
    throw "pg_restore was not found in PATH. Install PostgreSQL client tools first."
}

if (-not $Force) {
    throw "Restore is destructive. Re-run with -Force after confirming the target database."
}

Write-Host "Restoring PostgreSQL backup..."
Write-Host "Backup:   $BackupFile"
Write-Host "Database: $DatabaseUrl"

if ($PSCmdlet.ShouldProcess($DatabaseUrl, "Restore $BackupFile")) {
    & $pgRestore.Source `
        "--dbname=$DatabaseUrl" `
        "--clean" `
        "--if-exists" `
        "--no-owner" `
        "--verbose" `
        $BackupFile

    if ($LASTEXITCODE -ne 0) {
        throw "pg_restore failed with exit code $LASTEXITCODE."
    }
}

Write-Host "Restore completed successfully."