# ZBOT — 10-Sprint Roadmap
**Start Date:** Feb 25, 2026  
**Target v1.0:** ~May 2026  
**Sprint Length:** 1 week (Mon–Sun)

---

## Sprint 0 — Foundation ✅ IN PROGRESS
**Week of Feb 25 – Mar 3, 2026**

Goals: Repo, project structure, compiling binary, GCP wired up, Telegram echo bot alive.

| # | Task | Status |
|---|------|--------|
| S0-1 | Private GitHub repo `zbot` created | ✅ |
| S0-2 | Go module + Hexagonal directory structure scaffolded | ✅ |
| S0-3 | All core ports defined in `internal/agent/ports.go` | ✅ |
| S0-4 | Core agent loop scaffolded in `internal/agent/agent.go` | ✅ |
| S0-5 | Workflow orchestrator scaffolded in `internal/workflow/orchestrator.go` | ✅ |
| S0-6 | Memory store (pgvector) scaffolded | ✅ |
| S0-7 | GCP Secret Manager adapter wired | ✅ |
| S0-8 | All ADRs documented | ✅ |
| S0-9 | `go build ./...` passes with zero errors | ✅ |
| S0-10 | `main.go` + `wire.go` dependency injection wired | 🔲 |
| S0-11 | CI pipeline (govulncheck + test + build) | 🔲 |
| S0-12 | Telegram gateway connects, echoes messages | 🔲 |
| S0-13 | `config.example.yaml` + README complete | ✅ |

**Sprint 0 Definition of Done:** `go run ./cmd/zbot` connects to Telegram and echoes any message back.

---

## Sprint 1 — Core Agent Loop + Basic Tools
**Week of Mar 3 – Mar 9, 2026**

Goals: Real Claude API integration. web_search, read_file, write_file, run_code all working end-to-end via Telegram.

| # | Task | Status |
|---|------|--------|
| S1-1 | Claude (Anthropic) LLM adapter — `internal/gateway/claude.go` | 🔲 |
| S1-2 | OpenAI fallback adapter — `internal/gateway/openai.go` | 🔲 |
| S1-3 | Model router (sonnet default / opus on /think / haiku for lightweight) | 🔲 |
| S1-4 | `web_search` tool — Brave Search API live | 🔲 |
| S1-5 | `fetch_url` tool — HTTP fetch with SSRF prevention | 🔲 |
| S1-6 | `read_file` + `write_file` tools — path-safe, tested | 🔲 |
| S1-7 | `run_code` tool — Python + Node in Docker, live | 🔲 |
| S1-8 | Session history management (in-memory, per Telegram chat) | 🔲 |
| S1-9 | Rate limiting on Telegram gateway (token bucket) | 🔲 |
| S1-10 | `crypto/rand` based ID generation (replace stub) | 🔲 |

**Sprint 1 Definition of Done:** Send "search for recent AI news and save 3 key facts to a file" via Telegram → agent does it.

---

## Sprint 2 — Memory System Live
**Week of Mar 10 – Mar 16, 2026**

Goals: pgvector memory working. Agent remembers things across sessions. Vertex AI embeddings wired.

| # | Task | Status |
|---|------|--------|
| S2-1 | Vertex AI `text-embedding-004` embedder — real implementation | 🔲 |
| S2-2 | `zbot_memories` table migration runs on startup | 🔲 |
| S2-3 | `save_memory` tool live — saves to pgvector | 🔲 |
| S2-4 | Automatic memory injection on every turn (top-k semantic search) | 🔲 |
| S2-5 | `search_memory` tool — agent can explicitly query its memory | 🔲 |
| S2-6 | BM25 hybrid search (tsvector + vector fusion) | 🔲 |
| S2-7 | Time decay scoring — recent memories rank higher | 🔲 |
| S2-8 | Memory namespace isolation (zbot vs mem0 coexist) | 🔲 |
| S2-9 | Memory viewer CLI — list/delete memories from terminal | 🔲 |

**Sprint 2 Definition of Done:** Tell ZBOT your name in session 1. Close Telegram. Open new session. It knows your name.

---

## Sprint 3 — Vision + File Analysis
**Week of Mar 17 – Mar 23, 2026**

Goals: ZBOT can see. Photos, PDFs, screenshots analyzed via Claude vision.

| # | Task | Status |
|---|------|--------|
| S3-1 | Image attachment handling in Telegram gateway | 🔲 |
| S3-2 | Base64 image encoding → Claude vision API | 🔲 |
| S3-3 | PDF text extraction (pdftotext or pure Go) | 🔲 |
| S3-4 | `analyze_image` tool — describe, extract text, answer questions | 🔲 |
| S3-5 | `analyze_pdf` tool — extract + summarize | 🔲 |
| S3-6 | Chart/graph interpretation (Claude vision) | 🔲 |
| S3-7 | Code screenshot → read + fix code | 🔲 |
| S3-8 | File size limits + type validation | 🔲 |

**Sprint 3 Definition of Done:** Send a screenshot of a spreadsheet → ZBOT extracts the data and saves it as CSV.

---

## Sprint 4 — Scraper + Anti-Block Layer
**Week of Mar 24 – Mar 30, 2026**

Goals: ZBOT can scrape any site reliably. Proxy rotation, rate limiting, JS rendering.

| # | Task | Status |
|---|------|--------|
| S4-1 | Proxy pool client — rotating residential proxies | 🔲 |
| S4-2 | User-agent rotation library (realistic browser fingerprints) | 🔲 |
| S4-3 | Per-domain token bucket rate limiter | 🔲 |
| S4-4 | Domain blocklist — hardcoded + user-configurable | 🔲 |
| S4-5 | robots.txt parsing + respect (toggle per domain) | 🔲 |
| S4-6 | Retry logic — exponential backoff + jitter on 429/503 | 🔲 |
| S4-7 | SQLite scrape cache — 24hr TTL per URL | 🔲 |
| S4-8 | go-rod headless Chromium for JS-heavy sites | 🔲 |
| S4-9 | HTML → clean text extraction (strip nav/ads/boilerplate) | 🔲 |
| S4-10 | Wayback Machine fallback for blocked URLs | 🔲 |

**Sprint 4 Definition of Done:** Research 10 competitor websites → agent pulls full content from all 10 with zero blocks.

---

## Sprint 5 — Workflow Engine Live
**Week of Mar 31 – Apr 6, 2026**

Goals: Multi-step autonomous workflows. Parallel task execution. Survives restarts.

| # | Task | Status |
|---|------|--------|
| S5-1 | `tasks` table schema + migrations | 🔲 |
| S5-2 | `WorkflowStore` Postgres adapter (pgx v5, `FOR UPDATE SKIP LOCKED`) | 🔲 |
| S5-3 | `DataStore` adapter (Postgres JSONB for task outputs) | 🔲 |
| S5-4 | Orchestrator goroutine pool — live | 🔲 |
| S5-5 | Task decomposition via LLM (structured JSON response) | 🔲 |
| S5-6 | Parallel task execution (independent tasks run simultaneously) | 🔲 |
| S5-7 | Dependency resolution — tasks wait for upstream | 🔲 |
| S5-8 | Workflow status reporting to Telegram | 🔲 |
| S5-9 | Workflow cancellation (`/cancel <id>`) | 🔲 |
| S5-10 | Resume from restart — pending tasks picked up on boot | 🔲 |

**Sprint 5 Definition of Done:** "Research 5 competitors, summarize each, build a comparison table" → runs as 7-task parallel workflow, sends back a .md file.

---

## Sprint 6 — Proactive Automation + Scheduling
**Week of Apr 7 – Apr 13, 2026**

Goals: ZBOT can be told to do things on a schedule without being prompted.

| # | Task | Status |
|---|------|--------|
| S6-1 | Cron scheduler (pure Go, no external deps) | 🔲 |
| S6-2 | "Every morning at 8am send me a news briefing" — natural language schedule parser | 🔲 |
| S6-3 | URL/RSS monitor — alert when content changes | 🔲 |
| S6-4 | Price monitor — watch URL for value change | 🔲 |
| S6-5 | Inbound webhook receiver — trigger agent from GHL or Zapier | 🔲 |
| S6-6 | Heartbeat — ping user if no activity for N hours | 🔲 |
| S6-7 | Scheduled task persistence — survives restarts | 🔲 |
| S6-8 | List/cancel scheduled tasks via Telegram | 🔲 |

**Sprint 6 Definition of Done:** "Every Monday at 9am, check the Ziloss CRM GitHub issues and send me a summary" — runs autonomously.

---

## Sprint 7 — Skills System
**Week of Apr 14 – Apr 20, 2026**

Goals: Extensible skill registry. GHL integration skill. Custom persona support.

| # | Task | Status |
|---|------|--------|
| S7-1 | Skill interface + registry | 🔲 |
| S7-2 | Skill manifest (YAML — name, description, permissions, tools needed) | 🔲 |
| S7-3 | GHL (GoHighLevel) API skill — read contacts, pipeline, conversations | 🔲 |
| S7-4 | GitHub skill — read issues, PRs, code | 🔲 |
| S7-5 | Google Sheets skill — read/write spreadsheet data | 🔲 |
| S7-6 | Email skill (send via SMTP/SendGrid) | 🔲 |
| S7-7 | Prompt-only skills (persona definitions in YAML) | 🔲 |
| S7-8 | Skill permissions — tools a skill can access, declared in manifest | 🔲 |

**Sprint 7 Definition of Done:** "Pull all open GHL contacts tagged 'new lead', check if they're in the Google Sheet, flag any missing" — runs as a skill.

---

## Sprint 8 — Web UI + Audit Logging
**Week of Apr 21 – Apr 27, 2026**

Goals: Loopback-only web dashboard. Full audit trail. Tailscale-gated remote access.

| # | Task | Status |
|---|------|--------|
| S8-1 | Go HTTP server (loopback 127.0.0.1 only, never 0.0.0.0) | 🔲 |
| S8-2 | Audit log table — every tool call, model call, workflow event | 🔲 |
| S8-3 | Web UI: conversation history browser | 🔲 |
| S8-4 | Web UI: memory viewer + delete | 🔲 |
| S8-5 | Web UI: workflow status + cancel | 🔲 |
| S8-6 | Web UI: scheduled tasks manager | 🔲 |
| S8-7 | Web UI: audit log viewer (searchable) | 🔲 |
| S8-8 | Tailscale docs — accessing UI remotely | 🔲 |

**Sprint 8 Definition of Done:** Open `http://localhost:18790` → see full history of everything ZBOT has done, searchable.

---

## Sprint 9 — Security Hardening
**Week of Apr 28 – May 4, 2026**

Goals: govulncheck clean. Penetration test. Docker image pinned. All edge cases handled.

| # | Task | Status |
|---|------|--------|
| S9-1 | `govulncheck ./...` passes clean — zero known CVEs | 🔲 |
| S9-2 | Docker base images pinned to SHA256 (not tags) | 🔲 |
| S9-3 | TLS certificate pinning for Anthropic/OpenAI endpoints | 🔲 |
| S9-4 | Telegram `allowFrom` whitelist — only your user ID gets responses | 🔲 |
| S9-5 | Input sanitization layer — strip known prompt injection patterns | 🔲 |
| S9-6 | Output filter before tool execution — allowlist validation | 🔲 |
| S9-7 | Action confirmation for destructive operations | 🔲 |
| S9-8 | Load test — 100 concurrent Telegram messages handled gracefully | 🔲 |
| S9-9 | Penetration test — manual SSRF, path traversal, injection attempts | 🔲 |
| S9-10 | Secret rotation procedure documented | 🔲 |

**Sprint 9 Definition of Done:** `govulncheck` clean. Pen test complete. All attack vectors documented.

---

## Sprint 10 — v1.0 Release
**Week of May 5 – May 11, 2026**

Goals: Stable, documented, deployable. ZBOT is the daily driver.

| # | Task | Status |
|---|------|--------|
| S10-1 | Full integration test suite — all tools + workflow paths covered | 🔲 |
| S10-2 | Deployment guide (Mac Studio local + Cloud Run option) | 🔲 |
| S10-3 | Runbook — startup, restart, upgrade, rollback | 🔲 |
| S10-4 | Backup procedure for pgvector memories | 🔲 |
| S10-5 | Performance profiling — identify any bottlenecks | 🔲 |
| S10-6 | v1.0 tag + release notes | 🔲 |
| S10-7 | Update MASTER_PROJECT_INFO.md with ZBOT go-live | 🔲 |

**Sprint 10 Definition of Done:** ZBOT is running, stable, handling real daily tasks. v1.0 tagged.

---

## Summary Timeline

| Sprint | Week | Theme | Key Deliverable |
|--------|------|-------|-----------------|
| 0 | Feb 25 | Foundation | Repo + clean build + Telegram echo |
| 1 | Mar 3 | Agent Loop | Claude + tools working via Telegram |
| 2 | Mar 10 | Memory | Cross-session memory via pgvector |
| 3 | Mar 17 | Vision | Photo/PDF analysis |
| 4 | Mar 24 | Scraper | Anti-block web scraping |
| 5 | Mar 31 | Workflow Engine | Multi-step parallel workflows |
| 6 | Apr 7 | Scheduling | Autonomous proactive tasks |
| 7 | Apr 14 | Skills | GHL + GitHub + Sheets integrations |
| 8 | Apr 21 | Web UI | Audit dashboard |
| 9 | Apr 28 | Security | Hardening + pen test |
| 10 | May 5 | v1.0 | Ship it |
