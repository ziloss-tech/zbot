#!/bin/bash
set -euo pipefail

echo "▸ Building frontend..."
cd internal/webui/frontend
NODE_ENV=development npm install --include=dev
NODE_ENV=production npx vite build
cd ../../..

echo "▸ Building Go binary..."
go build -o zbot ./cmd/zbot/

echo "✓ zbot binary with embedded UI ready"
ls -lh zbot
