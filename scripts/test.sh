#!/bin/bash
# scripts/test.sh

set -e

echo "🧪 Running tests..."

go test ./... -v -cover -coverprofile=coverage.out

echo "📊 Coverage report:"
go tool cover -func=coverage.out