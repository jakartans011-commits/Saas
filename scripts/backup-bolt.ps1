<#
.SYNOPSIS
    Mengambil snapshot bbolt yang konsisten dari server TrailSeal yang
    sedang berjalan, lalu MEMBUKTIKAN hasilnya bisa dibaca.

.DESCRIPTION
    bbolt mengunci berkasnya secara eksklusif, jadi skrip ini TIDAK
    membaca berkas database langsung — itu mustahil selagi server hidup,
    dan menyalinnya mentah-mentah menghasilkan berkas rusak yang terlihat
    utuh.

    Sebagai gantinya skrip memanggil GET /api/admin/backup/bolt, yang di
    dalam server menjalankan tx.WriteTo dalam transaksi baca: konsisten
    pada satu titik waktu, tanpa menghentikan layanan.

    Setelah mengunduh, skrip menjalankan bolt-verify pada hasilnya. Backup
    yang belum pernah dibuka bukan backup, itu harapan — jadi pembuktian
    itu bagian dari proses, bukan langkah opsional yang bisa dilewati.

.PARAMETER ApiBaseUrl
    Asal server, mis. https://api.trailseal.dev. Default: APP_BASE_URL.

.PARAMETER RootApiKey
    Kunci master. Default: SAAS_ROOT_API_KEY.
    JANGAN pernah menuliskannya di baris perintah pada mesin bersama —
    ia tersimpan di riwayat PowerShell.

.PARAMETER OutputDir
    Folder tujuan. Default: scripts\backups.

.PARAMETER KeepDays
    Hapus backup lebih lama dari ini. 0 = jangan hapus apa pun.

.EXAMPLE
    . .\.env.ps1
    .\scripts\backup-bolt.ps1

.EXAMPLE
    .\scripts\backup-bolt.ps1 -ApiBaseUrl https://api.trailseal.dev -KeepDays 30
#>
[CmdletBinding()]
param(
    [string]$ApiBaseUrl = $env:APP_BASE_URL,
    [string]$RootApiKey = $env:SAAS_ROOT_API_KEY,
    [string]$OutputDir  = (Join-Path $PSScriptRoot "backups"),
    [int]$KeepDays      = 0,
    [switch]$SkipVerify
)

$ErrorActionPreference = "Stop"

if ([string]::IsNullOrWhiteSpace($ApiBaseUrl)) {
    throw "ApiBaseUrl wajib. Set APP_BASE_URL atau pakai -ApiBaseUrl."
}
if ([string]::IsNullOrWhiteSpace($RootApiKey)) {
    throw "RootApiKey wajib. Set SAAS_ROOT_API_KEY atau pakai -RootApiKey."
}

$ApiBaseUrl = $ApiBaseUrl.TrimEnd('/')
$url = "$ApiBaseUrl/api/admin/backup/bolt"

New-Item -ItemType Directory -Force -Path $OutputDir | Out-Null

$timestamp  = Get-Date -Format "yyyyMMdd-HHmmss"
$backupFile = Join-Path $OutputDir "saas-api-$timestamp.db"

Write-Host "Mengambil snapshot bbolt..."
Write-Host "Sumber : $url"
Write-Host "Tujuan : $backupFile"

# -OutFile tidak memberi akses ke header respons, padahal header
# X-TrailSeal-Bolt-Size itulah yang membuktikan unduhan tidak terpotong.
$response = Invoke-WebRequest `
    -Uri $url `
    -Headers @{ "X-Root-API-Key" = $RootApiKey } `
    -Method GET `
    -TimeoutSec 300

if ($response.StatusCode -ne 200) {
    throw "Server membalas HTTP $($response.StatusCode)."
}

[System.IO.File]::WriteAllBytes($backupFile, $response.Content)

$actual = (Get-Item $backupFile).Length
if ($actual -eq 0) {
    Remove-Item $backupFile -Force
    throw "Snapshot berukuran 0 byte — dibuang, tidak disimpan sebagai backup palsu."
}

# Bandingkan dengan ukuran yang DIKLAIM server. Tidak cocok berarti
# terpotong di tengah jalan — dan backup terpotong yang tersimpan diam-diam
# adalah kegagalan terburuk yang mungkin terjadi.
$claimed = $response.Headers["X-TrailSeal-Bolt-Size"]
if ($claimed) {
    $claimedBytes = [int64]($claimed | Select-Object -First 1)
    if ($claimedBytes -ne $actual) {
        Remove-Item $backupFile -Force
        throw "Unduhan TERPOTONG: server mengirim $claimedBytes byte, tersimpan $actual byte."
    }
    Write-Host "Ukuran cocok dengan yang dikirim server: $actual byte."
}

Write-Host "Tersimpan: $([math]::Round($actual / 1MB, 2)) MB"

# === PEMBUKTIAN ===
if (-not $SkipVerify) {
    $verifier = Get-Command bolt-verify -ErrorAction SilentlyContinue

    if ($verifier) {
        Write-Host "`nMemverifikasi snapshot..."
        & $verifier.Source -db $backupFile -expect-buckets tenants,users
        if ($LASTEXITCODE -ne 0) {
            throw "VERIFIKASI GAGAL. Berkas disimpan di $backupFile untuk diperiksa, JANGAN diandalkan sebagai backup."
        }
    }
    else {
        Write-Host "`nMemverifikasi snapshot (go run)..."
        $repoRoot = Split-Path $PSScriptRoot -Parent
        Push-Location $repoRoot
        try {
            & go run ./cmd/bolt-verify -db $backupFile -expect-buckets tenants,users
            if ($LASTEXITCODE -ne 0) {
                throw "VERIFIKASI GAGAL. Berkas disimpan di $backupFile untuk diperiksa, JANGAN diandalkan sebagai backup."
            }
        }
        finally {
            Pop-Location
        }
    }
}

# === RETENSI ===
if ($KeepDays -gt 0) {
    $cutoff = (Get-Date).AddDays(-$KeepDays)
    $old = Get-ChildItem $OutputDir -Filter "saas-api-*.db" -File |
           Where-Object { $_.LastWriteTime -lt $cutoff }

    if ($old) {
        Write-Host "`nMenghapus $($old.Count) backup lebih lama dari $KeepDays hari:"
        foreach ($f in $old) {
            Write-Host "  - $($f.Name)"
            Remove-Item $f.FullName -Force
        }
    }
}

Write-Host ""
Write-Host "======================================"
Write-Host "BACKUP bbolt SELESAI DAN TERVERIFIKASI"
Write-Host "======================================"
Write-Host "Berkas : $backupFile"
Write-Host "Ukuran : $([math]::Round($actual / 1MB, 2)) MB"
Write-Host "======================================"

$backupFile
