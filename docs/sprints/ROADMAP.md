# ZBOT — Sprint Roadmap (Updated March 21, 2026)
**Start Date:** Feb 25, 2026
**Current Status:** Pre-launch (Show HN pending)
**Sprint Length:** Originally 1 week — actual pace was much faster

> Note: Development moved significantly faster than planned. Most of
> Sprints 0–9 were completed in ~3 weeks. The roadmap below reflects
> actual completion status as of March 21, 2026.

---

## Sprint 0 — Foundation ✅ COMPLETE

| # | Task | Status | Notes |
|---|------|--------|-------|
| S0-1 | Private GitHub repo | ✅ | jeremylerwick-max/zbot |
| S0-2 | Go module + hexagonal structure | ✅ | |
| S0-3 | Core ports defined in ports.go | ✅ | |
| S0-4 | Core agent loop | ✅ | |
| S0-5 | Workflow orchestrator | ✅ | |
| S0-6 | Memory store (pgvector) | ✅ | |
| S0-7 | GCP Secret Manager adapter | ✅ | |
| S0-8 | All ADRs documented | ✅ | ADR-001 through ADR-007 |
| S0-9 | `go build` passes | ✅ | |
| S0-10 | wire.go DI wired | ✅ | ~1,260 lines |
| S0-11 | CI pipeline | ✅ | GitHub Actions: govulncheck + staticcheck + test + build |
| S0-12 | Gateway connects | ✅ | Changed from Telegram to Slack Socket Mode |
| S0-13 | config + README | ✅ | |

---

## Sprint 1 — Core Agent Loop + Basic Tools ✅ COMPLETE

| # | Task | Status | Notes |
|---|------|--------|-------|
| S1-1 | Claude LLM adapter | ✅ | internal/llm/anthropic.go |
| S1-2 | OpenAI-compatible adapter | ✅ | internal/llm/openaicompat.go (works with Ollama, Together, Groq, etc.) |
| S1-3 | Model router | ✅ | Haiku/Sonnet/Opus tiers + DeepSeek V3.2 via DeepInfra |
| S1-4 | web_search tool | ✅ | Serper ($0.30/1K) + Brave fallback |
| S1-5 | fetch_url tool | ✅ | With SSRF prevention, proxy rotation, caching |
| S1-6 | read_file + write_file tools | ✅ | Path-safe, workspace-scoped |
| S1-7 | run_code tool | ✅ | Python3, Go, Node, Bash |
| S1-8 | Session history management | ✅ | In-memory + SQLite persistence |
| S1-9 | Rate limiting | ✅ | Per-domain rate limiter |
| S1-10 | crypto/rand IDs | ✅ | |

---

## Sprint 2 — Memory System ✅ COMPLETE

| # | Task | Status | Notes |
|---|------|--------|-------|
| S2-1 | Embeddings | ✅ | Vertex AI text-embedding-004 |
| S2-2 | Memory table migration | ✅ | Auto-runs on startup |
| S2-3 | save_memory tool | ✅ | Moved to memory skill (Sprint 12) |
| S2-4 | Auto memory injection per turn | ✅ | Top-k semantic search |
| S2-5 | search_memory tool | ✅ | |
| S2-6 | BM25 hybrid search | ✅ | tsvector + vector fusion |
| S2-7 | Time decay scoring | ✅ | |
| S2-8 | Memory namespaces | ✅ | |
| S2-9 | Memory viewer | ✅ | Web UI panel, not CLI |

---

## Sprint 3 — Vision + File Analysis ✅ COMPLETE

| # | Task | Status | Notes |
|---|------|--------|-------|
| S3-1 | Image attachment handling | ✅ | Slack + web UI |
| S3-2 | Base64 → Claude vision | ✅ | |
| S3-3 | PDF text extraction | ✅ | pdftotext (poppler) |
| S3-4 | analyze_image tool | ✅ | |
| S3-5 | PDF extract tool | ✅ | |
| S3-6 | Chart interpretation | ✅ | Via Claude vision |
| S3-7 | Code screenshot reading | ✅ | |
| S3-8 | File size limits | ✅ | |

---

## Sprint 4 — Scraper + Anti-Block ✅ COMPLETE

| # | Task | Status | Notes |
|---|------|--------|-------|
| S4-1 | Proxy pool | ✅ | Rotating residential proxies |
| S4-2 | User-agent rotation | ✅ | |
| S4-3 | Per-domain rate limiter | ✅ | Token bucket |
| S4-4 | Domain blocklist | ✅ | |
| S4-5 | robots.txt parsing | ⏭️ | Skipped — not needed for current use cases |
| S4-6 | Retry logic | ✅ | Exponential backoff + jitter |
| S4-7 | SQLite scrape cache | ✅ | 24hr TTL |
| S4-8 | go-rod headless Chromium | ✅ | |
| S4-9 | HTML → clean text | ✅ | |
| S4-10 | Wayback fallback | ⏭️ | Not implemented |

---

## Sprint 5 — Workflow Engine ✅ COMPLETE

All tasks complete. Parallel execution, dependency resolution, Postgres persistence, cancel support.

---

## Sprint 6 — Scheduling ✅ COMPLETE

Cron scheduler + runner, webhook gateway, persistence, Slack commands.
Changed from Telegram to Slack as primary gateway (Socket Mode).

---

## Sprint 7 — Skills System ✅ COMPLETE

| Skill | Tools | Status |
|-------|-------|--------|
| GHL | 20 | ✅ Multi-location, workflow auditor, DND safety protocol |
| GitHub | 13 | ✅ Repos, issues, PRs, code search, commits, branches |
| Google Sheets | 4 | ✅ Read, write, append, list |
| Email | 1 | ✅ SMTP |
| Memory | 2 | ✅ save + search |
| Search | 2 | ✅ Serper + Brave |
| MCP Bridge | ∞ | ✅ Zero-code integration with any MCP server |
| Vault | 4 | ✅ AES-256-GCM encrypted secrets |
| Parallel Code | 2 | ✅ Farm tasks to Qwen Coder 32B |
| Factory | 2 | ✅ Autonomous software planning pipeline |

---

## Sprint 8 — Web UI + Audit ✅ COMPLETE

React 18 + Vite + Tailwind command center. SSE streaming. Memory browser.
Audit logger (Postgres). Dark theme (#0a0a1a).

---

## Sprint 9 — Security ✅ COMPLETE

| # | Task | Status |
|---|------|--------|
| S9-1 | govulncheck clean | ✅ |
| S9-4 | User whitelist | ✅ (Slack allowedUsers) |
| S9-5 | Prompt injection detection | ✅ |
| S9-6 | Tool input validation | ✅ |
| S9-7 | Destructive op confirmation | ✅ |
| S9-8 | Load test | ✅ |

---

## Sprint 10 — v1.0 Release 🔲 IN PROGRESS

| # | Task | Status |
|---|------|--------|
| S10-1 | Integration tests | 🔲 Partial — 14/32 packages have tests |
| S10-2 | Docker one-liner | ✅ docker-compose.yml ready |
| S10-3 | README + docs | ✅ Updated March 21 |
| S10-4 | Show HN | 🔲 Post pending |
| S10-5 | GHCR Docker image | 🔲 Workflow written, not yet triggered |
| S10-6 | v1.0 tag | 🔲 |

---

## Post-v1.0 Roadmap

- Multi-user auth (JWT + roles)
- Factory session persistence (Postgres)
- Config UI in web panel
- Mobile app (React Native)
- Community MCP server preset library
- Prometheus/Grafana metrics
