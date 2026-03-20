# ZBOT Launch Content — All Platforms
## Generated 2026-03-20

## 1. HACKER NEWS — Show HN
Title: Show HN: ZBOT – Self-hosted AI agent with built-in hallucination detection (Go, Apache 2.0)
Body: see docs/SHOW_HN.md in repo
First comment: see docs/SHOW_HN.md — architecture details section

## 2. X/TWITTER THREAD

### Tweet 1 (hook):
I built an open-source AI agent that catches its own hallucinations before you see them.

ZBOT runs a 5-stage cognitive loop: plan → remember → execute → verify → respond.

The verification stage (Thalamus) caught fabricated stats, fake Rust features, and made-up benchmarks in testing.

Self-hosted. Single Go binary. Apache 2.0.

github.com/ziloss-tech/zbot

### Tweet 2 (cost angle):
Cost breakdown per query:

Frontier stack (Claude Sonnet): ~$0.07
Budget stack (Grok Fast + Serper): ~$0.004
Local stack (Ollama): ~$0.0003

The cognitive overhead (planning + verification) adds ~$0.002. That's two Haiku calls.

Pro athlete performance for pickup game prices.

### Tweet 3 (tech):
Stack:
- Go backend (single binary, no Python deps)
- React + Tailwind UI with SSE streaming
- pgvector for persistent memory
- Works with Ollama, Together, Groq, Claude, GPT
- Credentialed web scraping (macOS Keychain)
- Deep research: 30-50 sources for ~$0.50

### Tweet 4 (CTA):
Try it:

git clone https://github.com/ziloss-tech/zbot
cp .env.example .env
go run ./cmd/zbot

Open localhost:18790. You're running a self-hosted AI agent with memory, tools, and hallucination detection.

Star it if it's useful. PRs welcome.

## 3. REDDIT POSTS

### r/selfhosted
Title: ZBOT — Self-hosted AI agent with persistent memory, tool use, and built-in hallucination detection [Go, Apache 2.0]

### r/LocalLLaMA
Title: Built an AI agent framework that works with Ollama out of the box — persistent memory, tool use, hallucination detection

### r/golang
Title: Show r/golang: ZBOT — AI agent platform in Go with hexagonal architecture, SSE streaming, and pgvector memory

## 4. YOUTUBE — 2 min demo
Script outline in the full launch-content.md on Claude's computer.

## 5. ACCOUNTS NEEDED
- [ ] X/Twitter: @ziloss_tech (or @zbot_agent — check availability)
- [ ] YouTube: Ziloss Technologies channel
- [ ] Reddit: can use personal account

## 6. LAUNCH ORDER
1. Push final code to GitHub (DONE)
2. Create X account
3. Record 2-min demo video → upload to YouTube
4. Post Show HN (weekday morning, 9-10am ET for best visibility)
5. Post X thread immediately after HN
6. Post Reddit (r/selfhosted, r/LocalLLaMA, r/golang) — stagger by 2-3 hours
7. Share in Ollama Discord
