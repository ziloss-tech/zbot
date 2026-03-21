# ZBOT — Decision Log

Running log of product, architecture, and implementation decisions.
Most recent first. For major architectural decisions, see `docs/adr/`.

---

## 2026-03-20 — Parallel Coding: Opus Architects, Qwen Implements

**Context:** Writing code with frontier models costs $3-15/M output tokens.
Most coding tasks don't need frontier reasoning — they need a bounded task,
clear interfaces, and test verification. Claudius Maximus has 512GB RAM
and Qwen Coder 32B running locally on Ollama at zero cost.

**Decision:** Build a parallel task dispatcher. Sonnet/Opus produces a
TaskManifest (architecture + interfaces + tests + bounded tasks). The
dispatcher farms individual files to Qwen Coder via Ollama. Tests verify
correctness. Retries with error feedback on failure.

**Live test result:** 3/3 tasks, all first attempt, 52 seconds total.
Qwen produced a concurrent health checker, HTTP handler, and built-in
checks (Postgres, Ollama, disk) — all compilable Go. Zero cost.

**Trade-off:** Qwen can't make architectural decisions or handle cross-module
integration. That's the orchestrator's job. If Qwen fails a test 3 times,
the task goes back to Sonnet for correction (~$0.02).

---



## 2026-03-20 — Vault: Own It vs. Use Infisical

**Context:** Need encrypted secrets storage for paid ZBOT users. Options:
Infisical (open-source, self-hosted), Doppler (SaaS), GCP Secret Manager
(already using), or build our own.

**Decision:** Build our own. AES-256-GCM encryption, HKDF per-user key
derivation from a single master key, Postgres storage, audit logging.

**Rationale:** The crypto is standard library Go (`crypto/aes`, `crypto/cipher`,
`golang.org/x/crypto/hkdf`). The complexity is in the multi-tenant key
derivation, which is ~20 lines of code. Shipping a dependency on Infisical
for 20 lines of crypto would be over-engineering. We own it, we understand
it, we can audit it.

**Trade-off:** No key rotation yet. Master key is static. Acceptable for
v1 — add rotation when someone needs it.

---

## 2026-03-20 — GitHub Skill: 5 → 13 Tools

**Context:** GitHub skill had basic issue/PR/file tools. Missing search,
code search, commits, branches, tree listing, PR creation, commenting.

**Decision:** Expand to 13 tools covering the full GitHub workflow a
developer needs. All free — just needs a GITHUB_TOKEN.

**Rationale:** Low-hanging fruit. GitHub's REST API is well-documented,
rate limits are generous (5,000/hour authenticated), and these tools make
ZBOT useful for any developer from day one.

---

## 2026-03-20 — UI Polish: react-markdown + Brain Icon

**Context:** ChatPane rendered raw markdown as plain text. Cortex label
showed the Anthropic "A" logo (wrong branding). Event bus strip used a
`window.__zbotEvents` global (hacky).

**Decision:** Added react-markdown + remark-gfm for proper rendering.
Replaced Anthropic logo with brain SVG. Cleaned event bus to use React
props. Added Clear button and per-message cost display.

**Root cause of npm build failure:** `NODE_ENV=production` was set in
the shell environment on Claudius Maximus, causing `npm install` to skip
devDependencies. Fix: `NODE_ENV=development npm install`.

---

## 2026-03-20 — GHL Sprints 1-3: 20 Tools + Multi-Location

**Context:** GHL skill had 6 basic CRUD tools for a single location.
Lead Certain operates multiple GHL locations (OCI, Esler CST, etc).

**Decision:** Rewrote GHL client with multi-location support. Added
workflow auditor, contact auditor, cross-location comparison, and a
3-phase DND review with safety protocol (read-only → test 5 → full run).

**Rationale:** The audit tools are the differentiator. Any CRM has CRUD.
ZBOT can tell you "121 of your 154 workflows are in draft status" and
"80+ follow identical templates that could be consolidated." That's
intelligence, not just API access.

---

## 2026-03-16 — Single Brain Architecture (ADR-004)

**Context:** ZBOT v1 used three models: GPT-4o planner, Claude executor,
GPT-4o critic. Information was lost at each handoff. Orchestration code
was the biggest source of bugs.

**Decision:** Replace with single-brain. Claude Sonnet handles planning,
execution, and self-critique in one context window. ~2,000 lines deleted.

**See:** docs/adr/ADR-004-single-brain.md

---

## 2026-03-16 — Prompt Modules: Dormant by Default

**Context:** Built modular prompt system (reasoning, memory policy, tool
control, verification modules). Could be activated with a one-line change
in wire.go.

**Decision:** Keep dormant. Ship with the working base prompt. Activate
modules after v1 launch when we can A/B test quality differences.

**Rationale:** Shipping speed > prompt engineering perfection. The modules
exist, they compile, they're tested. Flipping the switch is a future
sprint, not a launch blocker.

---

## 2026-02-25 — Language: Go (ADR-001)

**Context:** Needed a language for an autonomous AI agent that runs for
years, executes arbitrary code, and must be secure.

**Decision:** Go 1.22+. Single binary, native concurrency, excellent
stdlib crypto, no runtime dependencies.

**Trade-off accepted:** 30-40% slower to write than Python. Correct for
a security-sensitive autonomous agent.

**See:** docs/adr/ADR-001-language-go.md

---

## 2026-02-25 — Architecture: Hexagonal (ADR-002)

**Context:** Need testability without real databases or API keys. Need
swappability (change LLM provider, change vector DB) without touching
core logic.

**Decision:** Hexagonal architecture. `internal/agent/ports.go` defines
all interfaces. `cmd/zbot/wire.go` is the only file that knows about
concrete adapter types.

**See:** docs/adr/ADR-002-hexagonal-architecture.md

---

## Format

Each entry follows this structure:

```
## YYYY-MM-DD — Short Title

**Context:** What situation prompted this decision?
**Decision:** What did we decide?
**Rationale:** Why this option over alternatives?
**Trade-off:** What are we accepting as a downside?
**See:** Link to ADR if applicable.
```
