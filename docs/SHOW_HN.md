# Show HN: ZBOT — Self-hosted AI agent with brain-region cognitive architecture

ZBOT is an open-source, self-hosted AI agent that uses a multi-stage cognitive loop modeled on brain regions. Instead of one big LLM call per query, every message flows through five stages: a Frontal Lobe planner (classifies intent, builds execution plan), Hippocampus (loads relevant memory), Cortex (executes with tools), Hippocampus again (enriches context mid-task), and a Thalamus verifier (Socratic review that catches hallucinations before the user sees them).

The Thalamus stage is the differentiator. It runs a cheap Haiku call (~$0.001) that applies Aristotelian logic to verify Cortex's draft reply against the evidence it actually gathered. In testing, it caught fabricated casualty figures in a research query (35% confidence → rejected), invented Rust features that don't exist, and fake benchmark numbers. When Thalamus rejects, Cortex revises automatically — the user only sees the corrected version.

The whole cognitive overhead is ~$0.002/query (two Haiku calls for planning + verification) on top of the main Sonnet call. With the budget stack — Grok 4.1 Fast ($0.20/M input) + Serper search ($0.30/1K queries) — total cost is around $0.004 per query. Pro athlete performance for pickup game prices.

## Key points

- **5-stage cognitive loop** with verification built in — not "another AI wrapper"
- **Thalamus catches hallucinations** before they reach the user (verified on real queries)
- **Stall recovery** — detects when Claude asks for permission instead of executing, automatically retries with tool calls
- **Persistent memory** across conversations via pgvector semantic search + SQLite local history
- **Self-hosted** — single Go binary, React frontend, no Python dependencies, runs on a $5/mo VPS
- **Apache 2.0** — no vendor lock-in, bring your own API keys
- **Event-driven UI** — real-time SSE shows cognitive stages as they happen (planning → tools → verifying → done)

Repo: https://github.com/ziloss-tech/zbot

---

## First comment (architecture details)

The cognitive architecture is implemented in ~500 lines of Go across two files:

`cognitive.go` has three functions: `planTask()` (Frontal Lobe), `enrichMemory()` (Hippocampus mid-task), and `verifyReply()` (Thalamus). The main loop in `agent.go` calls them in sequence.

The key design decision: each cognitive stage uses a SEPARATE LLM call. Frontal Lobe and Thalamus use Haiku (the cheapest Claude model). Hippocampus is pure database (zero LLM cost). Only Cortex — the actual reasoning — uses Sonnet. This means the cognitive overhead is predictable and cheap.

The event bus is the nervous system. Every stage emits structured events (~50 tokens each) that the UI consumes via SSE. Thalamus reads these events to understand what Cortex is doing — it never sees the raw context, which keeps oversight cost at 5-15% instead of doubling the context window.

The hexagonal architecture means every component talks through interfaces in `ports.go`. You can swap the LLM provider (Claude, OpenAI-compatible, Ollama), the memory store (pgvector, or no-op for zero-config), and the search provider (Serper, Brave, or none) without changing the core loop.

Stack: Go backend (single binary), React + Tailwind frontend, SQLite for local state, optional Postgres for full features. Works with or without a database — gracefully degrades to in-memory everything.
