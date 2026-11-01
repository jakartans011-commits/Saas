<#
.SYNOPSIS
    E2E Backup & Restore test for TrailSeal PostgreSQL.
    Creates isolated database, backup, restore, verifies hash chain integrity.
.DESCRIPTION
    This script tests the complete backup and restore flow:
    1. Creates a test database with 3 chained events
    2. Verifies source data integrity
    3. Creates a pg_dump backup
    4. Restores to an isolated database
    5. Verifies all data, hashes, and chain relationships
.NOTES
    Requires Docker with PostgreSQL container running.
    Uses TRAILSEAL_POSTGRES_URL environment variable for connection.
#>

[CmdletBinding()]
param(
    [string]$Container = "trailseal-postgres",
    [string]$Database = "trailseal",
    [string]$User = "trailseal"
)

$ErrorActionPreference = "Stop"

function Invoke-DockerPsql {
    param(
        [Parameter(Mandatory = $true)]
        [string]$TargetDb,

        [Parameter(Mandatory = $true)]
        [string]$Sql
    )

    $output = $Sql | docker exec -i $Container psql `
        -U $User `
        -d $TargetDb `
        -v ON_ERROR_STOP=1 `
        2>&1

    if ($LASTEXITCODE -ne 0) {
        Write-Host ($output | Out-String)
        throw "psql failed."
    }

    return $output
}

function Get-Sha256Hex {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Value
    )

    $bytes = [System.Text.Encoding]::UTF8.GetBytes($Value)
    $sha = [System.Security.Cryptography.SHA256]::Create()

    try {
        $hash = $sha.ComputeHash($bytes)
    }
    finally {
        $sha.Dispose()
    }

    return (-join ($hash | ForEach-Object { $_.ToString("x2") }))
}

function Format-UtcTimestamp {
    param(
        [Parameter(Mandatory = $true)]
        [DateTime]$Value
    )

    $utc = $Value.ToUniversalTime()
    return $utc.ToString("yyyy-MM-dd'T'HH:mm:ss.ffffff'Z'", [System.Globalization.CultureInfo]::InvariantCulture)
}

function Compute-EventHash {
    param(
        [string]$ProjectId,
        [long]$Sequence,
        [DateTime]$OccurredAt,
        [string]$EventType,
        [string]$ActorId,
        [string]$Payload,
        [string]$PrevHash
    )

    $timestamp = Format-UtcTimestamp $OccurredAt

    $canonical = $ProjectId + "|" +
        $Sequence.ToString([System.Globalization.CultureInfo]::InvariantCulture) + "|" +
        $timestamp + "|" +
        $EventType + "|" +
        $ActorId + "|" +
        $Payload + "|" +
        $PrevHash

    return Get-Sha256Hex $canonical
}

# === VALIDATION ===

if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
    throw "docker was not found in PATH."
}

$running = docker inspect -f '{{.State.Running}}' $Container 2>$null

if ($LASTEXITCODE -ne 0 -or $running -ne "true") {
    throw "PostgreSQL container '$Container' is not running. Start it with: docker compose up -d"
}

# === SETUP ===

$stamp = Get-Date -Format "yyyyMMddHHmmssfff"
$tempDb = "trailseal_restore_test_$stamp"
$tenantId = "e2e-tenant-$stamp"
$projectId = [guid]::NewGuid().ToString()

$containerDump = "/tmp/trailseal-e2e-$stamp.dump"
$backupDir = Join-Path $PSScriptRoot "backups"
$hostDump = Join-Path $backupDir "e2e-$stamp.dump"

New-Item -ItemType Directory -Force -Path $backupDir | Out-Null

$createdDatabase = $false

try {
    # === CREATE RESTORE DATABASE ===
    Write-Host "Creating isolated restore database: $tempDb"
    Invoke-DockerPsql -TargetDb "postgres" -Sql "CREATE DATABASE `"$tempDb`";" | Out-Null
    $createdDatabase = $true

    # === CREATE PROJECT ===
    Write-Host "Creating E2E project: $projectId"
    $projectSql = @"
INSERT INTO projects (id, tenant_id, name, status)
VALUES ('$projectId', '$tenantId', 'E2E Backup Restore Test', 'active');
"@
    Invoke-DockerPsql -TargetDb $Database -Sql $projectSql | Out-Null

    # === PREPARE EVENTS ===
    Write-Host "Creating 3 chained events..."

    # Align timestamps to microsecond precision (PostgreSQL timestamptz precision)
    $occurred1 = [DateTime]::UtcNow.AddSeconds(-2).AddTicks(-([DateTime]::UtcNow.AddSeconds(-2).Ticks % 10))
    $occurred2 = [DateTime]::UtcNow.AddSeconds(-1).AddTicks(-([DateTime]::UtcNow.AddSeconds(-1).Ticks % 10))
    $occurred3 = [DateTime]::UtcNow.AddTicks(-([DateTime]::UtcNow.Ticks % 10))

    $payload1 = '{"message": "backup", "step": 1}'
    $payload2 = '{"message": "restore", "step": 2}'
    $payload3 = '{"message": "verify", "step": 3}'

    $eventType = "backup.test"
    $actorId = "e2e"

    $hash1 = Compute-EventHash -ProjectId $projectId -Sequence 1 -OccurredAt $occurred1 -EventType $eventType -ActorId $actorId -Payload $payload1 -PrevHash ""
    $hash2 = Compute-EventHash -ProjectId $projectId -Sequence 2 -OccurredAt $occurred2 -EventType $eventType -ActorId $actorId -Payload $payload2 -PrevHash $hash1
    $hash3 = Compute-EventHash -ProjectId $projectId -Sequence 3 -OccurredAt $occurred3 -EventType $eventType -ActorId $actorId -Payload $payload3 -PrevHash $hash2

    $ts1 = Format-UtcTimestamp $occurred1
    $ts2 = Format-UtcTimestamp $occurred2
    $ts3 = Format-UtcTimestamp $occurred3

    # === INSERT EVENTS ===
    $eventsSql = @"
INSERT INTO events (project_id, tenant_id, sequence, event_type, actor_id, payload, prev_hash, hash, occurred_at)
VALUES
(
    '$projectId', '$tenantId', 1, '$eventType', '$actorId',
    jsonb_build_object('message', 'backup', 'step', 1),
    NULL, '$hash1', '$ts1'::timestamptz
),
(
    '$projectId', '$tenantId', 2, '$eventType', '$actorId',
    jsonb_build_object('message', 'restore', 'step', 2),
    '$hash1', '$hash2', '$ts2'::timestamptz
),
(
    '$projectId', '$tenantId', 3, '$eventType', '$actorId',
    jsonb_build_object('message', 'verify', 'step', 3),
    '$hash2', '$hash3', '$ts3'::timestamptz
);

INSERT INTO project_chain_state (project_id, last_sequence, last_hash)
VALUES ('$projectId', 3, '$hash3');
"@

    Invoke-DockerPsql -TargetDb $Database -Sql $eventsSql | Out-Null
    Write-Host "3 chained events created."

    # === VERIFY SOURCE ===
    Write-Host "Verifying source chain..."
    $sourceSql = "SELECT COUNT(*) AS event_count FROM events WHERE project_id = '$projectId';"
    $sourceResult = Invoke-DockerPsql -TargetDb $Database -Sql $sourceSql

    if (($sourceResult | Out-String) -notmatch "\b3\b") {
        throw "Source verification failed: expected 3 events."
    }
    Write-Host "Source verified: 3 events."

    # === PG_DUMP ===
    Write-Host "Running pg_dump inside Docker..."
    docker exec $Container pg_dump -U $User -d $Database -Fc --no-owner -f $containerDump

    if ($LASTEXITCODE -ne 0) {
        throw "pg_dump failed."
    }

    # === COPY BACKUP ===
    Write-Host "Copying backup to host..."
    docker cp "${Container}:${containerDump}" $hostDump

    if ($LASTEXITCODE -ne 0 -or -not (Test-Path $hostDump)) {
        throw "docker cp failed or backup file not created."
    }

    # === PG_RESTORE ===
    Write-Host "Restoring backup into isolated database..."
    docker exec $Container pg_restore -U $User -d $tempDb --clean --if-exists --no-owner $containerDump

    if ($LASTEXITCODE -ne 0) {
        throw "pg_restore failed."
    }

    # === VERIFY RESTORED EVENTS ===
    Write-Host "Verifying restored events..."
    $restoreSql = @"
SELECT sequence, payload::text, COALESCE(prev_hash, '') AS prev_hash, hash
FROM events WHERE project_id = '$projectId' ORDER BY sequence;
"@
    $restoreResult = Invoke-DockerPsql -TargetDb $tempDb -Sql $restoreSql
    $restoreText = $restoreResult | Out-String

    if ($restoreText -notmatch [regex]::Escape($hash1)) { throw "Restored event 1 hash mismatch." }
    if ($restoreText -notmatch [regex]::Escape($hash2)) { throw "Restored event 2 hash mismatch." }
    if ($restoreText -notmatch [regex]::Escape($hash3)) { throw "Restored event 3 hash mismatch." }

    # === VERIFY PAYLOADS USING JSONB (fix for formatting differences) ===
    Write-Host "Verifying restored payloads (JSONB comparison)..."
    $payloadSql = @"
SELECT
    (SELECT payload::jsonb = jsonb_build_object('message', 'backup', 'step', 1)
     FROM events WHERE project_id = '$projectId' AND sequence = 1) AS ev1,
    (SELECT payload::jsonb = jsonb_build_object('message', 'restore', 'step', 2)
     FROM events WHERE project_id = '$projectId' AND sequence = 2) AS ev2,
    (SELECT payload::jsonb = jsonb_build_object('message', 'verify', 'step', 3)
     FROM events WHERE project_id = '$projectId' AND sequence = 3) AS ev3;
"@

    $payloadResult = Invoke-DockerPsql -TargetDb $tempDb -Sql $payloadSql
    $payloadText = $payloadResult | Out-String

    if ($payloadText -notmatch "t.*t.*t") {
        Write-Host "Payload verification failed. Result:"
        Write-Host $payloadText
        throw "Restored payload verification failed."
    }
    Write-Host "✅ Payloads verified (JSONB comparison)"

    # === VERIFY CHAIN ===
    Write-Host "Verifying hash-chain relationships..."
    $chainSql = @"
SELECT CASE
    WHEN
        (SELECT prev_hash FROM events WHERE project_id = '$projectId' AND sequence = 1) IS NULL
    AND (SELECT prev_hash FROM events WHERE project_id = '$projectId' AND sequence = 2) = '$hash1'
    AND (SELECT prev_hash FROM events WHERE project_id = '$projectId' AND sequence = 3) = '$hash2'
    AND (SELECT hash FROM events WHERE project_id = '$projectId' AND sequence = 1) = '$hash1'
    AND (SELECT hash FROM events WHERE project_id = '$projectId' AND sequence = 2) = '$hash2'
    AND (SELECT hash FROM events WHERE project_id = '$projectId' AND sequence = 3) = '$hash3'
    THEN 'PASS' ELSE 'FAIL'
END;
"@
    $chainResult = Invoke-DockerPsql -TargetDb $tempDb -Sql $chainSql

    if (($chainResult | Out-String) -notmatch "PASS") {
        throw "Hash-chain relationship verification failed."
    }
    Write-Host "✅ Hash-chain relationships verified"

    # === VERIFY CHAIN STATE ===
    Write-Host "Verifying chain state..."
    $stateSql = @"
SELECT last_sequence, last_hash
FROM project_chain_state WHERE project_id = '$projectId';
"@
    $stateResult = Invoke-DockerPsql -TargetDb $tempDb -Sql $stateSql
    $stateText = $stateResult | Out-String

    if ($stateText -notmatch "\b3\b" -or $stateText -notmatch [regex]::Escape($hash3)) {
        throw "Chain state verification failed."
    }
    Write-Host "✅ Chain state verified"

    # === FINAL ===
    Write-Host ""
    Write-Host "======================================"
    Write-Host "✅ E2E BACKUP/RESTORE PASS"
    Write-Host "======================================"
    Write-Host "Project      : $projectId"
    Write-Host "Events       : 3"
    Write-Host "Hash chain   : ✅ Verified"
    Write-Host "Payloads     : ✅ Verified"
    Write-Host "Chain state  : ✅ Verified"
    Write-Host "Backup file  : $hostDump"
    Write-Host "======================================"

} finally {
    if ($createdDatabase) {
        docker exec $Container psql -U $User -d postgres -v ON_ERROR_STOP=0 -c "DROP DATABASE IF EXISTS `"$tempDb`";" 2>$null | Out-Null
    }
    docker exec $Container rm -f $containerDump 2>$null | Out-Null
}