.PHONY: build run test lint vuln clean tidy ui ui-build dev

# ── Build ──────────────────────────────────────────────────────────────────────
build:
	go build -o bin/zbot ./cmd/zbot

# ── Run (development) ──────────────────────────────────────────────────────────
run:
	go run ./cmd/zbot --env=development

# ── Sprint 11: Dual Brain Command Center UI ──────────────────────────────────
ui:
	cd internal/webui/frontend && npm run dev

ui-build:
	bash scripts/build-ui.sh

dev:
	ZBOT_ENV=development GCP_PROJECT=ziloss go run ./cmd/zbot/

# ── Test ───────────────────────────────────────────────────────────────────────
test:
	go test ./... -race -timeout 60s -v

test-short:
	go test ./... -short -timeout 30s

# ── Lint ───────────────────────────────────────────────────────────────────────
lint:
	golangci-lint run ./...

# ── Vulnerability scan (run before every commit) ───────────────────────────────
vuln:
	govulncheck ./...

# ── Tidy dependencies ─────────────────────────────────────────────────────────
tidy:
	go mod tidy
	go mod verify

# ── Clean ──────────────────────────────────────────────────────────────────────
clean:
	rm -rf bin/ workspace/

# ── Database migrations ───────────────────────────────────────────────────────
migrate-up:
	@echo "Run: migrate -path=migrations -database=$$DATABASE_URL up"

# ── GCP Secret Manager setup (run once to provision secrets) ──────────────────
secrets-init:
	@echo "Provisioning ZBOT secrets in GCP Secret Manager..."
	@read -p "Anthropic API Key: " key; \
	  echo -n "$$key" | gcloud secrets create zbot-anthropic-api-key --data-file=- --project=$(GCP_PROJECT) 2>/dev/null || \
	  echo -n "$$key" | gcloud secrets versions add zbot-anthropic-api-key --data-file=- --project=$(GCP_PROJECT)
	@read -p "Telegram Bot Token: " tok; \
	  echo -n "$$tok" | gcloud secrets create zbot-telegram-token --data-file=- --project=$(GCP_PROJECT) 2>/dev/null || \
	  echo -n "$$tok" | gcloud secrets versions add zbot-telegram-token --data-file=- --project=$(GCP_PROJECT)
	@echo "Secrets provisioned. Never store these anywhere else."

# ── Docker (for running code sandbox locally) ─────────────────────────────────
docker-pull-runtimes:
	docker pull python:3.12-slim
	docker pull golang:1.22-alpine
	docker pull node:20-alpine
	docker pull alpine:3.19
	@echo "Runtime images pulled. Code execution sandbox ready."
