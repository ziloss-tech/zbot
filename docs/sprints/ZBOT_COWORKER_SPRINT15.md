# ZBOT Sprint 15 — Coworker Mission Brief
## Objective: Telegram Gateway — ZBOT in Your Pocket

You are working on ZBOT, a personal AI agent for Jeremy Lerwick (CEO, Ziloss Technologies).
The codebase is at: ~/Desktop/zbot
GitHub: https://github.com/jeremylerwick-max/zbot (private)
GCP Project: ziloss (project number: 203743871797)

Sprint 14 is complete. Proactive scheduling is live.
Your job is to add a Telegram gateway so Jeremy can talk to ZBOT from his phone anywhere.

---

## Current State (Sprint 14 Complete)

- Dual Brain Command Center UI live at http://localhost:18790 ✅
- GPT-4o planner + Claude executor + GPT-4o critic ✅
- Cross-session memory via pgvector ✅
- Workspace file panel ✅
- Proactive scheduling ✅
- Skills: search, memory, GHL, GitHub, Google Sheets, Email ✅
- Slack gateway: full feature, desktop-only ✅
- BUT: no mobile access — Slack on phone is clunky ❌
- No way to talk to ZBOT away from desk ❌

---

## Architecture Overview

```
Telegram Bot (Long Polling)
    ↓
internal/gateway/telegram.go   — NEW: message receive + send
    ↓
internal/agent/agent.go        — same agent loop used by Slack
    ↓
Claude responds
    ↓
Telegram sends reply (text + file if applicable)

For plan: commands:
Telegram → planner → orchestrator → result → Telegram message

Security: allowlist by Telegram user ID (GCP Secret: zbot-telegram-user-id)
```

---

## Sprint 15 Tasks — Complete ALL in Order

### PHASE 1: Telegram Bot Setup

File: `internal/gateway/telegram.go` (NEW)

Use the `go-telegram-bot-api` library:
```bash
go get github.com/go-telegram-bot-api/telegram-bot-api/v5
```

**TelegramGateway struct:**
```go
type TelegramGateway struct {
    bot         *tgbotapi.BotAPI
    allowedID   int64        // Jeremy's Telegram user ID
    agent       *agent.Agent
    planner     planner.Planner
    workflow    workflow.Orchestrator
    memory      agent.MemoryStore
    logger      *slog.Logger
    sessions    map[int64]*agent.Session  // per-chat session history
    mu          sync.Mutex
}
```

**Token:** GCP Secret Manager key: `zbot-telegram-token`
**Allowed user ID:** GCP Secret Manager key: `zbot-telegram-user-id`

**Start() method:**
1. Connect via Long Polling (not webhook — simpler, works behind NAT)
2. Log: "Telegram gateway connected — @{botname}"
3. Start receiving updates in a goroutine

**Security — CRITICAL:**
- Every incoming message: check update.Message.From.ID == allowedID
- If not allowed: do NOT respond. Log: "telegram: blocked message from unknown user {id}"
- This prevents anyone who finds the bot from using it

**Checkpoint:** Build passes. ZBOT logs "Telegram gateway connected".

---

### PHASE 2: Message Handling

File: `internal/gateway/telegram.go` (continued)

Handle incoming messages identically to Slack:

**Plain messages → agent:**
```
User: "what time is it in Tokyo?"
ZBOT: "It's 11:45 PM in Tokyo (JST, UTC+9)"
```

**plan: prefix → dual-brain workflow:**
```
User: "plan: research the top 3 CRM competitors and write a comparison"
ZBOT: "⚡ Planning your task..."
     [GPT-4o decomposes]
     "✅ Plan ready: 4 tasks. Executing now..."
     [Claude executes]
     "✅ Done! Report saved: reports/crm_comparison.md"
     [sends file]
```

**schedule: prefix → create scheduled job:**
```
User: "schedule: every morning at 8am research AI news"
ZBOT: "⏰ Scheduled: 'research AI news' — will run every morning at 8:00 AM"
```

**Commands:**
```
/start  — "Hi Jeremy! I'm ZBOT. Send me anything."
/help   — list of commands and examples
/status — current running workflows + scheduled jobs count
/memory — show last 5 memories saved
/files  — list last 5 files in workspace
/cancel {workflow_id} — cancel a running workflow
```

**Message formatting:**
- Telegram supports Markdown — use *bold*, `code`, _italic_
- Max message length: 4096 chars — truncate long outputs with "... [truncated, full file in workspace]"
- Send typing indicator (bot.Send(tgbotapi.NewChatAction(chatID, tgbotapi.ChatTyping))) while processing

**Checkpoint:** Send "hello" to bot from phone → ZBOT responds.

---

### PHASE 3: File Delivery

File: `internal/gateway/telegram.go` (continued)

When ZBOT creates a file during a workflow, send it to Telegram:

**Small files (< 50MB):**
- After workflow completes and a file was created in workspace:
- Send as Telegram document: `bot.Send(tgbotapi.NewDocument(chatID, tgbotapi.FilePath(absPath)))`
- Caption: "📄 {filename} • {size}"

**Large files (> 50MB):**
- Don't send — just note: "📄 File too large for Telegram: {filename} ({size}). View at http://localhost:18790"

**Images (.png, .jpg, .gif):**
- Send as photo: `tgbotapi.NewPhoto(chatID, tgbotapi.FilePath(absPath))`

**Checkpoint:** Run "plan: write a haiku about AI and save it to a file" → Telegram receives the .txt file.

---

### PHASE 4: Workflow Status Updates

File: `internal/gateway/telegram.go` (continued)

For long-running workflows, send progress updates to Telegram:

```
User: "plan: research 10 AI companies and build a comparison matrix"

ZBOT: "⚡ Planning with GPT-4o..."
ZBOT: "✅ Plan: 12 tasks across 3 phases. Starting execution..."
ZBOT: "⏳ Phase 1: Researching companies... (3/12 tasks done)"
ZBOT: "⏳ Phase 2: Comparing features... (8/12 tasks done)"
ZBOT: "✅ Complete! 12/12 tasks done in 3m 42s"
     [sends comparison_matrix.md as document]
```

Progress update rules:
- Send initial "Planning..." message immediately
- Send "Plan ready, executing..." when planner completes
- Send progress update every 60 seconds for long workflows (don't spam)
- Send completion message when done
- If workflow fails: "❌ Workflow failed at task {name}: {error}"

For scheduled jobs that fire while Jeremy is away:
- Send Slack notification (existing) AND Telegram message
- Telegram gets the shorter version: key result + file if created

**Checkpoint:** Run a 5-task plan from Telegram. See progress updates arrive.

---

### PHASE 5: Wire Into wire.go

File: `cmd/zbot/wire.go`

```go
// After Slack gateway setup, add Telegram:
if telegramToken, err := sm.Get(ctx, "zbot-telegram-token"); err == nil && telegramToken != "" {
    telegramUserID, _ := sm.Get(ctx, "zbot-telegram-user-id")
    userID, _ := strconv.ParseInt(telegramUserID, 10, 64)
    
    telegramGW := telegram.NewGateway(
        telegramToken,
        userID,
        coreAgent,
        plannerInstance,
        orchestratorInstance,
        memStore,
        logger,
    )
    go telegramGW.Start(ctx)
    logger.Info("Telegram gateway connecting...")
} else {
    logger.Warn("Telegram gateway skipped — zbot-telegram-token not available")
}
```

Add to startup log banner:
```
skills: [search, memory, ghl, github, sheets, email]
gateways: [slack ✓, telegram ✓]
```

**Checkpoint:** ZBOT starts and logs "Telegram gateway connected — @{botname}" and "Slack Socket Mode connected ✓" together.

---

## Definition of Done

1. `go build ./...` passes clean
2. Telegram token stored in GCP Secret Manager as "zbot-telegram-token"
3. Telegram user ID stored as "zbot-telegram-user-id"
4. ZBOT logs "Telegram gateway connected" at startup
5. Security: messages from unknown users are silently ignored
6. Plain chat messages work from Telegram
7. "plan:" prefix triggers dual-brain workflow with progress updates
8. "schedule:" prefix creates scheduled job with confirmation
9. /status, /help, /memory, /files commands work
10. Files created by workflows are sent to Telegram automatically
11. Manual test from phone: send "plan: research what ZBOT is and write a 3-sentence summary" → progress messages arrive → .txt file sent to Telegram

---

## Final Git Commit

```bash
cd ~/Desktop/zbot
git add -A
git commit -m "Sprint 15: Telegram gateway — ZBOT in your pocket

- internal/gateway/telegram.go: Long Poll bot, allowlist security
- Full message handling: plain chat, plan:, schedule:, commands
- Progress updates for long-running workflows
- File delivery: documents sent after workflow completion
- /start, /help, /status, /memory, /files, /cancel commands
- wire.go: Telegram gateway wired alongside Slack
- GCP secrets: zbot-telegram-token, zbot-telegram-user-id"

git tag -a v1.15.0 -m "Sprint 15: Telegram gateway"
git push origin main
git push origin v1.15.0
```

---

## Important Notes

- Use Long Polling (not webhook) — simpler, no public URL required, works from Mac Studio behind NAT
- NEVER respond to messages from users other than the allowedID — this is a personal agent, not a public bot
- go-telegram-bot-api/v5 is the library — it's well-maintained and idiomatic
- Session management: each Telegram chat ID gets its own session (like Slack does per channel)
- Typing indicator: always send ChatTyping action before any processing > 1 second
- go build ./... must pass after every phase before moving to the next
- All secrets come from GCP Secret Manager — never hardcode credentials
- To get Jeremy's Telegram user ID: send any message to @userinfobot on Telegram
