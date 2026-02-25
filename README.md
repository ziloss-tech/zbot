# ZBOT — Personal AI Agent Platform

> Internal Ziloss Technologies tool. Not a product. Not for distribution.

ZBOT is a privately-owned, self-hosted personal AI agent built ground-up in Go.
It gives Claude (or GPT-4 as fallback) hands — the ability to research the web,
create files and databases, execute code, analyze images, and run long multi-step
workflows autonomously — accessible via Telegram with a hardened local web UI.

## Architecture Principles

- **Hexagonal Architecture** — core domain logic is adapter-agnostic
- **Database as state** — workflows survive restarts; conversations are ephemeral
- **Context isolation** — each workflow step gets a fresh, scoped context window
- **Security-first** — Go memory safety, GCP Secret Manager, Docker sandboxing
- **Minimal deps** — stdlib first, third-party only when strictly necessary

## Project Structure

```
zbot/
├── cmd/zbot/          # Entrypoint — wires everything together (~50 lines)
├── internal/
│   ├── agent/         # Core agent loop + all port interfaces
│   ├── workflow/      # Orchestrator, task graph, worker pool
│   ├── memory/        # pgvector + BM25 hybrid memory subsystem
│   ├── tools/         # Web search, file ops, code runner, vision
│   ├── skills/        # Skill registry + definitions
│   ├── gateway/       # Telegram + Web UI adapters
│   ├── scraper/       # Anti-block web scraping subsystem
│   ├── secrets/       # GCP Secret Manager adapter
│   └── platform/      # Shared types, logging, config
├── migrations/        # SQL migration files (golang-migrate)
├── docs/
│   ├── adr/           # Architecture Decision Records
│   └── sprints/       # Sprint plans + changelogs
└── .github/workflows/ # CI — govulncheck, test, build
```

## Quick Start

```bash
# Prerequisites: Go 1.22+, Docker, GCP credentials
cp config.example.yaml config.yaml
# Edit config.yaml with your GCP project ID
go run ./cmd/zbot
```

## Sprint Progress

See [docs/sprints/](docs/sprints/) for the full 10-sprint roadmap.
Current sprint: **Sprint 0 — Foundation**

## Security

All API keys and credentials are stored in GCP Secret Manager.
Zero plaintext secrets anywhere in this repository.
See [docs/adr/ADR-004-secrets.md](docs/adr/ADR-004-secrets.md).
