#!/bin/bash
# Sprint 1 Setup Script
# Run this after pulling the Sprint 1 changes to resolve Go dependencies.
#
# Usage: cd ~/Desktop/zbot && bash sprint1-setup.sh

set -euo pipefail

echo "=== ZBOT Sprint 1 Setup ==="

# 1. Tidy up Go module dependencies (resolves go.sum)
echo "→ Running go mod tidy..."
go mod tidy

# 2. Verify build
echo "→ Verifying build..."
go build ./...

echo ""
echo "✅ Sprint 1 build verified. Run with:"
echo "   go run ./cmd/zbot"
