# ZBOT v2 — Technical Documentation

_Created: 2026-03-16 | Status: DRAFT_
_Audience: Developer (you, building this)_

---

## 1. Overview

This document is the implementation reference for ZBOT v2. It covers API contracts, data models, configuration, the port interface changes, and component-level specs for each subsystem. Read this alongside `ZBOT_V2_ARCHITECTURE.md` (system design) and the ADRs in `adr/` (decision rationale).

### What's New in v2

| Change | What It Means in Code |
|--------|-----------------------|
| Single brain | Delete planner/executor/critic. One `LLMClient` interface, model router in adapter. |
| Deep research v2 | New `deep_research` tool. Two-phase pipeline: Haiku gather → Sonnet synthesize. |
| Credentialed scraping | New `SecretsManager` port. Keychain + GCloud adapters. go-rod browser flow. |
| Memory overhaul | Daily notes layer. Context window flush. Diversity re-ranking. |
| Concurrent tasks | Priority queue + goroutine worker pool. Interactive queries never blocked. |
| Dynamic split-pane UI | New PaneManager. Delete 5 old components. New pane types. |

---

## 2. Directory Structure (Updated for v2)

```
zbot/
├── cmd/
│   └── zbot/
│       ├── main.go              # Dependency injection, wiring
│       └── wire.go              # Wire definitions
├── internal/
│   ├── agent/
│   │   ├── agent.go             # Core agent loop (MODIFIED: single-brain)
│   │   ├── ports.go             # Port interfaces (MODIFIED: collapsed LLM interfaces)
│   │   ├── router.go            # NEW: model tier selection logic
│   │   └── memory_injector.go   # MODIFIED: diversity re-ranking, daily notes
│   ├── research/
│   │   ├── gatherer.go          # NEW: Haiku source gathering pipeline
│   │   ├── synthesizer.go       # NEW: Sonnet synthesis pass
│   │   ├── types.go             # NEW: ResearchFact, ResearchSession types
│   │   └── research_test.go     # NEW: tests
│   ├── secrets/
│   │   ├── keychain.go          # NEW: macOS Keychain adapter
│   │   ├── gcloud.go            # MODIFIED: refactored to match SecretsManager port
│   │   └── scrubber.go          # NEW: credential scrubbing from logs/memory
│   ├── memory/
│   │   ├── pgvector.go          # EXISTING: pgvector adapter
│   │   ├── daily_notes.go       # NEW: daily markdown notes layer
│   │   ├── curator.go           # NEW: periodic memory promotion
│   │   ├── diversity.go         # NEW: diversity re-ranking filter
│   │   └── flush.go             # NEW: pre-compaction context flush
│   ├── scheduler/
│   │   ├── queue.go             # NEW: priority task queue
│   │   ├── pool.go              # NEW: goroutine worker pool
│   │   └── scheduler.go         # NEW: task scheduler (replaces parts of orchestrator)
│   ├── tools/
│   │   ├── web_search.go        # EXISTING
│   │   ├── fetch_url.go         # EXISTING
│   │   ├── run_code.go          # EXISTING
│   │   ├── deep_research.go     # NEW: orchestrates gather → synthesize
│   │   ├── credentialed_fetch.go # NEW: keychain → go-rod → content
│   │   ├── save_memory.go       # EXISTING
│   │   └── search_memory.go     # EXISTING
│   ├── gateway/
│   │   ├── telegram.go          # EXISTING
│   │   ├── claude.go            # MODIFIED: model router, streaming support
│   │   └── webui.go             # MODIFIED: SSE endpoints, pane routing
│   ├── webui/
│   │   ├── handlers.go          # MODIFIED: new API endpoints
│   │   ├── hub.go               # MODIFIED: pane-aware SSE routing
│   │   ├── embed.go             # EXISTING: go:embed frontend
│   │   └── frontend/            # MODIFIED: new pane-based UI
│   │       └── src/
│   │           ├── App.tsx
│   │           ├── components/
│   │           │   ├── CommandBar.tsx       # KEEP
│   │           │   ├── PaneManager.tsx      # NEW: dynamic split-pane layout
│   │           │   ├── ChatPane.tsx         # NEW: replaces part of old UI
│   │           │   ├── CodePane.tsx         # NEW: syntax editor + run
│   │           │   ├── ResearchPane.tsx     # EVOLVED from ResearchPanel
│   │           │   ├── TerminalPane.tsx     # NEW (nice-to-have)
│   │           │   ├── FileViewerPane.tsx   # EVOLVED from WorkspacePanel
│   │           │   ├── MemoryPane.tsx       # EVOLVED from MemoryPanel
│   │           │   ├── MetricsStrip.tsx     # SIMPLIFIED
│   │           │   └── Sidebar.tsx          # KEEP, updated nav
│   │           ├── hooks/
│   │           │   ├── useSSE.ts            # MODIFIED: pane-aware events
│   │           │   ├── usePanes.ts          # NEW: pane state management
│   │           │   └── useWorkflow.ts       # KEEP
│   │           └── lib/
│   │               ├── api.ts              # MODIFIED: new endpoints
│   │               └── types.ts            # MODIFIED: pane types
│   ├── planner/                 # DELETE ENTIRE PACKAGE
│   ├── prompts/
│   │   ├── gpt_prompts.go      # DELETE (GPT-4o planner/critic prompts)
│   │   └── system_prompts.go   # MODIFIED: single-brain prompt structure
│   └── workflow/
│       └── orchestrator.go      # MODIFIED: simplified, no critic step
├── config.example.yaml          # MODIFIED: new secrets.backend field
├── scripts/
│   └── build-ui.sh             # EXISTING
└── docs/                        # YOU ARE HERE
```

---

## 3. Port Interfaces (Updated)

### 3.1 LLMClient (replaces PlannerClient + ExecutorClient + CriticClient)

```go
// internal/agent/ports.go

type ModelTier string
const (
    ModelHaiku  ModelTier = "haiku"
    ModelSonnet ModelTier = "sonnet"
    ModelOpus   ModelTier = "opus"
    ModelAuto   ModelTier = "auto"  // let router decide
)

type LLMClient interface {
    // Chat sends a request and returns the full response.
    Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)

    // ChatStream sends a request and streams tokens to the channel.
    // The returned ChatResponse contains the full accumulated response.
    ChatStream(ctx context.Context, req ChatRequest, tokens chan<- string) (*ChatResponse, error)
}

type ChatRequest struct {
    Messages      []Message
    Tools         []ToolDef
    ModelHint     ModelTier   // which model tier to use
    Stream        bool
    MaxTokens     int
    Temperature   float64
    ExtendedThink bool        // enable extended thinking (Sonnet/Opus only)
}

type ChatResponse struct {
    Content    string
    ToolCalls  []ToolCall
    StopReason string
    Usage      TokenUsage
    Model      string       // actual model used (for logging)
}

type TokenUsage struct {
    InputTokens  int
    OutputTokens int
    CacheRead    int  // prompt caching hits
    CacheWrite   int
}
```

### 3.2 SecretsManager (new)

```go
type SecretsManager interface {
    // Store saves a credential. Overwrites if key exists.
    Store(ctx context.Context, key string, value []byte) error

    // Retrieve fetches a credential. Returns ErrNotFound if missing.
    Retrieve(ctx context.Context, key string) ([]byte, error)

    // Delete removes a credential.
    Delete(ctx context.Context, key string) error

    // List returns all keys matching the prefix.
    List(ctx context.Context, prefix string) ([]string, error)
}
```

### 3.3 MemoryStore (updated)

```go
type MemoryStore interface {
    // Save stores a memory with embedding.
    Save(ctx context.Context, mem Memory) error

    // Search returns semantically similar memories, diversity-reranked.
    Search(ctx context.Context, query string, opts SearchOpts) ([]Memory, error)

    // FlushContext extracts and saves critical facts before context compaction.
    FlushContext(ctx context.Context, conversation []Message) error

    // WriteDailyNote appends an entry to today's daily notes file.
    WriteDailyNote(ctx context.Context, entry string) error

    // Delete removes a memory by ID.
    Delete(ctx context.Context, id string) error
}

type SearchOpts struct {
    TopK             int
    DiversityThreshold float64  // cosine similarity threshold for re-ranking (default: 0.92)
    Namespace        string
    MinScore         float64
}
```

### 3.4 Gateway (unchanged)

```go
type Gateway interface {
    Start(ctx context.Context) error
    SendMessage(ctx context.Context, chatID string, msg string) error
    OnMessage(handler func(ctx context.Context, msg IncomingMessage))
}
```

---

## 4. API Contracts (Web UI)

### 4.1 SSE Stream (updated for panes)

```
GET /api/stream/:workflowID
```

Response: `text/event-stream`

```
data: {"workflow_id":"abc123","pane_id":"research-1","source":"agent","type":"token","payload":"The"}
data: {"workflow_id":"abc123","pane_id":"research-1","source":"agent","type":"token","payload":" key"}
data: {"workflow_id":"abc123","pane_id":"research-1","source":"agent","type":"status","payload":"gathering: 23/50 sources"}
data: {"workflow_id":"abc123","pane_id":"code-1","source":"agent","type":"code","payload":"import requests\n..."}
data: {"workflow_id":"abc123","pane_id":"","source":"system","type":"pane_open","payload":"{\"type\":\"research\",\"id\":\"research-1\"}"}
data: {"workflow_id":"abc123","pane_id":"","source":"system","type":"complete","payload":""}
```

Event types:
| Type | Description |
|------|-------------|
| `token` | Streaming text token for a pane |
| `status` | Status update (e.g., "gathering: 23/50 sources") |
| `code` | Code block content for Code pane |
| `pane_open` | Server requests the frontend to open a pane |
| `pane_close` | Server requests the frontend to close a pane |
| `complete` | Workflow/task finished |
| `error` | Error occurred |

**Reconnection:** On reconnect, client sends `Last-Event-ID` header. Server replays events from `zbot_stream_events` table after that ID.

**Keepalive:** Server sends `:ping\n\n` comment every 15 seconds.

### 4.2 Submit Message

```
POST /api/message
Content-Type: application/json

{
  "content": "Research AR display technology for Z-Glass",
  "conversation_id": "conv_abc123"  // optional, creates new if omitted
}

Response:
{
  "conversation_id": "conv_abc123",
  "workflow_id": "wf_def456"  // if a workflow was spawned
}
```

### 4.3 Pane Management

```
GET /api/panes
Response:
{
  "panes": [
    {"id": "chat-main", "type": "chat", "title": "Chat"},
    {"id": "research-1", "type": "research", "title": "AR Display Research"}
  ]
}

POST /api/panes
Body: {"type": "code", "title": "Script Editor"}
Response: {"id": "code-1", "type": "code", "title": "Script Editor"}

DELETE /api/panes/:id
Response: 204 No Content
```

### 4.4 Workflows

```
GET /api/workflows
Response:
{
  "workflows": [
    {
      "id": "wf_def456",
      "goal": "Research AR display technology",
      "status": "running",
      "tasks_total": 3,
      "tasks_done": 1,
      "created_at": "2026-03-16T10:30:00Z"
    }
  ]
}

GET /api/workflow/:id
Response:
{
  "id": "wf_def456",
  "goal": "Research AR display technology",
  "status": "running",
  "tasks": [
    {
      "id": "task_1",
      "title": "Generate search queries",
      "status": "done",
      "output": "Generated 35 queries",
      "started_at": "...",
      "finished_at": "..."
    },
    {
      "id": "task_2",
      "title": "Gather sources",
      "status": "running",
      "output": "23/50 sources gathered...",
      "started_at": "..."
    }
  ]
}
```

### 4.5 Memory

```
GET /api/memories?q=AR+displays&limit=10
Response:
{
  "memories": [
    {
      "id": "mem_abc",
      "content": "User is building Z-Glass with AR display...",
      "score": 0.89,
      "created_at": "2026-03-15T14:00:00Z",
      "namespace": "default"
    }
  ]
}

DELETE /api/memories/:id
Response: 204 No Content
```

### 4.6 Credentials

```
POST /api/credentials
Body: {"domain": "wsj.com", "email": "user@example.com", "password": "..."}
Response: 201 Created
// Password is stored in Keychain/GCloud, NEVER in Postgres or response body

GET /api/credentials
Response:
{
  "credentials": [
    {"domain": "wsj.com", "email": "user@example.com"},
    {"domain": "bloomberg.com", "email": "user@example.com"}
  ]
}
// Note: passwords NEVER returned in list

DELETE /api/credentials/:domain
Response: 204 No Content
```

---

## 5. Data Models

### 5.1 Database Schema Changes (v2 migrations)

```sql
-- Migration: v2_001_task_output.sql
-- Add output tracking to tasks table
ALTER TABLE zbot_tasks ADD COLUMN IF NOT EXISTS output TEXT NOT NULL DEFAULT '';
ALTER TABLE zbot_tasks ADD COLUMN IF NOT EXISTS started_at TIMESTAMPTZ;
ALTER TABLE zbot_tasks ADD COLUMN IF NOT EXISTS finished_at TIMESTAMPTZ;
ALTER TABLE zbot_tasks ADD COLUMN IF NOT EXISTS error TEXT NOT NULL DEFAULT '';
ALTER TABLE zbot_tasks ADD COLUMN IF NOT EXISTS priority INT NOT NULL DEFAULT 1;

-- Migration: v2_002_stream_events.sql
-- SSE event log for replay on reconnect
CREATE TABLE IF NOT EXISTS zbot_stream_events (
    id          BIGSERIAL PRIMARY KEY,
    workflow_id TEXT NOT NULL,
    task_id     TEXT,
    pane_id     TEXT,
    source      TEXT NOT NULL,  -- 'agent' | 'system'
    event_type  TEXT NOT NULL,  -- 'token' | 'status' | 'code' | 'pane_open' | 'complete' | 'error'
    payload     TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_stream_events_workflow
    ON zbot_stream_events(workflow_id, id);

-- Migration: v2_003_daily_notes.sql
-- Track daily notes metadata (actual notes are markdown files on disk)
CREATE TABLE IF NOT EXISTS zbot_daily_notes (
    id         BIGSERIAL PRIMARY KEY,
    note_date  DATE NOT NULL,
    file_path  TEXT NOT NULL,
    entry_count INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(note_date)
);

-- Migration: v2_004_research_sessions.sql
-- Track deep research sessions
CREATE TABLE IF NOT EXISTS zbot_research_sessions (
    id            TEXT PRIMARY KEY,
    query         TEXT NOT NULL,
    status        TEXT NOT NULL DEFAULT 'gathering',  -- gathering | synthesizing | done | failed
    sources_total INT NOT NULL DEFAULT 0,
    sources_done  INT NOT NULL DEFAULT 0,
    gather_cost   NUMERIC(10,6) NOT NULL DEFAULT 0,
    synth_cost    NUMERIC(10,6) NOT NULL DEFAULT 0,
    report        TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at   TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS zbot_research_facts (
    id          BIGSERIAL PRIMARY KEY,
    session_id  TEXT NOT NULL REFERENCES zbot_research_sessions(id),
    source_url  TEXT NOT NULL,
    title       TEXT NOT NULL DEFAULT '',
    facts       JSONB NOT NULL DEFAULT '[]',
    quotes      JSONB NOT NULL DEFAULT '[]',
    relevance   REAL NOT NULL DEFAULT 0,
    fetched_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_research_facts_session
    ON zbot_research_facts(session_id);

-- Migration: v2_005_credential_domains.sql
-- Track which domains have stored credentials (NOT the credentials themselves)
CREATE TABLE IF NOT EXISTS zbot_credential_domains (
    domain      TEXT PRIMARY KEY,
    email       TEXT NOT NULL,
    auth_type   TEXT NOT NULL DEFAULT 'login_form',  -- login_form | api_key
    login_url   TEXT,
    backend     TEXT NOT NULL DEFAULT 'keychain',     -- keychain | gcloud
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

### 5.2 Go Types

```go
// internal/research/types.go

type ResearchSession struct {
    ID           string
    Query        string
    Status       string  // "gathering" | "synthesizing" | "done" | "failed"
    SourcesTotal int
    SourcesDone  int
    GatherCost   float64
    SynthCost    float64
    Report       string
    Facts        []ResearchFact
    CreatedAt    time.Time
    FinishedAt   *time.Time
}

type ResearchFact struct {
    ID        int64
    SessionID string
    SourceURL string
    Title     string
    Facts     []string
    Quotes    []string
    Relevance float64
    FetchedAt time.Time
}
```

```go
// internal/scheduler/queue.go

type Task struct {
    ID          string
    WorkflowID  string
    Title       string
    Priority    int             // 0=interactive, 1=foreground, 2=background, 3=scheduled
    Status      TaskStatus      // queued | running | done | failed | cancelled
    Instruction string
    Output      string
    Error       string
    Cancel      context.CancelFunc
    DependsOn   []string        // task IDs this task waits for
    StartedAt   *time.Time
    FinishedAt  *time.Time
}

type TaskStatus string
const (
    TaskQueued    TaskStatus = "queued"
    TaskRunning   TaskStatus = "running"
    TaskDone      TaskStatus = "done"
    TaskFailed    TaskStatus = "failed"
    TaskCancelled TaskStatus = "cancelled"
)
```

---

## 6. Configuration (Updated config.yaml)

```yaml
# config.yaml — ZBOT v2

# LLM Configuration
llm:
  provider: anthropic
  default_model: sonnet    # haiku | sonnet | opus
  escalation_model: opus
  bulk_model: haiku
  api_key_env: ANTHROPIC_API_KEY  # read from env var
  max_tokens: 8192
  extended_thinking: true   # enable for research synthesis

# Model Router
router:
  auto_escalate: true       # auto-escalate to opus on complex tasks
  escalation_triggers:
    tool_chain_depth: 5     # escalate if >5 sequential tool calls
    token_budget_pct: 90    # escalate if sonnet uses >90% of token budget

# Memory
memory:
  backend: pgvector
  postgres_url_env: DATABASE_URL
  embedding_model: text-embedding-004
  search_top_k: 5
  diversity_threshold: 0.92
  daily_notes_dir: ./memory  # relative to ZBOT working directory
  flush_threshold_pct: 80   # flush context when usage > 80%
  curation_schedule: weekly  # daily | weekly | manual

# Secrets
secrets:
  backend: keychain          # keychain | gcloud
  gcloud_project: ziloss     # only used if backend=gcloud

# Deep Research
research:
  max_sources: 50
  gather_workers: 10         # parallel goroutines for fetching
  brave_api_key_env: BRAVE_API_KEY
  brave_rate_limit: 15       # requests per second
  failure_threshold_pct: 50  # abort if >50% sources fail
  synthesis_model: sonnet    # sonnet | opus

# Credentialed Sites
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
    login_url: https://www.statista.com/login

# Task Scheduler
scheduler:
  max_workers: 5
  priority_levels: 4        # 0=interactive, 1=foreground, 2=background, 3=scheduled

# Web UI
webui:
  enabled: true
  bind: 127.0.0.1:18790     # NEVER 0.0.0.0
  sse_ping_interval: 15s
  sse_replay_limit: 100     # max events replayed on reconnect

# Gateways
telegram:
  enabled: true
  bot_token_env: TELEGRAM_BOT_TOKEN
  allow_from:
    - 123456789              # your Telegram user ID

slack:
  enabled: false
  bot_token_env: SLACK_BOT_TOKEN

# Execution Sandbox
sandbox:
  runtime: docker
  memory_limit: 512m
  cpu_limit: 1
  network: none              # none | bridge (for tools that need internet)
  timeout: 60s

# Audit
audit:
  enabled: true
  postgres_url_env: DATABASE_URL  # same DB, different table
```

---

## 7. Implementation Order

Based on the sprint plan in `ZBOT_V2_PLAN.md`, here's the recommended implementation sequence with dependencies:

### Sprint A — Architecture Simplification (THIS WEEK)

**Goal:** Single-brain agent running, old multi-model code deleted.

| # | Task | Depends On | Files |
|---|------|------------|-------|
| A-1 | Update `ports.go` — collapse LLM interfaces | — | `internal/agent/ports.go` |
| A-2 | Implement model router in Claude adapter | A-1 | `internal/gateway/claude.go`, `internal/agent/router.go` |
| A-3 | Simplify agent loop for single-brain | A-1 | `internal/agent/agent.go` |
| A-4 | Delete planner package | A-3 | `internal/planner/` (entire dir) |
| A-5 | Delete GPT prompts | A-4 | `internal/prompts/gpt_prompts.go` |
| A-6 | Remove critic step from orchestrator | A-3 | `internal/workflow/orchestrator.go` |
| A-7 | Update system prompts for single-brain pattern | A-3 | `internal/prompts/system_prompts.go` |
| A-8 | Update `main.go` wiring | A-1,A-2 | `cmd/zbot/main.go` |
| A-9 | `go build ./...` clean | All above | — |
| A-10 | Integration test: message → tool use → response | A-9 | — |

**Definition of Done:** Send a multi-step task via Telegram → Sonnet handles it end-to-end without planner/critic involvement.

### Sprint B — Deep Research v2

| # | Task | Depends On | Files |
|---|------|------------|-------|
| B-1 | Create `internal/research/` package + types | — | `internal/research/types.go` |
| B-2 | Implement Haiku gatherer | A-2 (model router) | `internal/research/gatherer.go` |
| B-3 | Implement Sonnet synthesizer | A-2 | `internal/research/synthesizer.go` |
| B-4 | Run v2 migrations (research tables) | — | SQL migrations |
| B-5 | Create `deep_research` tool | B-2, B-3 | `internal/tools/deep_research.go` |
| B-6 | Wire to SSE for live progress | B-5 | `internal/webui/hub.go` |
| B-7 | Cost tracking per session | B-5 | `internal/research/synthesizer.go` |

### Sprint C — Credentialed Research

| # | Task | Depends On | Files |
|---|------|------------|-------|
| C-1 | Implement Keychain adapter | — | `internal/secrets/keychain.go` |
| C-2 | Refactor GCloud adapter to `SecretsManager` port | — | `internal/secrets/gcloud.go` |
| C-3 | Run v2 migration (credential_domains) | — | SQL migration |
| C-4 | Implement credential scrubber | C-1 | `internal/secrets/scrubber.go` |
| C-5 | Implement go-rod login flow | C-1 or C-2 | `internal/tools/credentialed_fetch.go` |
| C-6 | Add user commands: add/remove/list logins | C-5 | Agent tool definitions |
| C-7 | Security audit: verify credentials never leak | C-5 | Manual test |

### Sprint D — Memory Overhaul

| # | Task | Depends On | Files |
|---|------|------------|-------|
| D-1 | Implement daily notes writer | — | `internal/memory/daily_notes.go` |
| D-2 | Implement context window flush | — | `internal/memory/flush.go` |
| D-3 | Implement diversity re-ranking | — | `internal/memory/diversity.go` |
| D-4 | Implement memory curator | D-1 | `internal/memory/curator.go` |
| D-5 | Update `MemoryStore` port | D-1,D-2,D-3 | `internal/agent/ports.go` |
| D-6 | Wire flush into agent loop | D-2 | `internal/agent/agent.go` |

### Sprint E — UI Overhaul (Parallel with B/C/D backend work)

| # | Task | Depends On | Files |
|---|------|------------|-------|
| E-1 | Delete old components (Planner, Executor, Observer, Handoff, Critic) | — | Frontend |
| E-2 | Implement PaneManager.tsx | — | `frontend/src/components/PaneManager.tsx` |
| E-3 | Implement ChatPane.tsx | E-2 | `frontend/src/components/ChatPane.tsx` |
| E-4 | Implement CodePane.tsx | E-2 | `frontend/src/components/CodePane.tsx` |
| E-5 | Evolve ResearchPane.tsx | E-2 | `frontend/src/components/ResearchPane.tsx` |
| E-6 | Update useSSE.ts for pane-aware events | E-2 | `frontend/src/hooks/useSSE.ts` |
| E-7 | Implement usePanes.ts hook | E-2 | `frontend/src/hooks/usePanes.ts` |
| E-8 | Simplify MetricsStrip | — | `frontend/src/components/MetricsStrip.tsx` |
| E-9 | Update Sidebar nav | — | `frontend/src/components/Sidebar.tsx` |
| E-10 | Responsive layout (mobile stacked) | E-2 | CSS/Tailwind |

---

## 8. Tool Definitions (v2)

### 8.1 deep_research (NEW)

```json
{
  "name": "deep_research",
  "description": "Conduct deep research on a topic. Gathers 30-50 sources in parallel, then synthesizes a comprehensive report with citations.",
  "input_schema": {
    "type": "object",
    "properties": {
      "query": {
        "type": "string",
        "description": "The research question or topic"
      },
      "max_sources": {
        "type": "integer",
        "description": "Maximum number of sources to gather (default: 50)"
      },
      "focus_areas": {
        "type": "array",
        "items": {"type": "string"},
        "description": "Optional list of specific aspects to focus on"
      }
    },
    "required": ["query"]
  }
}
```

### 8.2 credentialed_fetch (NEW)

```json
{
  "name": "credentialed_fetch",
  "description": "Fetch content from a paywalled site using stored credentials. Credentials are never visible to the model.",
  "input_schema": {
    "type": "object",
    "properties": {
      "url": {
        "type": "string",
        "description": "The URL to fetch"
      },
      "domain": {
        "type": "string",
        "description": "The domain to look up credentials for (e.g., 'wsj.com')"
      }
    },
    "required": ["url", "domain"]
  }
}
```

### 8.3 manage_credentials (NEW)

```json
{
  "name": "manage_credentials",
  "description": "Add, remove, or list stored site credentials.",
  "input_schema": {
    "type": "object",
    "properties": {
      "action": {
        "type": "string",
        "enum": ["add", "remove", "list"],
        "description": "The action to perform"
      },
      "domain": {
        "type": "string",
        "description": "Site domain (required for add/remove)"
      },
      "email": {
        "type": "string",
        "description": "Login email (required for add)"
      },
      "password": {
        "type": "string",
        "description": "Login password (required for add). Stored in Keychain/GCloud, never in conversation."
      }
    },
    "required": ["action"]
  }
}
```

---

## 9. Error Handling Patterns

### 9.1 LLM Errors

| Error | Response |
|-------|----------|
| Rate limit (429) | Exponential backoff: 1s, 2s, 4s, max 30s. Max 5 retries. |
| Server error (500/503) | Retry 3 times with backoff. If persistent, notify user. |
| Context too long | Trigger context flush (save to memory), compact, retry. |
| Model overloaded | Queue the request, retry after 5s. Show "busy" status in UI. |

### 9.2 Tool Errors

| Error | Response |
|-------|----------|
| Docker timeout | Kill container, return timeout error to model, let it decide next step. |
| Fetch URL blocked | Return error to model with reason. Model may try alternative URL or tell user. |
| Credential not found | Return "no credentials stored for {domain}" to model. Model asks user to add credentials. |
| Search API failure | Return error. Model may reformulate query or report the issue. |

### 9.3 SSE Connection Errors

| Error | Response |
|-------|----------|
| Client disconnect | Server cleans up subscriber. No action needed. |
| Client reconnect | Replay last N events from `zbot_stream_events` (keyed by `Last-Event-ID`). |
| Server restart | All SSE connections drop. Clients reconnect, full state restored from Postgres. |

---

## 10. Testing Strategy

### Unit Tests

Every new package gets `_test.go` files. Mock all ports via interfaces.

| Package | What to Test |
|---------|-------------|
| `research/gatherer` | Query generation, fact extraction parsing, failure handling |
| `research/synthesizer` | Report structure, citation generation |
| `secrets/keychain` | Store/retrieve/delete (integration test on macOS) |
| `secrets/scrubber` | Credential patterns are scrubbed from all string types |
| `memory/diversity` | Re-ranking produces diverse results |
| `memory/flush` | Critical facts extracted from conversation |
| `scheduler/queue` | Priority ordering, cancellation |
| `agent/router` | Correct model tier selected for different scenarios |

### Integration Tests

| Test | What It Validates |
|------|-------------------|
| Message → tool → response | Full agent loop with real Anthropic API |
| Deep research end-to-end | Haiku gathers → Sonnet synthesizes → report generated |
| Credentialed fetch | Keychain retrieve → go-rod login → content extraction |
| Concurrent tasks | 3 tasks running simultaneously don't interfere |
| SSE reconnect | Disconnect/reconnect preserves state |

### Security Tests (Sprint 9)

| Test | What It Validates |
|------|-------------------|
| Credential scrubbing | Credentials never appear in any log output |
| SSRF prevention | Private IP ranges blocked in fetch_url |
| Docker escape | Container can't access host filesystem or network |
| Memory poisoning | Adversarial input in memory doesn't cause prompt injection |

---

_Filed: ~/Desktop/Projects/zbot/docs/ZBOT_V2_TECHNICAL_DOCS.md_
