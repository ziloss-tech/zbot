# ZBOT Coworker Sprint — START HERE

## Date: 2026-03-18
## Priority: CRITICAL — Wire the cognitive loop, then build + test

---

## WHAT JUST HAPPENED

We just refactored agent.go to implement a real multi-stage cognitive loop. The code compiles but the cheap LLM (Frontal Lobe + Thalamus verification) is NOT wired yet. That's your first task.

## THE COGNITIVE LOOP (agent.go → Run())

The agent loop is now 5 stages instead of 1:

```
1. FRONTAL LOBE (planTask)     → cheap LLM classifies task, writes execution plan
2. HIPPOCAMPUS (memory.Search) → loads relevant memories (zero LLM cost)  
3. CORTEX (llm.Complete loop)  → executes plan with tools (main cost center)
4. HIPPOCAMPUS (enrichMemory)  → searches memory AGAIN based on what Cortex found
5. THALAMUS (verifyReply)      → Socratic verification of draft reply before user sees it
```

Stages 1, 4, 5 are NEW. They exist in code but stage 1 and 5 need cheapLLM wired.

## YOUR FIRST TASK — Wire cheapLLM

File: `cmd/zbot/wire.go`

The Agent struct has `cheapLLM LLMClient` and `SetCheapLLM(llm LLMClient)`.
You need to call `ag.SetCheapLLM(haikuClient)` after the agent is constructed.

There are two scenarios:
1. **Anthropic mode** (ZBOT_ANTHROPIC_API_KEY set): Create a Haiku client
2. **OpenAI-compat mode** (Ollama/Together/Grok): Reuse the same client (it's already cheap)

Look for where `ag` is constructed (~line 355) and add:

```go
// Wire cheap LLM for cognitive stages (Frontal Lobe + Thalamus verification).
if useOpenAICompat {
    ag.SetCheapLLM(llmClient) // already cheap
} else {
    ag.SetCheapLLM(llm.NewHaikuClient(anthropicKey, logger))
}
```

The `llm.NewHaikuClient` function already exists in `internal/llm/haiku.go`.

## YOUR SECOND TASK — Test the cognitive loop

After wiring, rebuild and test:

```bash
cd ~/Desktop/Projects/zbot
go build -o zbot-bin ./cmd/zbot
pkill -f "zbot-bin"; sleep 2
set -a && source .env && set +a
nohup ./zbot-bin > /tmp/zbot.log 2>&1 &
sleep 3
```

Then test with:
```bash
# Simple chat (should plan as type=chat, verification=none)
curl -s -X POST http://localhost:18790/api/chat/stream \
  -H "Content-Type: application/json" \
  -d '{"message": "What is 2+2?"}' 

# Research task (should plan as type=research, verification=thorough)
curl -s -X POST http://localhost:18790/api/chat/stream \
  -H "Content-Type: application/json" \
  -d '{"message": "Search the web for the latest Rust release notes"}'
```

Check logs for cognitive stages:
```bash
grep "frontal lobe\|thalamus\|hippocampus\|plan_start\|plan_complete\|verify" /tmp/zbot.log
```

You should see:
- `frontal lobe plan` with type/complexity/steps
- `plan_start` and `plan_complete` events
- `verify_start` and `verify_complete` events (for research/complex tasks)
- `thalamus approved` or `thalamus rejected` with confidence score

## KEY FILES

```
internal/agent/agent.go       — The 5-stage Run() loop (DON'T REWRITE — fix bugs only)
internal/agent/cognitive.go   — NEW: planTask, enrichMemory, verifyReply implementations
internal/agent/ports.go       — EventBus interface + new event types (EventPlanStart etc.)
internal/agent/eventbus.go    — In-memory event bus implementation
cmd/zbot/wire.go              — Dependency injection (wire cheapLLM HERE)
internal/llm/haiku.go         — Haiku client constructor
internal/llm/anthropic.go     — Claude client
internal/llm/openaicompat.go  — OpenAI-compatible client (Ollama/Grok/Together)
```

## WHAT THE COGNITIVE STAGES DO

### Frontal Lobe (planTask in cognitive.go)
- Input: user message
- Output: TaskPlan JSON {type, complexity, steps, needs_memory, verification, model_tier}
- Uses: cheapLLM with frontalLobePrompt
- Cost: ~$0.001 per call
- If cheapLLM is nil: returns nil, Cortex handles everything (old behavior)

### Hippocampus Mid-Task (enrichMemory in cognitive.go)
- Input: tool results from Cortex's execution
- Output: additional Fact[] from memory
- Uses: memory.Search (database only, zero LLM)
- Triggers: only when plan.NeedsMemory=true AND tools were invoked
- What it does: extracts topics from tool results, searches memory for related facts
- Injected as a system message: "IMPORTANT - Additional context from memory"

### Thalamus Verification (verifyReply in cognitive.go)  
- Input: user question + plan + evidence + draft reply
- Output: {approved, confidence, issues, suggestion}
- Uses: cheapLLM with thalamusVerifyPrompt (Socratic/Aristotelian logic)
- Cost: ~$0.001 per call
- Triggers: only when plan.Verification != "none" (skips simple chat)
- If rejected: sends suggestion back to Cortex for ONE revision attempt

## AFTER WIRING — The Sprint Tasks

Once the cognitive loop is verified working:

### TASK 3: File tree + code preview panes
See docs/SPRINT_NEXT.md for full details on FileTreePane and CodePreviewPane.

### TASK 4: Fix Thalamus frontend to use real event bus
ThalamusPane.tsx currently fakes events. Wire it to useEventBus hook.

### TASK 5: Update the SSE event strip
The ChatPane live event strip should show cognitive stage events:
- "Frontal Lobe planning..." → "Plan: research (3 steps)"  
- "Thalamus verifying..." → "Approved (92% confidence)"

## DEFINITION OF DONE

1. `go build ./...` passes
2. `go test ./...` passes  
3. Simple chat: Frontal Lobe classifies as "chat", skips verification
4. Research query: Frontal Lobe plans steps, Thalamus verifies the reply
5. Events stream to UI (plan_start, plan_complete, verify_start, verify_complete)
6. Commit and push to public-release branch, remote: public

## DO NOT

- Rewrite agent.go Run() — it works, just wire cheapLLM
- Change the Thalamus backend handler (thalamus_handler.go works)
- Change the streaming chat handler (chat_stream_handler.go works)  
- Remove any existing event emissions
- Add npm dependencies without checking bundle size

## GIT

```bash
cd ~/Desktop/Projects/zbot
git checkout public-release
git push public public-release:main  # remote "public" = ziloss-tech/zbot
```
