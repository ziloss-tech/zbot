# ZBOT — Product Requirements Document

**Version:** 1.0
**Author:** Jeremy Lerwick
**Date:** March 20, 2026
**Status:** Pre-launch (Show HN pending)

---

## 1. What Is ZBOT

ZBOT is a self-hosted AI agent that gives individuals and small teams
enterprise-grade AI automation for the cost of their own API keys.

Users bring a Claude key (or any OpenAI-compatible model), a Postgres
database, and their service API keys. ZBOT provides the orchestration
layer: cognitive reasoning, tool use, memory, workflows, research,
and an encrypted vault — all running on their own hardware.

## 2. Who Is It For

**Primary persona: The Technical Solo Operator.**
Runs a small business (1-10 people). Uses GoHighLevel, Stripe, GitHub,
Google Sheets, and 3-5 other SaaS tools daily. Comfortable with a
terminal. Tired of paying $500+/month for Zapier + AI chatbots +
secrets management when they know the underlying APIs are simple.

**Secondary persona: The Self-Hosted Enthusiast.**
Runs local AI models (Ollama, vLLM, LM Studio). Wants an agent
framework that works with any model, not just Claude. Values data
sovereignty — their conversations, memories, and secrets never leave
their hardware.

**Anti-persona: Enterprise IT buyer.**
ZBOT is not a managed SaaS product (yet). It requires a terminal,
a Postgres instance, and API keys. If someone needs a procurement
process and an SLA, ZBOT isn't ready for them.

## 3. The Problem

SaaS platforms charge $50-500/month each for what amounts to
authenticated API calls plus simple workflow logic:
- Zapier/Make: $50-200/mo for if-then automation
- Infisical/Doppler: $150-600/mo for secrets management
- Perplexity/research tools: $200+/mo for multi-source research
- AI chatbot platforms: $500+/mo for "AI assistants"

A technical operator using 5 of these services pays $1,000-2,000/month
for capabilities that are fundamentally simple: call APIs, store data,
make decisions.

## 4. The Solution

ZBOT replaces the entire middle layer with one self-hosted binary:

| Capability | Replaces | Their Cost | ZBOT Cost |
|---|---|---|---|
| 20 GHL tools | GHL manual ops | $297-497/mo | $0 |
| 13 GitHub tools | GitHub Copilot workspace | $19-39/mo | $0 |
| Encrypted vault | Infisical/Doppler | $150-600/mo | $0 |
| MCP bridge | Zapier/Make connectors | $50-200/mo | $0 |
| Deep research | Perplexity Pro | $200+/mo | ~$0.04/query |
| Cognitive engine | AI chatbot platforms | $500+/mo | ~$0.002/turn |
| Workflow orchestrator | Custom dev | $5,000+ setup | $0 |

Total user cost: API keys (~$20-50/mo) + Postgres (~$7/mo).

## 5. Success Metrics

**Launch (Week 1-2):**
- Show HN post published
- 50+ GitHub stars
- 10+ people successfully self-host ZBOT
- Zero critical bugs reported in first 48 hours

**Month 1:**
- 200+ GitHub stars
- 5+ community-contributed MCP server configs
- 1 non-Jeremy user submits a PR
- r/selfhosted and r/LocalLLaMA posts gain traction

**Month 3:**
- 1,000+ GitHub stars
- Active Discord/community with 100+ members
- 3+ blog posts or YouTube videos by community members
- Clear signal on what paid tier should include

## 6. Feature Map

### Core (Shipped)
- Single-brain cognitive engine (plan → memory → execute → verify)
- 54 built-in tools across 7 skill categories
- MCP bridge for zero-code integration with any MCP server
- Encrypted secrets vault (AES-256-GCM, per-user keys)
- Web UI with SSE streaming and markdown rendering
- Slack gateway (Socket Mode)
- Persistent memory (pgvector + semantic search)
- Deep research pipeline (multi-model)
- Workflow orchestrator with task graphs
- Cron scheduler + monitor runner

### Next (Prioritized)
- YouTube demo video (2 min, show cognitive loop)
- Docker one-liner install (`docker run ziloss/zbot`)
- Config UI in web panel (add MCP servers, manage vault)
- Community MCP server preset library
- Prometheus/Grafana metrics export

### Future (Validated by demand)
- Multi-user auth (JWT + roles)
- Team shared vault
- Hosted tier (managed ZBOT for non-technical users)
- Mobile app (React Native, connects to self-hosted instance)
- Plugin marketplace

## 7. What ZBOT Is NOT

- Not a managed SaaS (user runs it themselves)
- Not a chatbot wrapper (it has tools, memory, and workflows)
- Not an AI model (it orchestrates models — any model)
- Not a Zapier clone (Zapier is dumb triggers; ZBOT reasons)
- Not enterprise software (no SSO, no SLA, no procurement)

## 8. Technical Constraints

- Single Go binary, no runtime dependencies
- Must work with or without Postgres (graceful degradation)
- Must work with or without internet (local models supported)
- Secrets never logged, never displayed in UI unless user requests
- All MCP servers run as child processes (isolated, killable)
- Context window managed: auto-compact at 70%, clear at 90%

## 9. Open Questions

- Pricing model for hosted tier (per-seat? per-workflow? flat?)
- Should ZBOT have its own model routing marketplace?
- Community governance model (BDFL? elected maintainers?)
- License: Apache 2.0 (current) vs AGPL for hosted protection?
