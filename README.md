# ZBOT — Self-Hosted AI Agent with Memory

Your AI agent, your hardware, your data. Run any model — Llama, Mistral, Qwen, DeepSeek, Claude, GPT — through a single interface with persistent memory, tool use, and a web UI.

![Go](https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go) ![License](https://img.shields.io/badge/license-MIT-green) ![Docker](https://img.shields.io/badge/docker-ready-blue?logo=docker)

## Why ZBOT?

Most AI tools lock you into one provider, send your data to the cloud, and charge per token. ZBOT is different:

- **Run any model.** Ollama locally, Together/Groq for cheap hosted, Claude/GPT for frontier — same interface.
- **Your data stays yours.** Self-host on your own hardware. Nothing phones home.
- **Persistent memory.** ZBOT remembers across conversations using pgvector semantic search.
- **Real tool use.** Web search, file I/O, code execution, image analysis — not just chat.
- **Web UI included.** React command center with chat, memory browser, audit log.

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

### Option 3: Together AI ($0.20/M tokens)

```bash
cp .env.example .env
# Edit .env:
#   ZBOT_LLM_BASE_URL=https://api.together.xyz/v1
#   ZBOT_LLM_MODEL=meta-llama/Llama-3.3-70B-Instruct-Turbo
#   ZBOT_LLM_API_KEY=your-together-key
go run ./cmd/zbot
```

## Supported Providers

Any OpenAI-compatible API works. Here are tested configurations:

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
- **fetch_url** — scrape and read any URL with caching + rate limiting
- **read_file / write_file** — workspace file management
- **run_code** — execute Python, JavaScript, Go, or bash in a sandbox
- **save_memory / search_memory** — persistent semantic memory
- **analyze_image** — vision/multimodal analysis

### Memory (pgvector)
ZBOT saves important facts across conversations using hybrid BM25 + vector search with time decay. Plug in any Postgres instance with pgvector, or run without it (in-memory fallback).

### Web UI
React-based command center at `:18790`:
- Chat interface with streaming responses
- Memory browser (view, search, delete memories)
- Conversation history with token/cost tracking
- Audit log of every tool and model call
- Workflow viewer for multi-step tasks

### Slack Bot (optional)
Set `ZBOT_SLACK_TOKEN` + `ZBOT_SLACK_APP_TOKEN` to run ZBOT as a Slack bot.

## Configuration

All config is via environment variables. See [`.env.example`](.env.example) for the full list.

### Minimal (local, no database):
```
ZBOT_LLM_BASE_URL=http://localhost:11434/v1
ZBOT_LLM_MODEL=llama3.1:8b
ZBOT_LLM_API_KEY=ollama
```

### Full (with persistent memory):
```
ZBOT_LLM_BASE_URL=http://localhost:11434/v1
ZBOT_LLM_MODEL=llama3.1:8b
ZBOT_LLM_API_KEY=ollama
ZBOT_DATABASE_URL=postgresql://zbot:secret@localhost:5432/zbot?sslmode=disable
ZBOT_BRAVE_API_KEY=your-brave-key
```

## Architecture

```
┌─────────────────────────────────────────────┐
│                  Web UI (:18790)             │
│         React Command Center                │
└─────────────────┬───────────────────────────┘
                  │
┌─────────────────▼───────────────────────────┐
│              Agent Core                      │
│   System Prompt → LLM → Tool Loop → Reply   │
│                                              │
│   LLM: any OpenAI-compatible endpoint        │
│   Tools: search, fetch, files, code, vision  │
│   Memory: pgvector semantic search           │
└─────────────────┬───────────────────────────┘
                  │
┌─────────────────▼───────────────────────────┐
│           Your Model Provider                │
│   Ollama │ Together │ Groq │ OpenRouter │ …  │
└─────────────────────────────────────────────┘
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

## License

MIT — see [LICENSE](LICENSE).

## Contributing

Issues and PRs welcome. The codebase follows Go standard project layout with clean architecture (ports and adapters). Key packages:

- `internal/agent/` — core agent loop + interfaces
- `internal/llm/` — LLM clients (Anthropic, OpenAI-compatible)
- `internal/tools/` — tool implementations
- `internal/memory/` — pgvector semantic memory
- `internal/webui/` — React frontend + Go HTTP server
