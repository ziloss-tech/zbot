# ZBOT — Agent Instructions

> Read this FIRST before touching any code. This is the single source of truth
> for how to work in this repo.

## What Is ZBOT

A self-hosted AI agent written in Go. It connects to Slack, runs a web UI,
executes tools, manages workflows, does deep research, and stores encrypted
secrets — all from a single binary. Users bring their own API keys. ZBOT
provides the orchestration.

Tagline: "Pro athlete performance for pickup game prices."

## Stack

- **Language:** Go 1.22+ (ADR-001). Single binary. No runtime dependencies.
- **Architecture:** Hexagonal / Ports and Adapters (ADR-002). Business logic
  in `internal/agent/`. All external systems are adapters behind interfaces
  defined in `internal/agent/ports.go`.
- **LLM:** Claude Sonnet 4.6 default. Single-brain architecture (ADR-004).
  Supports any OpenAI-compatible backend via `ZBOT_LLM_BASE_URL`.
- **Database:** Postgres + pgvector on GCP Cloud SQL. SQLite for local chat
  history. In-memory fallback when no Postgres.
- **Frontend:** React 18 + Vite + Tailwind + Framer Motion. Built assets
  embedded in the Go binary. Dark theme (#0a0a1a).
- **Secrets:** GCP Secret Manager (production) or env vars (self-hosted).
  User secrets in AES-256-GCM encrypted vault (internal/vault/).

## Directory Structure

```
cmd/zbot/           → Entry point. wire.go is the dependency injection root.
internal/
  agent/            → Core domain. ports.go defines ALL interfaces. DO NOT
                      add external imports here.
  llm/              → LLM adapters (Anthropic, OpenAI-compat, DeepSeek, Haiku)
  memory/           → pgvector store, embeddings, daily notes, flusher, curator
  skills/           → Modular skill system. Each skill = directory with
                      client.go + tools.go + skill.go
    ghl/            → GoHighLevel CRM (20 tools)
    github/         → GitHub API (13 tools)
    email/          → SMTP send
    sheets/         → Google Sheets read/write
    memory/         → save_memory + search_memory
    search/         → Serper + Brave web search
    mcpbridge/      → Dynamic MCP server loader (Zapier replacement)
  vault/            → Encrypted secrets vault (AES-256-GCM, HKDF per-user keys)
  tools/            → Core tools (fetch, file, code runner, vision, PDF)
  research/         → Deep research pipeline (multi-model)
  workflow/         → Task graph orchestrator
  scheduler/        → Cron job scheduler + runner
  gateway/          → Slack (Socket Mode) + webhook gateway
  webui/            → Web UI server + SSE streaming + React frontend
  security/         → Prompt injection detection, confirmation gates
  secrets/          → GCP Secret Manager + env var adapters
  audit/            → Postgres audit logger
  prompts/          → Modular prompt system (dormant, one-line activation)
docs/
  adr/              → Architecture Decision Records (ADR-001 through ADR-007)
  sprints/          → Sprint specs and roadmap
```

## How To Add A New Skill

1. Create `internal/skills/yourskill/`
2. Add `client.go` (API client), `tools.go` (tool implementations), `skill.go`
3. Each tool implements the `agent.Tool` interface from `ports.go`:
   - `Name() string`
   - `Definition() ToolDefinition` (JSON Schema for inputs)
   - `Execute(ctx, input) (*ToolResult, error)`
4. `skill.go` implements `skills.Skill` interface:
   - `Name()`, `Description()`, `Tools()`, `SystemPromptAddendum()`
5. Register in `cmd/zbot/wire.go` under the Skills System section
6. Run `go build ./...` to verify. Run `go test ./internal/skills/yourskill/`

## How To Add An MCP Server (Zero Code)

Add to `~/zbot-workspace/mcp-servers.json`:
```json
[
  {
    "name": "stripe",
    "command": "npx",
    "args": ["-y", "@stripe/agent-toolkit"],
    "env": {"STRIPE_SECRET_KEY": "vault:STRIPE_SECRET_KEY"}
  }
]
```
ZBOT auto-discovers tools via MCP protocol at startup.

## Rules

### Architecture Rules
- `internal/agent/` NEVER imports adapters. Only interfaces.
- All secrets go through `agent.SecretsManager` interface. Never hardcode.
- Every tool must respect `ctx` cancellation.
- wire.go is the ONLY file that knows about concrete types. If you're importing
  an adapter package anywhere else, you're doing it wrong.

### Code Quality (SOLID + Pragmatism)
- **S** — Each tool does one thing. Each skill wraps one API.
- **O** — New skills don't modify existing skills or the agent core.
- **L** — Any `agent.Tool` implementation is substitutable.
- **I** — Don't force tools to implement methods they don't use.
- **D** — Depend on ports.go interfaces, not concrete types.
- **DRY** — If two skills share logic, extract to a shared package.
- **KISS** — If a junior dev can't read it in 5 minutes, simplify.
- **YAGNI** — Don't build features nobody asked for yet.

### Testing
- `go test ./...` must pass before any commit.
- `go vet ./...` must be clean.
- New tools need at least one test. Use in-memory mocks, not real APIs.
- Vault tests use `memStore` (see vault_test.go for pattern).

### Git
- Active branch: `public-release` (local working branch)
- Remotes: `public` → ziloss-tech/zbot, `origin` → jeremylerwick-max/zbot
- Push to both: `git push public public-release:main && git push origin public-release:main`
- `main` branch is kept in sync with `public-release`
- Commit messages: `feat:`, `fix:`, `docs:`, `chore:` prefixes
- Never commit secrets, API keys, or personal data. Run `git diff --cached`
  before committing if unsure.

### What NOT To Touch Without Asking
- `cmd/zbot/wire.go` — Complex dependency injection. Breaking this breaks
  everything. Understand the full file before editing.
- `internal/agent/ports.go` — Changing interfaces affects every adapter.
- `.env` — Contains real secrets on Jeremy's machine.

## Current State (March 2026)

- 63 built-in tools across 10 skills + unlimited via MCP bridge
- Cognitive engine: 5-stage brain loop (plan → memory → execute → memory → verify)
- Thalamus catches hallucinations before user sees them (~$0.002/turn)
- Deep research pipeline: 5 models collaborating
- Encrypted vault: AES-256-GCM, per-user HKDF key derivation, Postgres store
- Web UI: Dark theme, markdown rendering, SSE streaming, event bus
- Deployed: Slack Socket Mode + web UI at localhost:18790

## Credits

ZBOT builds on open-source infrastructure. Thanks to:
- Anthropic for the Claude API and MCP specification
- The MCP server community (registry at github.com/mcp)
- Go standard library crypto packages (AES-GCM, HKDF)
- React, Vite, Tailwind, Framer Motion teams
- pgvector for Postgres vector search
