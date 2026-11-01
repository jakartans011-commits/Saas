# scripts/build.ps1 - Multi-platform build script for Windows

Write-Host "🔨 ShadowHarvest Agent - Multi-Platform Build" -ForegroundColor Cyan
Write-Host "==============================================" -ForegroundColor Cyan

# Create bin directory
New-Item -ItemType Directory -Force -Path bin | Out-Null

# Build function
function Build-Platform {
    param(
        [string]$os,
        [string]$arch,
        [string]$output
    )
    
    Write-Host "📦 Building for $os/$arch..." -ForegroundColor Yellow
    
    $env:GOOS = $os
    $env:GOARCH = $arch
    
    if (go build -o "bin\$output" cmd/agent/main.go) {
        Write-Host "✅ $os/$arch complete: bin\$output" -ForegroundColor Green
    } else {
        Write-Host "❌ $os/$arch failed" -ForegroundColor Red
        exit 1
    }
}

Write-Host ""

# Windows
Build-Platform "windows" "amd64" "agent-windows-amd64.exe"

# Linux
Build-Platform "linux" "amd64" "agent-linux-amd64"
Build-Platform "linux" "arm64" "agent-linux-arm64"

# macOS
Build-Platform "darwin" "amd64" "agent-darwin-amd64"
Build-Platform "darwin" "arm64" "agent-darwin-arm64"

# Summary
Write-Host ""
Write-Host "==============================================" -ForegroundColor Green
Write-Host "✅ BUILD COMPLETE!" -ForegroundColor Green
Write-Host ""
Write-Host "📁 Files in bin/:"
Get-ChildItem -Path bin -File | Format-Table Name, Length

Write-Host ""
Write-Host "🚀 Usage:"
Write-Host "  Windows: bin\agent-windows-amd64.exe"
Write-Host "  Linux:   ./bin/agent-linux-amd64"
Write-Host "  macOS:   ./bin/agent-darwin-amd64"