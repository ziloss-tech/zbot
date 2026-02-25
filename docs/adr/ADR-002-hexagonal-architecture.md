# ADR-002: Architecture — Hexagonal (Ports and Adapters)

**Status:** Accepted  
**Date:** 2026-02-25

## Context

ZBOT needs to be testable without real databases or API keys, and swappable
(e.g. change vector DB, change LLM provider) without touching core logic.

## Decision

**Hexagonal Architecture** (Ports and Adapters). The core agent domain defines
interfaces (ports). All external systems are adapters that implement those interfaces.

```
core/agent → defines interfaces (ports.go)
  ├── LLMClient        port ← implemented by: anthropic/, openai/
  ├── MemoryStore      port ← implemented by: memory/pgvector
  ├── WorkflowStore    port ← implemented by: postgres/
  ├── Tool             port ← implemented by: tools/web.go, tools/filesystem.go, etc.
  ├── Gateway          port ← implemented by: gateway/telegram.go, gateway/webui.go
  ├── SecretsManager   port ← implemented by: secrets/gcp.go, secrets/env.go
  └── AuditLogger      port ← implemented by: audit/postgres.go
```

## Consequences

- **Testability:** Unit tests inject mock adapters. No database, no network, no API keys needed.
- **Swappability:** Want to switch from pgvector to Weaviate? Write one new adapter. Core never changes.
- **Clarity:** Reading `internal/agent/ports.go` gives a complete picture of all system dependencies.
- **Cost:** More initial boilerplate than a flat structure. Worth it at this project's lifetime.

## Directory Rule

Business logic lives in `internal/`. It imports only the port interfaces. It never
imports `gateway/`, `secrets/`, or any adapter package. `cmd/zbot/main.go` is the
only place that knows about concrete adapter types — it wires them together.
