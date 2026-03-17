# ZBOT — Self-Hosted AI Agent with Memory

Your AI agent, your hardware, your data. Run any model — Llama, Mistral, Qwen, DeepSeek, Claude, GPT — through a single interface with persistent memory, tool use, deep research, and a web UI.

![Go](https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go) ![License](https://img.shields.io/badge/license-MIT-green) ![Docker](https://img.shields.io/badge/docker-ready-blue?logo=docker) ![Claude](https://img.shields.io/badge/Claude-Sonnet_4.6-blueviolet?logo=anthropic)

## Why ZBOT?

Most AI tools lock you into one provider, send your data to the cloud, and charge per token. ZBOT is different:

- **Run any model.** Ollama locally, Together/Groq for cheap hosted, Claude/GPT for frontier — same interface.
- **Your data stays yours.** Self-host on your own hardware. Nothing phones home.
- **Persistent memory.** Remembers across conversations using pgvector semantic search with diversity re-ranking.
- **Deep research.** Two-phase Haiku→Sonnet pipeline gathers 30-50 sources and synthesizes comprehensive reports for ~$0.10-0.50.
- **Credentialed scraping.** Securely fetch from authenticated sites — credentials stored in macOS Keychain or GCP Secret Manager.
- **Real tool use.** Web search, file I/O, code execution, image analysis, credential management — not just chat.
- **Web UI included.** React command center with dynamic split-pane layout, memory browser, and SSE streaming.

## What's New in v2

### Single-Brain Architecture
The v1 multi-model dance (GPT-4o Planner → Claude Executor → GPT-4o Critic) is gone. Claude Sonnet 4.6 handles planning, execution, and self-critique in a single context window — faster, cheaper, and no information lost between handoffs.

### Deep Research v2
Two-phase research pipeline optimized for cost and quality:
- **Phase 1 (Haiku):** Generates search queries, fires parallel searches, extracts structured facts from each page. Cost: ~$0.01-0.05.
- **Phase 2 (Sonnet):** Synthesizes all extracted facts into a comprehensive report with citations and extended thinking. Cost: ~$0.10-0.50.
- **Total: ~$0.10-0.50** for 30-50 sources with better synthesis than the v1 five-model pipeline.

### Memory System
Layered memory architecture that never loses important context:
- **pgvector semantic search** with hybrid BM25 + vector scoring and time decay
- **Daily notes** — markdown files (`memory/YYYY-MM-DD.md`) alongside the database. Human-readable, git-trackable.
- **Memory curator** — periodic LLM-based review of daily notes, promoting important facts to permanent memory
- **Diversity re-ranking** — cosine similarity filter prevents near-duplicate memories from cluttering retrieval
- **Context flush** — extracts critical facts before context window compaction so marathon sessions don't lose information

### Credentialed Research
Fetch content from authenticated sites without exposing credentials:
- **macOS Keychain** — zero-setup, encrypted, Touch ID protected (default for Mac users)
- **GCP Secret Manager** — for cloud deployments
- **Domain-matched injection** — supports Bearer, Basic, Cookie, Header, and API key auth types
- **Credential scrubber** — regex-based log redaction ensures passwords never appear in logs, memory, or conversation history
- **Agent tool** — "Z, add my WSJ login" → ZBOT asks for credentials → stores securely → done

## Quick Start

### Option 1: Ollama (fully local, free)

```bash
# 1. Install Ollama and pull a model
ollama pull llama3.1:8b

# 2. Clone and run ZBOT
git clone https://github.com/jeremylerwick-max/zbot.git
cd zbot
cp .env.example .env
# .env defaults to Ollama — just run it:
go run ./cmd/zbot
```

Open **http://localhost:18790** — you're chatting with a local AI agent.

### Option 2: Docker

```bash
docker run -p 18790:18790 \
  -e ZBOT_LLM_BASE_URL=http://host.docker.internal:11434/v1 \
  -e ZBOT_LLM_MODEL=llama3.1:8b \
  -e ZBOT_LLM_API_KEY=ollama \
  ghcr.io/jeremylerwick-max/zbot:latest
```

### Option 3: Claude (Anthropic)

```bash
cp .env.example .env
# Edit .env:
#   ZBOT_ANTHROPIC_API_KEY=your-anthropic-key
go run ./cmd/zbot
```

This gives you the full v2 experience: single-brain Claude, deep research v2, and model tier routing (Haiku for bulk, Sonnet for default, Opus for escalation).

## Supported Providers

Any OpenAI-compatible API works. Tested configurations:

| Provider | Base URL | Example Model | Cost |
|----------|----------|---------------|------|
| **Ollama** | `http://localhost:11434/v1` | `llama3.1:8b` | Free (local) |
| **Together** | `https://api.together.xyz/v1` | `Llama-3.3-70B-Instruct-Turbo` | ~$0.20/M |
| **Groq** | `https://api.groq.com/openai/v1` | `llama-3.1-70b-versatile` | Free tier |
| **OpenRouter** | `https://openrouter.ai/api/v1` | `google/gemini-2.5-flash` | Varies |
| **LM Studio** | `http://localhost:1234/v1` | Your loaded model | Free (local) |
| **vLLM** | `http://localhost:8000/v1` | Your served model | Free (local) |
| **Anthropic** | *(native, set `ZBOT_ANTHROPIC_API_KEY`)* | `claude-sonnet-4-6` | $3/$15/M |
| **OpenAI** | `https://api.openai.com/v1` | `gpt-4o` | $2.50/$10/M |

## Features

### Tools
ZBOT doesn't just chat — it acts. Built-in tools:

- **web_search** — Brave Search API for real-time web results
- **fetch_url** — scrape and read any URL with caching, rate limiting, and proxy rotation
- **credentialed_fetch** — fetch authenticated content with domain-matched credential injection
- **manage_credentials** — add, remove, and list stored site credentials via the agent
- **read_file / write_file** — workspace file management
- **run_code** — execute Python, JavaScript, Go, or bash in a sandbox
- **save_memory / search_memory** — persistent semantic memory with diversity re-ranking
- **analyze_image** — vision/multimodal analysis
- **pdf_extract** — extract text from PDF attachments

### Skills System
Modular skill architecture for domain-specific capabilities:
- **Memory** — save and search long-term facts
- **Search** — web search orchestration
- **GHL** — GoHighLevel CRM integration
- **GitHub** — repository and issue management
- **Google Sheets** — spreadsheet read/write
- **Email** — SMTP email sending

### Workflows & Scheduling
- **Multi-step workflows** — `plan: research top 5 competitors and write a report` decomposes into tasks and executes them with progress tracking
- **Cron scheduling** — `//schedule 0 8 * * 1 | Check open GHL leads` runs recurring tasks
- **Background execution** — deep research runs in the background while you chat in the foreground

### Web UI
React-based command center at `:18790`:
- Dynamic split-pane layout with draggable panels
- Chat interface with SSE streaming responses
- Memory browser (view, search, delete memories)
- Conversation history with token/cost tracking
- Workflow viewer for multi-step task progress
- Deep research panel with real-time iteration tracking
- Schedule manager for recurring tasks
- Audit log of every tool and model call

### Slack Bot (optional)
Set `ZBOT_SLACK_TOKEN` + `ZBOT_SLACK_APP_TOKEN` to run ZBOT as a Slack bot with full tool access.

## Architecture

```
┌──────────────────────────────────────────────────────────┐
│               Web UI (:18790) / Slack / Webhooks         │
│            React Command Center + SSE Streaming          │
└──────────────────────┬───────────────────────────────────┘
                       │
┌──────────────────────▼───────────────────────────────────┐
│                    Agent Core                             │
│      System Prompt → LLM → Tool Loop → Reply             │
│                                                          │
│      v2: Single-brain architecture (Claude Sonnet 4.6)   │
│      Model tiers: Haiku (bulk) → Sonnet → Opus (escal.)  │
├──────────────────────────────────────────────────────────┤
│  Tools        │  Memory           │  Research            │
│  ─────────    │  ─────────        │  ─────────           │
│  web_search   │  pgvector store   │  v2: Haiku→Sonnet    │
│  fetch_url    │  daily notes .md  │  Parallel search     │
│  cred_fetch   │  diversity rerank │  Structured extract  │
│  manage_cred  │  curator (LLM)    │  Synthesis + cites   │
│  run_code     │  context flush    │  Cost tracking       │
│  file I/O     │  time decay       │  Claim memory        │
│  vision       │                   │                      │
├──────────────────────────────────────────────────────────┤
│  Secrets                │  Security                      │
│  ──────────             │  ──────────                    │
│  macOS Keychain         │  Credential scrubber           │
│  GCP Secret Manager     │  Prompt injection detection    │
│  Env var fallback       │  SSRF blocklist                │
│                         │  Destructive op confirmation   │
└──────────────────────┬───────────────────────────────────┘
                       │
┌──────────────────────▼───────────────────────────────────┐
│                 Your Model Provider                       │
│    Ollama │ Together │ Groq │ OpenRouter │ Claude │ GPT   │
└──────────────────────────────────────────────────────────┘
```

## Configuration

All config is via environment variables. See [`.env.example`](.env.example) for the full list.

### Minimal (local, no database):
```
ZBOT_LLM_BASE_URL=http://localhost:11434/v1
ZBOT_LLM_MODEL=llama3.1:8b
ZBOT_LLM_API_KEY=ollama
```

### Full (with persistent memory + deep research):
```
ZBOT_ANTHROPIC_API_KEY=your-anthropic-key
ZBOT_DATABASE_URL=postgresql://zbot:secret@localhost:5432/zbot?sslmode=disable
ZBOT_BRAVE_API_KEY=your-brave-key
```

### Postgres Setup (for memory + research)
```bash
# Docker one-liner for pgvector
docker run -d --name zbot-pg \
  -e POSTGRES_USER=zbot \
  -e POSTGRES_PASSWORD=secret \
  -e POSTGRES_DB=zbot \
  -p 5432:5432 \
  pgvector/pgvector:pg16
```

## Building

```bash
# Prerequisites: Go 1.22+, Node 18+ (for frontend)

# Build frontend
cd internal/webui/frontend && npm ci && npx vite build && cd ../../..

# Build binary
go build -o zbot ./cmd/zbot

# Or Docker
docker build -t zbot .
```

## Project Structure

```
cmd/zbot/              — Entry point + dependency wiring
internal/
  agent/               — Core agent loop, interfaces (ports)
  llm/                 — LLM clients (Anthropic, OpenAI-compatible, Haiku, Opus)
  tools/               — Tool implementations (search, fetch, code, credentials)
  memory/              — pgvector store, daily notes, diversity reranker, curator, flusher
  research/            — Deep research v1 (multi-model) + v2 (Haiku→Sonnet)
  secrets/             — Keychain, GCP Secret Manager, env fallback, scrubber
  security/            — Injection detection, SSRF blocklist, confirmation gates
  scraper/             — Browser fetcher, proxy pool, rate limiter, cache
  skills/              — Skill registry + domain skills (GHL, GitHub, Sheets, Email)
  workflow/            — Multi-step task orchestrator + Postgres store
  scheduler/           — Cron scheduler + runner
  webui/               — React frontend + Go HTTP/SSE server
  gateway/             — Slack + webhook gateways
  audit/               — Postgres audit logger
  prompts/             — System prompt modules
```

## License

MIT — see [LICENSE](LICENSE).

## Contributing

Issues and PRs welcome. The codebase follows hexagonal architecture (ports and adapters) with the `agent` package defining all interfaces. Run tests with:

```bash
go test ./...
```
