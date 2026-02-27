#!/usr/bin/env bash
# ZBOT Observability Stack — One-command startup
# Usage: ./start.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

echo "🔭 Starting ZBOT observability stack..."

# Check Docker is running
if ! docker info &>/dev/null; then
  echo "❌ Docker is not running. Start Docker Desktop first."
  exit 1
fi

docker compose up -d

echo ""
echo "✅ Stack is up:"
echo "   Grafana   → http://localhost:3000  (admin / zbot-grafana)"
echo "   Loki      → http://localhost:3100"
echo "   Prometheus→ http://localhost:9090"
echo ""
echo "📋 To ship ZBOT logs to Loki, start ZBOT with:"
echo "   cd ~/Desktop/zbot && ./zbot 2>&1 | tee /tmp/zbot.log"
echo "   Then uncomment the file scrape block in promtail.yml"
echo ""
echo "📊 Dashboards auto-load from grafana/dashboards/ — no manual import needed."
