# ZBOT Sprint 1 — Coworker Mission Brief
## Objective: Wire Real Claude AI Into ZBOT So It Actually Thinks and Responds

You are working on ZBOT, a personal AI agent for Jeremy Lerwick (CEO, Ziloss Technologies).
The codebase is at: ~/Desktop/zbot
GitHub: https://github.com/jeremylerwick-max/zbot (private)
GCP Project: ziloss (project number: 203743871797)

ZBOT is currently running and connected to Slack via Socket Mode. When Jeremy DMs the ZBOT
app in Slack, it currently echoes messages back (Sprint 0 stub). Your job is to replace
that echo stub with a REAL Claude-powered agent that can think, use tools, and respond
intelligently.

---

## Current State (Sprint 0 Complete)

### What exists and WORKS:
- Slack Socket Mode gateway: ~/Desktop/zbot/internal/gateway/slack.go ✅
- Core agent loop scaffold: ~/Desktop/zbot/internal/agent/agent.go ✅
- All domain interfaces (ports): ~/Desktop/zbot/internal/agent/ports.go ✅
- GCP Secret Manager adapter: ~/Desktop/zbot/internal/secrets/gcp.go ✅
- Tool scaffolds: web.go, coderunner.go, filetools.go in internal/tools/ ✅
- Workflow orchestrator scaffold: ~/Desktop/zbot/internal/workflow/orchestrator.go ✅
- Memory store scaffold: ~/Desktop/zbot/internal/memory/store.go ✅
- Wire/boot file: ~/Desktop/zbot/cmd/zbot/wire.go (currently has echo stub) ✅
- go build ./... passes clean ✅

### GCP Secrets already stored (use these exact names):
- "ANTHROPIC_API_KEY" → Anthropic Claude API key
- "openai-api-key" → OpenAI fallback
- "brave-search-api-key" → Brave Search API for web search tool
- "database-url" → postgresql+asyncpg://ziloss:ZilossMemory2024!@/ziloss_memory?host=/cloudsql/ziloss:us-central1:ziloss-postgres
- "zbot-slack-token" → Slack bot token (xoxb-)
- "zbot-slack-app-token" → Slack app token (xapp-)

### Key architecture decisions (DO NOT change these):
- Hexagonal architecture: core domain knows NOTHING about external services
- All external dependencies injected as interfaces defined in ports.go
- No ORM — use pgx/v5 directly with raw SQL
- All secrets via GCP Secret Manager ONLY, never env vars or config files
- Docker sandboxing for code execution
- go build ./... must always pass before committing

---

## Sprint 1 Tasks — Complete ALL of These

### TASK 1: Implement the Anthropic LLM Client

Create ~/Desktop/zbot/internal/llm/anthropic.go

Implement the LLMClient interface from ports.go:
```go
type LLMClient interface {
    Complete(ctx context.Context, msgs []Message, tools []ToolDefinition) (ToolCalls []ToolCall, Reply string, err error)
    CompleteStream(ctx context.Context, msgs []Message, tools []ToolDefinition, out chan<- string) error
    ModelName() string
}
```

Requirements:
- Use the official Anthropic Go SDK: github.com/anthropics/anthropic-sdk-go
- Default model: claude-sonnet-4-6 (model string: "claude-sonnet-4-6")
- Support tool use (function calling) — this is critical for the agent loop
- Map ZBOT's internal Message and ToolDefinition types to Anthropic SDK types
- Handle tool_use content blocks from Claude's response and return them as ToolCall structs
- Max tokens: 8192 default
- Add a ModelRouter struct that picks model based on task:
  - Default: claude-sonnet-4-6
  - If message contains "/think" flag: claude-opus-4-6 (model string: "claude-opus-4-6")
  - If task is classified as "simple" (short, no tools needed): claude-haiku-4-5-20251001
- The API key comes in as a constructor parameter (already retrieved from GCP Secret Manager by wire.go)

### TASK 2: Implement pgvector Memory Store

Create ~/Desktop/zbot/internal/memory/pgvector.go (the scaffold at store.go has the interface, implement it for real)

The existing database-url secret uses Cloud SQL with asyncpg driver notation. For pgx/v5 you need to convert it.
The actual postgres connection string should be:
  host=/cloudsql/ziloss:us-central1:ziloss-postgres dbname=ziloss_memory user=ziloss password=ZilossMemory2024! sslmode=disable

BUT for local development (running on Mac Studio, not Cloud Run), use TCP instead:
  Check if the Cloud SQL proxy is running. If not, try connecting directly.
  Jeremy's pgvector instance is at: 34.28.163.109 (this is the Cloud SQL instance public IP)
  So for local dev: postgresql://ziloss:ZilossMemory2024!@34.28.163.109:5432/ziloss_memory?sslmode=disable

The memory table already exists (from the existing mem0 system):
  Table: mem0_memories_vertex (768-dim vectors, Vertex AI embeddings)
  BUT create a NEW table for ZBOT: zbot_memories
  Schema:
  ```sql
  CREATE TABLE IF NOT EXISTS zbot_memories (
      id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
      session_id TEXT NOT NULL,
      content TEXT NOT NULL,
      embedding vector(768),
      metadata JSONB DEFAULT '{}',
      created_at TIMESTAMPTZ DEFAULT NOW(),
      updated_at TIMESTAMPTZ DEFAULT NOW()
  );
  CREATE INDEX IF NOT EXISTS zbot_memories_embedding_idx 
      ON zbot_memories USING hnsw (embedding vector_cosine_ops)
      WITH (m = 16, ef_construction = 64);
  CREATE INDEX IF NOT EXISTS zbot_memories_session_idx ON zbot_memories(session_id);
  CREATE INDEX IF NOT EXISTS zbot_memories_fts_idx ON zbot_memories USING gin(to_tsvector('english', content));
  ```

For embeddings, use Vertex AI text-embedding-004 model (768 dims).
Use the Google Cloud AI Platform Go SDK: cloud.google.com/go/aiplatform/apiv1
Project: ziloss, Location: us-central1, Model: text-embedding-004

Implement MemoryStore interface:
- Save(ctx, sessionID, content string, metadata map[string]any) error
  → generate embedding → insert into zbot_memories
- Search(ctx, query string, limit int) ([]Fact, error)
  → generate embedding for query → hybrid search: 70% vector cosine + 30% BM25 full-text + time decay
  → time decay formula: score * exp(-0.01 * days_since_created)
- Delete(ctx, id string) error

If Vertex AI embedding fails, fall back to a simple keyword-based search (no embedding).
If the database connection fails entirely, use an in-memory fallback (sync.Map) so ZBOT still works.

### TASK 3: Wire All Real Tools

The tool scaffolds exist. Make them actually work:

**internal/tools/web.go** — WebSearchTool and FetchURLTool:
- WebSearchTool.Execute(): Call Brave Search API
  - Endpoint: https://api.search.brave.com/res/v1/web/search
  - Header: X-Subscription-Token: {brave_api_key}
  - Return top 5 results formatted as markdown with title, URL, snippet
  - Brave API key comes in as constructor parameter
- FetchURLTool.Execute(): HTTP GET with SSRF protection
  - Block: localhost, 127.x.x.x, 169.254.x.x, 10.x.x.x, 172.16-31.x.x, 192.168.x.x
  - 30s timeout
  - Truncate response at 50KB
  - Strip HTML tags, return readable text
  - Use golang.org/x/net/html for HTML parsing if available, otherwise regex strip

**internal/tools/coderunner.go** — CodeRunnerTool:
- The scaffold already has Docker logic. Make sure it compiles and handles errors gracefully.
- If Docker is not available, return a helpful error message rather than panicking.
- Supported languages: python3, go, node, bash
- Security flags must be enforced: --rm --network=none --memory=512m --cpus=1 --read-only --tmpfs /tmp:size=100m --user=1000:1000

**internal/tools/filetools.go** — ReadFileTool, WriteFileTool, MemorySaveTool:
- These are mostly scaffolded. Make sure safePath() works correctly (no path traversal).
- Workspace root: ~/zbot-workspace (create if doesn't exist)
- MemorySaveTool should call the MemoryStore.Save() — it needs MemoryStore injected

### TASK 4: Replace Echo Stub With Real Agent

Update ~/Desktop/zbot/cmd/zbot/wire.go:

Replace the echo handler with a real agent.Run() call:

```go
// Real agent setup:
// 1. Init GCP secrets manager ✅ (already done)
// 2. Get all API keys from GCP ✅ (already done for slack tokens)
// 3. Get ANTHROPIC_API_KEY from GCP secrets
// 4. Get brave-search-api-key from GCP secrets  
// 5. Connect to postgres (use TCP to 34.28.163.109 for local dev)
// 6. Create memory store
// 7. Create LLM client (Anthropic)
// 8. Create tools (web search, file, code runner, memory save)
// 9. Create agent with all dependencies
// 10. Wire handler: msg → agent.Run() → reply
```

The handler function signature is:
```go
handler := func(ctx context.Context, sessionID, userID, text string) (string, error) {
    // Build TurnInput
    // Call agent.Run()
    // Return the reply string
}
```

For conversation history: keep a simple in-memory map[string][]Message keyed by sessionID.
This resets on restart which is fine for Sprint 1. Sprint 2 will add persistence.

### TASK 5: System Prompt

Wire in this exact system prompt for ZBOT:

```
You are ZBOT, a personal AI agent built exclusively for Jeremy Lerwick, CEO and founder of Ziloss Technologies in Salt Lake City, Utah.

You are NOT a general assistant. You are Jeremy's dedicated agent with full context about his businesses and goals.

ABOUT JEREMY'S BUSINESSES:
- Lead Certain: Performance-based lead nurturing service, $200K/month revenue, 75% margins
- Ziloss CRM: SaaS platform under development (GoHighLevel competitor with AI-native features)
- Real estate automation for his mother Deborah Boler's brokerage in Midland, Texas
- Various AI agent orchestration and automation systems

YOUR CAPABILITIES:
- web_search: Search the internet for current information
- fetch_url: Fetch and read the content of any URL
- read_file: Read files from Jeremy's workspace
- write_file: Write files to Jeremy's workspace  
- run_code: Execute Python, Go, JavaScript, or bash code in a secure sandbox
- save_memory: Save important facts to your long-term memory

YOUR PERSONALITY:
- Direct and efficient — no fluff, no unnecessary caveats
- Action-oriented — when in doubt, do it and report back
- Technically sophisticated — Jeremy is a senior developer, don't over-explain basics
- Honest about limitations and uncertainty
- Proactive — if you notice something important while doing a task, mention it

INSTRUCTIONS:
- Always use your tools when they would help — don't just answer from memory when you could verify with web_search
- For multi-step tasks, work through them systematically and report progress
- Save important information to memory using save_memory so you remember it next time
- Keep responses concise unless detail is specifically needed
- If a task will take multiple steps, briefly outline your plan before starting
- Markdown formatting is fine — Slack renders it
```

### TASK 6: Test End-to-End

After all tasks are complete:

1. Run: cd ~/Desktop/zbot && go build ./... 
   - Must pass with ZERO errors or warnings

2. Run: cd ~/Desktop/zbot && go run ./cmd/zbot
   - Should see: "Slack Socket Mode connected ✓"
   - Should NOT see any error about missing secrets or connection failures

3. Leave ZBOT running. When Jeremy returns he will DM "hello" to ZBOT in Slack
   and expects a real, intelligent response from Claude — not an echo.

### TASK 7: Git Commit

Once everything works:

```bash
cd ~/Desktop/zbot
git add -A
git commit -m "Sprint 1: Real Claude agent live — LLM client, memory, tools, full pipeline wired

- internal/llm/anthropic.go: Anthropic SDK client with tool use support
- internal/memory/pgvector.go: pgvector + Vertex AI embeddings + BM25 hybrid search  
- internal/tools/web.go: Brave Search + URL fetch with SSRF protection
- internal/tools/coderunner.go: Docker sandboxed code execution
- internal/tools/filetools.go: Safe file read/write + memory save tool
- cmd/zbot/wire.go: Full dependency injection, echo stub replaced with agent.Run()
- ZBOT now thinks with claude-sonnet-4-6 and responds intelligently in Slack"
git push origin main
```

---

## Important Notes

- **Never put secrets in code or config files.** All secrets via GCP Secret Manager only.
- **Never break the build.** go build ./... must pass after every change.
- **Hexagonal architecture.** The agent package must never import from llm, gateway, or tools directly — only through interfaces.
- **Error handling.** Every error must be wrapped with fmt.Errorf("context: %w", err). Never swallow errors.
- **Graceful degradation.** If memory store fails, ZBOT should still work (just without memory). If a tool fails, return the error as a tool result, don't crash.
- **The workspace root** for file tools is ~/zbot-workspace — create it if it doesn't exist.
- **Jeremy's Slack user ID** for the allowlist — check by looking at any message event that comes through or set allowedUsers to empty slice for now (open to all users in workspace, fine for personal tool).

## Go Dependencies You'll Need

Run these to install:
```bash
cd ~/Desktop/zbot
go get github.com/anthropics/anthropic-sdk-go
go get cloud.google.com/go/aiplatform/apiv1
go get github.com/jackc/pgx/v5/pgxpool  # already installed
go get golang.org/x/net/html
```

## Success Criteria

When done, Jeremy should be able to:
1. Open Slack, DM ZBOT: "what's the weather in salt lake city"
2. ZBOT uses web_search tool, finds the answer, responds with real weather info
3. DM ZBOT: "write me a python script that prints fibonacci numbers"  
4. ZBOT uses run_code tool, executes it, returns the output
5. DM ZBOT: "remember that my mac studio has 512GB RAM"
6. ZBOT saves it to memory and confirms

That's Sprint 1 done. Good luck — Jeremy is counting on you.
