# ZBOT v2 — Architecture Overhaul + New Features
_Created: 2026-03-16 | Status: PLANNING_

---

## Architecture Changes (from tonight's discussion)

### 1. KILL the Planner/Executor/Critic Multi-Model Dance
- **Old:** GPT-4o Planner → Claude Sonnet Executor → GPT-4o Critic (3 models, 3 context windows, info lost at each handoff)
- **New:** Sonnet 4.6 as the single default brain for all tasks. One model, one context, no orchestration overhead.
- **Escalation:** Only use Opus 4.6 for genuinely complex tasks (long reasoning, multi-step analysis)
- **Why:** Models are good enough now to plan + execute + self-critique in a single pass. The multi-model dance was a 2024 pattern that's obsolete.

### 2. Deep Research v2 — Haiku Gathers, Thinking Model Synthesizes
- **Phase 1 (CHEAP): Source Gathering with Haiku**
  - Haiku generates 20-50 search queries from the research question
  - Haiku fires searches in parallel (Brave Search API)
  - Haiku reads each page and extracts relevant facts/quotes into structured JSON
  - Target: 30-50 sources per research session
  - Cost: ~$0.01-0.05 (Haiku is $0.25/M input, $1.25/M output — just doing pattern extraction)

- **Phase 2 (SMART): Synthesis with Extended Thinking**
  - Feed ALL extracted facts/sources from Phase 1 into Sonnet 4.6 with extended thinking
  - One pass: "Here are 47 sources on [topic]. Synthesize a comprehensive report with citations."
  - The thinking model reasons through contradictions, weighs evidence, structures the argument
  - Cost: ~$0.10-0.50 per synthesis

- **Total cost per deep research: ~$0.10-0.50** for 30-50 sources
  - vs old pipeline: $0.05-0.25 for fewer sources with worse synthesis
  - vs all-premium: $1.50-$3.00

### 3. Credentialed Site Research — Apple Keychain + GCloud
- **Default (Mac users): macOS Keychain**
  - Zero setup — already on every Mac, encrypted, Touch ID protected
  - Uses `security` CLI built into macOS (no dependencies)
  - Store: `security add-generic-password -s "zbot-{domain}" -a "{email}" -w "{password}" -U`
  - Retrieve: `security find-generic-password -s "zbot-{domain}" -w`
  - User says "Z, add my WSJ login" → ZBOT asks for credentials → stores in Keychain → done
  - macOS permission popup = user must approve access (can't be silently accessed)

- **Power users: Google Cloud Secret Manager** (existing, already built)
  - For cloud deployments or users who prefer GCloud
  - Config toggle: `secrets.backend: keychain | gcloud`

- **How credentialed scraping works:**
  ```
  credentialed_sources:
    - domain: wsj.com
      keychain_service: zbot-wsj
      auth_type: login_form
      login_url: https://accounts.wsj.com/login
    - domain: bloomberg.com
      keychain_service: zbot-bloomberg
      auth_type: api_key
    - domain: statista.com
      keychain_service: zbot-statista
      auth_type: login_form
  ```

- **Security guarantees:**
  - Credentials never in logs, memory, conversation history, or output
  - Headless browser (go-rod) logs in, scrapes, discards session immediately
  - Keychain encrypted at rest (AES-256-GCM by macOS)
  - GCloud Secret Manager encrypted at rest + in transit

### 4. OpenClaw-Style Memory Improvements
- **Memory flush before compaction:** Write critical context to pgvector BEFORE context window fills up. Marathon tasks don't lose information.
- **Daily notes layer:** Add a `memory/YYYY-MM-DD.md` daily log alongside pgvector. Human-readable, git-trackable.
- **Long-term memory curation:** Periodic promotion of important facts from daily notes to a stable MEMORY.md or equivalent pgvector namespace.
- **Time decay scoring:** Recent memories rank higher (already in ZBOT roadmap Sprint 2, may need tuning)
- **Diversity re-ranking:** Avoid returning near-duplicate memory snippets

### 5. Concurrent Task Execution
- Single API key supports unlimited concurrent requests (rate-limited by TPM/RPM, not serialized)
- Go goroutines handle parallel task execution natively
- Deep research running in background while user asks quick questions in foreground
- Task queue with worker pool — configurable max concurrent tasks (default: 5)
- Each task gets its own goroutine, own context, own conversation thread
- User sees status of all running tasks: "Deep research: 23/50 sources gathered... | Calendar: ✅ done | Email: triaging..."
- Priority system: interactive queries (user just asked something) get priority over background tasks
- Graceful cancellation: "Z, cancel the research" kills only that goroutine

### 6. Heartbeat / Proactive Scheduling (Sprint 14 — already specced)
- Cron-based wake-up at configurable intervals
- Check HEARTBEAT config for standing instructions
- Run reasoning loop → decide if user needs to be notified
- Examples: "check email every morning at 8am", "alert me if AAPL drops below $180"
- Foundation for Z-Glass concierge features

---

## New Feature: Z-Glass Integration (Future Sprint)
- Brilliant Labs Halo as I/O peripheral for ZBOT
- Glasses become a "channel" like Telegram/Slack
- Full spec: ~/Desktop/IDEAS/Z_GLASS_PROJECT.md
- Dev notes: ~/Desktop/IDEAS/BRILLIANT_LABS_DEV_NOTES.md

---

## Updated Sprint Plan

### Sprint A — Architecture Simplification (THIS WEEK)
- [ ] Rip out planner/executor/critic orchestration
- [ ] Set Sonnet 4.6 as default single brain
- [ ] Add Opus escalation logic (complexity detection)
- [ ] Update prompt modules to single-model pattern

### Sprint B — Deep Research v2
- [ ] Haiku source gathering pipeline (parallel search + extract)
- [ ] Structured JSON intermediate format for extracted facts
- [ ] Sonnet/Opus synthesis pass with extended thinking
- [ ] Citation generation with real URLs
- [ ] Cost tracking per session

### Sprint C — Credentialed Research
- [ ] Apple Keychain adapter (Go, using `security` CLI)
- [ ] GCloud Secret Manager adapter (existing, refactor to match interface)
- [ ] Config: `secrets.backend: keychain | gcloud`
- [ ] User-facing commands: "Z, add my [site] login", "Z, remove my [site] login", "Z, list my saved logins"
- [ ] Headless browser (go-rod) login flow for credentialed sites
- [ ] Domain → credential mapping config
- [ ] Credential scrubbing from all logs/memory/output

### Sprint D — Memory Overhaul
- [ ] Memory flush before context compaction
- [ ] Daily notes layer (Markdown files alongside pgvector)
- [ ] Long-term memory curation/promotion
- [ ] Time decay scoring tuning
- [ ] Diversity re-ranking for search results

### Sprint E — Heartbeat / Proactive (Sprint 14 from original roadmap)
- [ ] Cron scheduler (pure Go)
- [ ] HEARTBEAT config with standing instructions
- [ ] Natural language schedule parser
- [ ] Proactive notification via Telegram/Slack
- [ ] URL/RSS monitor
- [ ] Scheduled task persistence (survives restarts)

### Sprint F — Social Media / Launch Prep
- [ ] Create @zbot Twitter/X account
- [ ] Create landing page
- [ ] Write launch thread
- [ ] Record demo video
- [ ] GitHub README overhaul with screenshots/GIFs
- [ ] License review (currently public repo)

---

## Tech Stack (unchanged)
- **Language:** Go
- **LLM:** Anthropic API (Sonnet 4.6 default, Opus 4.6 escalation, Haiku for bulk)
- **Memory:** pgvector (PostgreSQL 34.28.163.109/ziloss_memory)
- **Search:** Brave Search API
- **Secrets:** macOS Keychain (default) + GCloud Secret Manager (power users)
- **Scraping:** go-rod (headless Chromium)
- **Channels:** Telegram, Web UI, Slack, (future: Halo glasses)
- **Infrastructure:** Claudius Maximus (Mac Studio M3 Ultra), GCP (ziloss project)

---

_Filed: ~/Desktop/Projects/zbot/docs/ZBOT_V2_PLAN.md_
