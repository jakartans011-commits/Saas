# ============================================================================
# ShadowHarvest Agent - Folder Setup Script
# ============================================================================

Write-Host "========================================" -ForegroundColor Cyan
Write-Host "  ShadowHarvest Agent - Folder Setup" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""

# ============================================================================
# Tentukan Root Directory
# ============================================================================

# Jika dijalankan dari direktori proyek
$RootDir = Get-Location

# Atau tentukan path spesifik (uncomment dan sesuaikan)
# $RootDir = "D:\project\ShadowHarvest-Agent"

Write-Host "Root Directory: $RootDir" -ForegroundColor Yellow
Write-Host ""

# ============================================================================
# Daftar Folder yang Akan Dibuat
# ============================================================================

$Folders = @(
    # Entry point
    "cmd",
    "cmd\agent",
    
    # Core agent (TIDAK BERUBAH)
    "core",
    
    # Modules (kosong dulu)
    "modules",
    "modules\crypto",
    "modules\c2",
    "modules\evasion",
    "modules\runtime",
    "modules\decision",
    "modules\collector",
    "modules\keylogger",
    "modules\placeholder",
    
    # Shared utilities
    "pkg",
    "pkg\utils",
    
    # Configuration
    "configs",
    
    # Internal
    "internal",
    "internal\utils",
    
    # Tests
    "tests",
    "tests\unit",
    "tests\integration",
    
    # Docs
    "docs"
)

# ============================================================================
# Buat Folder
# ============================================================================

Write-Host "Creating folders..." -ForegroundColor Green
Write-Host ""

$CreatedCount = 0
$ExistsCount = 0

foreach ($Folder in $Folders) {
    $FullPath = Join-Path -Path $RootDir -ChildPath $Folder
    
    if (Test-Path -Path $FullPath) {
        Write-Host "  [EXISTS] $Folder" -ForegroundColor Gray
        $ExistsCount++
    } else {
        New-Item -Path $FullPath -ItemType Directory -Force | Out-Null
        Write-Host "  [CREATED] $Folder" -ForegroundColor Green
        $CreatedCount++
    }
}

Write-Host ""
Write-Host "Summary:" -ForegroundColor Yellow
Write-Host "  Created: $CreatedCount folders" -ForegroundColor Green
Write-Host "  Already exists: $ExistsCount folders" -ForegroundColor Gray
Write-Host "  Total: $($Folders.Count) folders" -ForegroundColor Cyan

# ============================================================================
# Buat File Placeholder
# ============================================================================

Write-Host ""
Write-Host "Creating placeholder files..." -ForegroundColor Green
Write-Host ""

$PlaceholderFiles = @(
    @{Path = "cmd\agent\main.go"; Content = "package main`n`nfunc main() {}"},
    @{Path = "core\agent.go"; Content = "package core`n`n// Agent core"},
    @{Path = "core\module.go"; Content = "package core`n`n// Module interface"},
    @{Path = "core\event.go"; Content = "package core`n`n// Event bus"},
    @{Path = "core\config.go"; Content = "package core`n`n// Configuration"},
    @{Path = "core\health.go"; Content = "package core`n`n// Health check"},
    @{Path = "modules\placeholder\module.go"; Content = "package placeholder`n`n// Placeholder module"},
    @{Path = "modules\crypto\module.go"; Content = "package crypto`n`n// Crypto module"},
    @{Path = "modules\c2\module.go"; Content = "package c2`n`n// C2 module"},
    @{Path = "modules\evasion\module.go"; Content = "package evasion`n`n// Evasion module"},
    @{Path = "modules\runtime\module.go"; Content = "package runtime`n`n// Runtime module"},
    @{Path = "modules\decision\module.go"; Content = "package decision`n`n// Decision module"},
    @{Path = "pkg\utils\utils.go"; Content = "package utils`n`n// Shared utilities"},
    @{Path = "configs\agent.yaml"; Content = "# Agent Configuration`n`nagent_id: agent-001"},
    @{Path = "README.md"; Content = "# ShadowHarvest Agent`n`nModular agent framework"},
    @{Path = ".gitignore"; Content = "# Go`n*.exe`n*.test`n*.out`n*.log`n/vendor/`n/dist/`n/bin/`n`n# IDE`n.idea/`n.vscode/`n*.swp`n*.swo`n`n# OS`n.DS_Store`nThumbs.db"}
)

$FileCreated = 0
foreach ($File in $PlaceholderFiles) {
    $FullPath = Join-Path -Path $RootDir -ChildPath $File.Path
    
    if (Test-Path -Path $FullPath) {
        Write-Host "  [EXISTS] $($File.Path)" -ForegroundColor Gray
    } else {
        # Create directory if not exists
        $Dir = Split-Path -Path $FullPath -Parent
        if (!(Test-Path -Path $Dir)) {
            New-Item -Path $Dir -ItemType Directory -Force | Out-Null
        }
        
        # Create file with content
        $File.Content | Out-File -FilePath $FullPath -Encoding UTF8
        Write-Host "  [CREATED] $($File.Path)" -ForegroundColor Green
        $FileCreated++
    }
}

Write-Host ""
Write-Host "Placeholder files created: $FileCreated" -ForegroundColor Green

# ============================================================================
# Buat go.mod
# ============================================================================

Write-Host ""
Write-Host "Creating go.mod..." -ForegroundColor Green

$GoModPath = Join-Path -Path $RootDir -ChildPath "go.mod"
$GoModContent = @'
module shadowharvest

go 1.21

require (
    github.com/prometheus/client_golang v1.24.0
    golang.org/x/sys v0.47.0
)
'@

if (Test-Path -Path $GoModPath) {
    Write-Host "  [EXISTS] go.mod" -ForegroundColor Gray
} else {
    $GoModContent | Out-File -FilePath $GoModPath -Encoding UTF8
    Write-Host "  [CREATED] go.mod" -ForegroundColor Green
}

# ============================================================================
# Tampilkan Struktur Folder
# ============================================================================

Write-Host ""
Write-Host "========================================" -ForegroundColor Cyan
Write-Host "  Folder Structure Created" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""

Write-Host "shadowharvest/" -ForegroundColor Yellow
Write-Host "├── cmd/" -ForegroundColor White
Write-Host "│   └── agent/" -ForegroundColor White
Write-Host "│       └── main.go (placeholder)" -ForegroundColor Gray
Write-Host "├── core/ (INTI - TIDAK BERUBAH)" -ForegroundColor Green
Write-Host "│   ├── agent.go (placeholder)" -ForegroundColor Gray
Write-Host "│   ├── module.go (placeholder)" -ForegroundColor Gray
Write-Host "│   ├── event.go (placeholder)" -ForegroundColor Gray
Write-Host "│   ├── config.go (placeholder)" -ForegroundColor Gray
Write-Host "│   └── health.go (placeholder)" -ForegroundColor Gray
Write-Host "├── modules/ (TEMPAT SEMUA MODUL)" -ForegroundColor Magenta
Write-Host "│   ├── placeholder/" -ForegroundColor White
Write-Host "│   │   └── module.go (placeholder)" -ForegroundColor Gray
Write-Host "│   ├── crypto/" -ForegroundColor White
Write-Host "│   │   └── module.go (placeholder)" -ForegroundColor Gray
Write-Host "│   ├── c2/" -ForegroundColor White
Write-Host "│   │   └── module.go (placeholder)" -ForegroundColor Gray
Write-Host "│   ├── evasion/" -ForegroundColor White
Write-Host "│   │   └── module.go (placeholder)" -ForegroundColor Gray
Write-Host "│   ├── runtime/" -ForegroundColor White
Write-Host "│   │   └── module.go (placeholder)" -ForegroundColor Gray
Write-Host "│   └── decision/" -ForegroundColor White
Write-Host "│       └── module.go (placeholder)" -ForegroundColor Gray
Write-Host "├── pkg/" -ForegroundColor White
Write-Host "│   └── utils/" -ForegroundColor White
Write-Host "│       └── utils.go (placeholder)" -ForegroundColor Gray
Write-Host "├── configs/" -ForegroundColor White
Write-Host "│   └── agent.yaml (placeholder)" -ForegroundColor Gray
Write-Host "├── internal/" -ForegroundColor White
Write-Host "│   └── utils/" -ForegroundColor White
Write-Host "├── tests/" -ForegroundColor White
Write-Host "│   ├── unit/" -ForegroundColor White
Write-Host "│   └── integration/" -ForegroundColor White
Write-Host "├── docs/" -ForegroundColor White
Write-Host "├── go.mod (created)" -ForegroundColor Green
Write-Host "├── README.md (placeholder)" -ForegroundColor Gray
Write-Host "└── .gitignore (placeholder)" -ForegroundColor Gray

# ============================================================================
# Informasi Crypto.go
# ============================================================================

Write-Host ""
Write-Host "========================================" -ForegroundColor Cyan
Write-Host "  INFORMASI CRYPTO.GO" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""

Write-Host "File crypto.go Anda: pkg/utils/crypto.go" -ForegroundColor Yellow
Write-Host ""
Write-Host "STATUS: JANGAN DIHAPUS, PINDAHKAN!" -ForegroundColor Red
Write-Host ""
Write-Host "Langkah yang disarankan:" -ForegroundColor Green
Write-Host "  1. Copy file crypto.go ke: pkg/utils/crypto.go" -ForegroundColor White
Write-Host "  2. Pastikan package name: package utils" -ForegroundColor White
Write-Host "  3. Biarkan semua fungsi export (huruf besar)" -ForegroundColor White
Write-Host "  4. Tidak perlu mengubah apapun" -ForegroundColor White
Write-Host "  5. Nanti akan dijadikan module tanpa perubahan" -ForegroundColor White

# ============================================================================
# Informasi Langkah Selanjutnya
# ============================================================================

Write-Host ""
Write-Host "========================================" -ForegroundColor Cyan
Write-Host "  LANGKAH SELANJUTNYA" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""

Write-Host "1. Pindahkan crypto.go ke pkg/utils/crypto.go" -ForegroundColor Yellow
Write-Host "2. Jalankan: go mod tidy" -ForegroundColor Yellow
Write-Host "3. Copy-paste kode dari rekomendasi sebelumnya" -ForegroundColor Yellow
Write-Host "4. Jalankan: go run cmd/agent/main.go" -ForegroundColor Yellow
Write-Host ""

Write-Host "========================================" -ForegroundColor Cyan
Write-Host "  SETUP SELESAI!" -ForegroundColor Green
Write-Host "========================================" -ForegroundColor Cyan

# ============================================================================
# Buka Folder di Explorer (Optional)
# ============================================================================

$OpenExplorer = Read-Host "`nBuka folder di Explorer? (y/n)"
if ($OpenExplorer -eq 'y' -or $OpenExplorer -eq 'Y') {
    explorer.exe $RootDir
}

Write-Host ""
Write-Host "Tekan Enter untuk keluar..."
Read-Host