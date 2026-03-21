# Show HN: ZBOT – Self-hosted AI agent with 56 tools, encrypted vault, $0.02/query

I built an open-source AI agent that replaces Zapier + secrets manager + AI chatbot with a single Go binary. You bring your own API keys; ZBOT provides the orchestration.

**Why I built it:** I run a small lead-gen business using GoHighLevel, GitHub, Google Sheets, and 5 other SaaS tools. I was paying ~$1,200/mo for Zapier ($150), Infisical ($200), Perplexity ($200), and various AI chatbot platforms ($500+). The underlying APIs are simple — what I was paying for was glue code and a nice UI.

**What it does:**

56 built-in tools across 8 skills (CRM, GitHub, encrypted vault, web search, Google Sheets, email, memory, code execution) plus unlimited tool expansion via MCP bridge (plug in any MCP server, zero code).

Every message runs through a 5-stage cognitive loop: plan → load memory → execute with tools → enrich memory → verify. The verification stage (Thalamus) uses a $0.001 Haiku call to catch hallucinations before you see them. In testing, it caught fabricated statistics, invented API features, and fake benchmark numbers — the user only ever sees the corrected version.

Total cognitive overhead: ~$0.002/turn on top of the main Sonnet call. With DeepSeek V3.2 for planning/verification ($0.14/M) and Serper for search ($0.30/1K), a typical interactive turn costs ~$0.017.

**Self-hosted means:**

- Your conversations, memories, and secrets never leave your hardware
- Works with any OpenAI-compatible model (Ollama, Together, Groq, vLLM)
- Encrypted secrets vault (AES-256-GCM, per-user HKDF key derivation) — no third-party secrets manager needed
- Persistent memory via pgvector semantic search
- Single binary, no Python, runs on a $7/mo Postgres instance

**Quick start:**

    git clone https://github.com/ziloss-tech/zbot && cd zbot
    cp .env.example .env  # add your API key
    docker compose up -d
    # Open http://localhost:18790

Or build from source: `go build ./cmd/zbot && ./zbot`

Repo: https://github.com/ziloss-tech/zbot

Apache 2.0 licensed. Written in Go. PRs welcome.

---

# First comment (post immediately after)

Architecture details for the curious:

The cognitive loop is ~500 lines across two files. Each stage is a separate LLM call — the expensive model (Sonnet) only runs for the actual reasoning (Cortex). Planning and verification use the cheapest available model (DeepSeek V3.2 at $0.14/M, or Haiku as fallback). Memory retrieval is pure database, zero LLM cost.

The event bus is the nervous system. Every stage emits structured events (~50 tokens) that the web UI consumes via SSE. You can watch the agent think in real-time: planning → searching → calling tools → verifying → done.

The hexagonal architecture (ports.go defines all interfaces, wire.go is the only file that knows about concrete types) means you can swap the LLM provider, memory store, and search engine without touching the core loop.

MCP bridge: drop a JSON config file in your workspace and any MCP-compatible server becomes a ZBOT skill at startup. I replaced my entire Zapier setup with this.

Stack: Go 1.22+ single binary, React 18 + Vite + Tailwind frontend (embedded in the binary), Postgres + pgvector for memory, SQLite for local chat history. Gracefully degrades to in-memory everything if no database is configured.

The vault was a fun build — AES-256-GCM encryption with HKDF per-user key derivation from a single master key. It's ~200 lines of Go standard library crypto. I considered Infisical but the multi-tenant key derivation is 20 lines of code — didn't need a dependency for that.

Happy to answer questions about the architecture, the cognitive loop, or the cost model.
