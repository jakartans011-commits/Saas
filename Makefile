# Makefile untuk Windows (PowerShell)
.PHONY: help build build-all test clean lint run deps

help:
	@echo "Available commands:"
	@echo "  make build      - Build agent for Windows"
	@echo "  make build-all  - Build for all platforms"
	@echo "  make test       - Run tests"
	@echo "  make clean      - Clean build artifacts"
	@echo "  make run        - Run agent locally"
	@echo "  make deps       - Download dependencies"

# Build for Windows
build:
	@echo "Building ShadowHarvest Agent..."
	@if not exist bin mkdir bin
	@go build -o bin/shadowharvest.exe cmd/main.go
	@echo "✅ Build complete: bin/shadowharvest.exe"

# Build for ALL platforms
build-all:
	@echo "Building for all platforms..."
	@if not exist bin mkdir bin
	@set GOOS=windows&& set GOARCH=amd64&& go build -o bin/shadowharvest-windows-amd64.exe cmd/main.go
	@set GOOS=linux&& set GOARCH=amd64&& go build -o bin/shadowharvest-linux-amd64 cmd/main.go
	@set GOOS=darwin&& set GOARCH=amd64&& go build -o bin/shadowharvest-darwin-amd64 cmd/main.go
	@echo "✅ Build complete!"

# Run tests
test:
	@echo "Running tests..."
	@go test ./... -v -cover

# Clean
clean:
	@echo "Cleaning..."
	@if exist bin rmdir /s /q bin
	@if exist coverage.out del coverage.out
	@if exist logs\*.log del logs\*.log
	@echo "✅ Clean complete"

# Run agent
run:
	@echo "Running agent..."
	@go run cmd/main.go

# Download dependencies
deps:
	@echo "Downloading dependencies..."
	@go mod download
	@go mod tidy
	@echo "✅ Dependencies ready"

# Makefile - Tambahkan

# Deploy with Docker
deploy:
	@echo "🐳 Building Docker image..."
	@docker-compose build
	@echo "🚀 Starting containers..."
	@docker-compose up -d
	@echo "✅ Dashboard deployed at http://localhost:8080"

# Stop deployment
deploy-stop:
	@docker-compose down

# Deploy to cloud (VPS)
deploy-cloud:
	@echo "🚀 Deploying to cloud server..."
	@scp -r . user@your-server.com:/app/
	@ssh user@your-server.com "cd /app && docker-compose up -d"
	@echo "✅ Deployed successfully!"