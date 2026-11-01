<#
.SYNOPSIS
    Memulihkan snapshot bbolt ke berkas database TrailSeal.

.DESCRIPTION
    Restore bbolt bukan proses "impor" seperti pg_restore — ia penggantian
    berkas. Karena itu urutannya penting dan tidak boleh diringkas:

      1. HENTIKAN server. bbolt mengunci berkasnya secara eksklusif;
         mengganti berkas di bawah proses yang sedang berjalan
         menghasilkan kerusakan, bukan pemulihan.
      2. Snapshot diverifikasi LEBIH DULU, sebelum apa pun disentuh.
         Memulihkan backup yang rusak di atas database yang masih bisa
         dibaca sebagian mengubah masalah menjadi bencana.
      3. Berkas lama disimpan sebagai .pre-restore, tidak dihapus.
      4. Hasil restore diverifikasi lagi setelah di tempat.
      5. Baru jalankan server kembali.

    Skrip ini menolak berjalan tanpa -Force, mengikuti pola
    restore-postgres.ps1.

.PARAMETER BackupFile
    Snapshot hasil backup-bolt.ps1.

.PARAMETER TargetPath
    Berkas database tujuan. Default: <TRAILSEAL_DATA_DIR>\saas-api.db,
    atau data\saas-api.db bila env tidak diset — sama dengan yang dipakai
    main.go.

.PARAMETER Force
    Wajib. Restore itu destruktif.

.EXAMPLE
    # 1. hentikan server lebih dulu
    .\scripts\restore-bolt.ps1 -BackupFile .\scripts\backups\saas-api-20261101-120000.db -Force
#>
[CmdletBinding(SupportsShouldProcess)]
param(
    [Parameter(Mandatory = $true)]
    [string]$BackupFile,

    [string]$TargetPath,

    [switch]$Force
)

$ErrorActionPreference = "Stop"

if (-not (Test-Path $BackupFile -PathType Leaf)) {
    throw "Berkas backup tidak ditemukan: $BackupFile"
}

if (-not $TargetPath) {
    $dataDir = $env:TRAILSEAL_DATA_DIR
    if ([string]::IsNullOrWhiteSpace($dataDir)) { $dataDir = "data" }
    $TargetPath = Join-Path $dataDir "saas-api.db"
}

if (-not $Force) {
    throw "Restore itu destruktif. Jalankan ulang dengan -Force setelah memastikan: (1) server SUDAH DIHENTIKAN, (2) target benar: $TargetPath"
}

# === PENGAMAN: server masih jalan? ===
$running = Get-Process saas-api -ErrorAction SilentlyContinue
if ($running) {
    throw "Proses saas-api masih berjalan (PID $($running.Id -join ', ')). Hentikan dulu — bbolt mengunci berkasnya, dan mengganti berkas di bawah proses hidup merusak database."
}

$repoRoot = Split-Path $PSScriptRoot -Parent

function Invoke-BoltVerify {
    param([string]$Path, [string]$Label)

    Write-Host "`nMemverifikasi $Label..."
    $verifier = Get-Command bolt-verify -ErrorAction SilentlyContinue

    if ($verifier) {
        & $verifier.Source -db $Path -expect-buckets tenants,users
    }
    else {
        Push-Location $repoRoot
        try { & go run ./cmd/bolt-verify -db $Path -expect-buckets tenants,users }
        finally { Pop-Location }
    }

    if ($LASTEXITCODE -ne 0) {
        throw "Verifikasi $Label GAGAL."
    }
}

# === 1. VERIFIKASI SNAPSHOT SEBELUM MENYENTUH APA PUN ===
Invoke-BoltVerify -Path $BackupFile -Label "snapshot sumber"

Write-Host ""
Write-Host "Backup : $BackupFile"
Write-Host "Target : $TargetPath"

if (-not $PSCmdlet.ShouldProcess($TargetPath, "Restore $BackupFile")) {
    return
}

# === 2. SIMPAN BERKAS LAMA, JANGAN HAPUS ===
if (Test-Path $TargetPath) {
    $stamp  = Get-Date -Format "yyyyMMdd-HHmmss"
    $backup = "$TargetPath.pre-restore-$stamp"
    Write-Host "`nMenyimpan database saat ini ke:"
    Write-Host "  $backup"
    Move-Item -LiteralPath $TargetPath -Destination $backup -Force
}
else {
    $targetDir = Split-Path $TargetPath -Parent
    if ($targetDir) { New-Item -ItemType Directory -Force -Path $targetDir | Out-Null }
}

# === 3. PASANG SNAPSHOT ===
Copy-Item -LiteralPath $BackupFile -Destination $TargetPath -Force

# === 4. VERIFIKASI DI TEMPAT ===
Invoke-BoltVerify -Path $TargetPath -Label "database hasil restore"

Write-Host ""
Write-Host "======================================"
Write-Host "RESTORE bbolt SELESAI DAN TERVERIFIKASI"
Write-Host "======================================"
Write-Host "Dari   : $BackupFile"
Write-Host "Ke     : $TargetPath"
Write-Host ""
Write-Host "Langkah berikutnya: jalankan kembali server, lalu periksa"
Write-Host "GET /readyz harus 200 dengan checks.bolt = ok."
Write-Host "======================================"
