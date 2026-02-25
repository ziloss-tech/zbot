# ZBOT Sprint 10 — Coworker Mission Brief
## Objective: v1.0 Release — Ship It

You are working on ZBOT, a personal AI agent for Jeremy Lerwick (CEO, Ziloss Technologies).
The codebase is at: ~/Desktop/zbot
GitHub: https://github.com/jeremylerwick-max/zbot (private)
GCP Project: ziloss (project number: 203743871797)

ZBOT is security hardened. Sprint 9 is done.
Your job is to polish, document, test, and ship v1.0.

---

## Current State (Sprint 9 Complete)

- Real Claude responses via Slack ✅
- Cross-session memory via pgvector ✅
- Vision: images + PDFs ✅
- Anti-block web scraper ✅
- Multi-step parallel workflow engine ✅
- Cron scheduler + webhooks ✅
- Skills: GHL, GitHub, Google Sheets, Email ✅
- Web UI at http://localhost:18790 ✅
- Real audit logging to Postgres ✅
- govulncheck clean ✅
- Security hardened ✅
- go build ./... passes clean ✅

---

## Sprint 10 Tasks — Complete ALL of These

### TASK 1: Full Integration Test Suite

Create: tests/integration/

Write integration tests for all critical paths. Use Go's testing package with build tag:
```go
//go:build integration
```

Run with: go test -tags=integration ./tests/integration/...

Tests to write:

**tests/integration/agent_test.go**
```go
// TestAgentWebSearch — agent calls web_search and returns a real result
// TestAgentWriteFile — agent writes a file to workspace and it exists
// TestAgentSaveMemory — agent saves a fact, search returns it
// TestAgentCodeRunner — agent runs python code and returns stdout
// TestAgentMultiTurn — 3-message conversation, context preserved
```

**tests/integration/memory_test.go**
```go
// TestMemorySaveAndSearch — save a fact, search returns it ranked #1
// TestMemoryTimeDecay — older facts rank lower than newer ones for same query
// TestMemoryNamespaceIsolation — zbot namespace can't see mem0 namespace
// TestMemoryFallback — when Postgres down, in-memory fallback works
```

**tests/integration/workflow_test.go**
```go
// TestWorkflowCreateAndRun — create 3-task workflow, all tasks complete
// TestWorkflowParallel — independent tasks run concurrently
// TestWorkflowDependency — task waits for dependency to complete
// TestWorkflowCancelation — cancel stops pending tasks
// TestWorkflowRestartResume — kill process mid-workflow, restart picks up
```

**tests/integration/skills_test.go**
```go
// TestGHLGetContacts — fetches real GHL contacts (uses test location)
// TestGitHubListIssues — fetches real GitHub issues from zbot repo
// TestSheetsRead — reads from a test spreadsheet
```

### TASK 2: Performance Profiling

Create: cmd/profile/main.go

Run the agent through 100 turns and collect:
- CPU profile: go tool pprof
- Memory profile: heap snapshot before/after
- Goroutine count before/after (check for leaks)

```bash
go test -cpuprofile=cpu.prof -memprofile=mem.prof -bench=. ./tests/bench/
go tool pprof cpu.prof
```

Create: tests/bench/agent_bench_test.go
```go
func BenchmarkAgentSingleTurn(b *testing.B)
func BenchmarkMemorySearch(b *testing.B)
func BenchmarkWorkflowClaim(b *testing.B)
```

Document findings in: PERFORMANCE.md
Target baselines:
- Single agent turn (no tools): < 3s P95
- Memory search (pgvector): < 200ms P95
- Workflow task claim: < 50ms P95

### TASK 3: Deployment Guide

Create: docs/DEPLOYMENT.md

Document how to run ZBOT:

**Mac Studio (Local — Primary)**
```bash
# Prerequisites
brew install go poppler
# Install go-rod Chromium
go run github.com/go-rod/rod/lib/launcher/main@latest

# Clone and build
git clone https://github.com/jeremylerwick-max/zbot
cd zbot
go build -o zbot ./cmd/zbot

# Set up GCP credentials
gcloud auth application-default login

# Run
./zbot
# Access web UI at http://localhost:18790
```

**Cloud Run (Optional — Remote)**
Document how to build a Docker image and deploy to Cloud Run if needed.
Include: Dockerfile, cloudbuild.yaml, and the gcloud run deploy command.

**Secrets Setup**
List every GCP Secret Manager secret ZBOT needs with the exact secret name and format:
- zbot-slack-token
- zbot-slack-app-token
- anthropic-api-key
- brave-api-key
- ghl-api-key
- github-token
- google-sheets-credentials
- smtp-host, smtp-port, smtp-user, smtp-pass, smtp-from
- zbot-webhook-secret
- zbot-allowed-user-id
- zbot-proxy-list (optional)

### TASK 4: Runbook

Create: docs/RUNBOOK.md

**Startup**
```bash
cd ~/Desktop/zbot
./zbot  # or: go run ./cmd/zbot
# Verify: "Slack Socket Mode connected ✓"
# Verify: "postgres connected"
# Verify: "memory schema ready"
```

**Restart**
```bash
# Find PID:
pgrep zbot
# Kill gracefully (SIGTERM):
kill <pid>
# Restart:
./zbot
# Pending workflow tasks auto-resume on boot.
# Scheduled jobs auto-reload from DB.
```

**Upgrade**
```bash
git pull origin main
go build -o zbot ./cmd/zbot
kill $(pgrep zbot)
./zbot
```

**Rollback**
```bash
git log --oneline -10  # find last good commit
git checkout <commit-hash>
go build -o zbot ./cmd/zbot
./zbot
```

**Backup Memories**
```bash
# Export all memories to JSON:
./memcli list --limit 9999 --format json > memories_backup_$(date +%Y%m%d).json

# Or pg_dump:
pg_dump -h 34.28.163.109 -U ziloss ziloss_memory -t zbot_memories > memories_$(date +%Y%m%d).sql
```

**Common Issues**
- "Slack Socket Mode failed": check zbot-slack-token and zbot-slack-app-token in Secret Manager
- "postgres connection refused": check Cloud SQL is running, IP is 34.28.163.109
- "vertex AI embedder unavailable": check GCP credentials with `gcloud auth application-default print-access-token`
- ZBOT not responding in Slack: check if zbot-allowed-user-id matches your actual Slack user ID

### TASK 5: Update README.md

Rewrite ~/Desktop/zbot/README.md with:
- What ZBOT is and does (complete feature list)
- Architecture diagram (ASCII)
- Quick start instructions (2-3 commands to get running)
- Link to DEPLOYMENT.md and RUNBOOK.md
- Sprint completion status table (all 10 sprints ✅)
- Current capabilities list
- How to add a new skill (2-paragraph summary)

### TASK 6: Update MASTER_PROJECT_INFO.md

File: ~/Desktop/MASTER_PROJECT_INFO.md

Add a ZBOT section with:
- v1.0 go-live date
- All capabilities (bullet list)
- Web UI URL: http://localhost:18790
- GCP Project: ziloss
- Postgres: 34.28.163.109, DB: ziloss_memory
- Skills active: GHL (Location: fRrP1e3LGLFewc5dQDhS), GitHub, Google Sheets, Email
- Workspace: ~/zbot-workspace
- memcli location: ~/Desktop/zbot/memcli

### TASK 7: v1.0 Tag + Release Notes

Create: CHANGELOG.md

```markdown
# CHANGELOG

## v1.0.0 — 2026-05-11

### What's New
Complete personal AI agent platform for Jeremy Lerwick.

### Capabilities
- **Slack interface**: DM ZBOT for instant AI responses
- **Long-term memory**: pgvector semantic memory across sessions
- **Vision**: Analyze images and PDFs sent via Slack
- **Web research**: Brave Search + anti-block scraper with headless Chromium
- **Code execution**: Python, Go, Node, bash in Docker sandbox
- **Workflows**: Multi-step parallel task execution, survives restarts
- **Scheduling**: Natural language cron — "every Monday at 9am do X"
- **Webhooks**: External triggers from GHL, Zapier, etc.
- **Skills**: GHL CRM, GitHub, Google Sheets, Email
- **Web UI**: Full audit dashboard at http://localhost:18790
- **Security**: govulncheck clean, user allowlist, injection defense

### Architecture
Go 1.22, Hexagonal Architecture, GCP Cloud SQL pgvector, Vertex AI embeddings,
Claude claude-sonnet-4-6 default / claude-opus-4-6 for /think / claude-haiku-4-5-20251001 for lightweight tasks.
```

Then tag and push:
```bash
git tag -a v1.0.0 -m "ZBOT v1.0.0 — Personal AI agent platform, full feature set"
git push origin v1.0.0
```

---

## Definition of Done

1. `go test -tags=integration ./tests/integration/...` passes — all integration tests green
2. `govulncheck ./...` passes clean
3. `go build ./...` passes clean
4. CI pipeline green on GitHub (all checks pass)
5. README.md is complete and accurate
6. DEPLOYMENT.md has working instructions (test them — actually follow the guide)
7. RUNBOOK.md covers startup, restart, upgrade, rollback, backup
8. MASTER_PROJECT_INFO.md updated with ZBOT go-live
9. v1.0.0 tag pushed to GitHub
10. ZBOT is running, handling real daily tasks from Jeremy in Slack

---

## Final Git Commit

```bash
cd ~/Desktop/zbot
git add -A
git commit -m "Sprint 10: v1.0.0 — Integration tests, profiling, deployment docs, runbook

- tests/integration/: Full integration test suite (agent, memory, workflow, skills)
- tests/bench/: Benchmark suite with documented baselines
- PERFORMANCE.md: Profiling results and baselines
- docs/DEPLOYMENT.md: Mac Studio + Cloud Run deployment guide
- docs/RUNBOOK.md: Startup, restart, upgrade, rollback, backup procedures
- README.md: Rewritten with full feature list and architecture
- CHANGELOG.md: v1.0.0 release notes
- MASTER_PROJECT_INFO.md: ZBOT go-live documented"

git tag -a v1.0.0 -m "ZBOT v1.0.0 — Personal AI agent platform, full feature set"
git push origin main
git push origin v1.0.0
```

## Congratulations

ZBOT is done. v1.0.0 shipped.

Jeremy has a fully autonomous personal AI agent that:
- Thinks with Claude claude-sonnet-4-6 via Slack
- Remembers everything across sessions
- Can see images and read PDFs
- Scrapes any website without getting blocked
- Runs multi-step parallel workflows
- Does things on a schedule without being asked
- Integrates with GHL, GitHub, Google Sheets, and Email
- Has a full audit trail at http://localhost:18790
- Is security hardened and production ready

## Important Notes

- go build ./... must pass after every change.
- Integration tests may need real API credentials — use Jeremy's actual GCP secrets.
- Benchmark baselines in PERFORMANCE.md are targets, not hard failures.
- The Docker image for Cloud Run is optional — Mac Studio is the primary runtime.
- Don't skip the DEPLOYMENT.md testing step — actually follow the guide to verify it works.
