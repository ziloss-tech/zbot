# ZBOT Sprints 2, 3, 4 — Handoff Notes

**Date:** 2026-02-25
**Author:** Claude (Cowork session)
**Status:** All code written — needs dependency install, compile check, and testing

---

## What Was Completed

### Sprint 2 — Memory Persistence (4/4 tasks)

| # | Task | File | Status |
|---|------|------|--------|
| 1 | SearchMemoryTool | `internal/tools/memory.go` | ✓ NEW |
| 2 | AutoSave wiring | `cmd/zbot/wire.go` (line ~222) | ✓ MODIFIED |
| 3 | Namespace isolation | `internal/memory/store.go` | ✓ MODIFIED |
| 4 | memcli CLI | `cmd/memcli/main.go` | ✓ NEW |

**Key changes:**
- `memory.New()` now takes a `namespace` parameter (5th arg) — e.g. `"zbot"` → table `zbot_memories`
- `store.tableName()` replaces all hardcoded `"zbot_memories"` references
- `Store.List()`, `Store.Stats()`, `Store.Count()` added for memcli
- AutoSave runs after every agent turn for replies > 200 chars

### Sprint 3 — Vision + File Analysis (6/6 tasks)

| # | Task | File | Status |
|---|------|------|--------|
| 1 | Slack file download | `internal/gateway/slack.go` | ✓ MODIFIED |
| 2 | Multimodal LLM messages | `internal/llm/anthropic.go` | ✓ MODIFIED |
| 3 | AnalyzeImageTool | `internal/tools/vision.go` | ✓ NEW |
| 4 | PDFExtractTool | `internal/tools/vision.go` | ✓ NEW |
| 5-6 | Wire into agent + system prompt | `cmd/zbot/wire.go` | ✓ MODIFIED |

**Key changes:**
- `gateway.Attachment` struct with `Data []byte`, `MediaType string`, `Filename string`
- `MessageHandler` signature now includes `attachments []Attachment`
- `buildUserParam()` in anthropic.go creates multimodal messages (image blocks + text)
- Images uploaded in Slack → Claude vision automatically
- PDFs uploaded in Slack → pdftotext → text prepended to message

### Sprint 4 — Scraper + Anti-Block (9/9 tasks)

| # | Task | File | Status |
|---|------|------|--------|
| 1 | Proxy pool | `internal/scraper/proxy.go` | ✓ NEW |
| 2 | User-agent rotation | `internal/scraper/useragent.go` | ✓ NEW |
| 3 | Per-domain rate limiter | `internal/scraper/ratelimit.go` | ✓ NEW |
| 4 | Domain blocklist | `internal/scraper/blocklist.go` | ✓ NEW |
| 5 | Retry w/ exponential backoff | `internal/scraper/retry.go` | ✓ NEW |
| 6 | SQLite scrape cache | `internal/scraper/cache.go` | ✓ NEW |
| 7 | Headless browser (go-rod) | `internal/scraper/browser.go` | ✓ NEW |
| 8 | HTML text extraction | `internal/scraper/extract.go` | ✓ NEW |
| 9 | Wire into fetch_url | `internal/tools/web.go` + `wire.go` | ✓ MODIFIED |

**Key changes:**
- `FetchURLTool` now uses full pipeline: blocklist → cache → rate limit → proxy+UA rotation → retry → JS detection → headless fallback → text extraction → cache result
- `NewFetchURLToolFull()` constructor takes the full scraper stack
- Proxy list loaded from GCP Secret Manager (`zbot-proxy-list`), falls back to direct
- Cache stored at `$WORKSPACE/.cache/scrape.db`

---

## What You Need To Do

### 1. Install New Go Dependencies

```bash
cd ~/path/to/zbot

# New direct dependencies for Sprint 4
go get modernc.org/sqlite
go get github.com/go-rod/rod
go get golang.org/x/net

# Tidy up
go mod tidy
```

### 2. Verify Slack Library Compatibility

**⚠️ IMPORTANT:** The `slackevents.MessageEvent.Files` field and `slackevents.File` struct need verification against `slack-go/slack v0.18.0`. Specifically check:

```go
// In internal/gateway/slack.go, line 215:
func (g *SlackGateway) downloadFiles(ctx context.Context, files []slackevents.File) []Attachment {
```

The fields I used are:
- `f.Mimetype` — might be `f.MimeType` (capital T)
- `f.Name` — should be fine
- `f.Size` — should be `int` but might be `int64`
- `f.URLPrivateDownload` — might be `f.URLPrivate` (no "Download" suffix)

**Quick check:** Search the slack-go source for `MessageEvent` and `File`:
```bash
grep -r "type MessageEvent struct" vendor/github.com/slack-go/slack/slackevents/
# or
go doc github.com/slack-go/slack/slackevents MessageEvent
```

If `slackevents.File` doesn't exist or is different, you may need to use `slack.File` from the main package and adjust the handler accordingly. In that case, the `handleEventsAPI` function would need to call `g.client.GetFileInfo()` for each file ID.

### 3. Create GCP Secret (optional — for proxy rotation)

If you want proxy rotation, create a `zbot-proxy-list` secret in GCP Secret Manager:
```bash
# Newline-separated proxy URLs
echo -e "http://user:pass@proxy1.example.com:8080\nhttp://user:pass@proxy2.example.com:8080" | \
  gcloud secrets create zbot-proxy-list --data-file=- --project=ziloss
```

Without this secret, ZBOT still works — it just uses direct connections (no proxy rotation).

### 4. Install System Dependencies

```bash
# For PDF text extraction (Sprint 3)
brew install poppler   # provides pdftotext

# For headless browser JS fallback (Sprint 4)
# go-rod auto-downloads Chromium on first use, OR:
brew install --cask chromium
```

### 5. Compile & Test

```bash
# Should compile clean after go mod tidy
go build ./cmd/zbot/
go build ./cmd/memcli/

# Test memcli (requires Postgres connectivity)
./memcli stats
./memcli list --limit 5
```

### 6. Test Manually in Slack

After deploying:
1. **Memory search:** "Do you remember anything about Lead Certain?"
2. **Image analysis:** Upload a screenshot → "What's in this image?"
3. **PDF handling:** Upload a PDF → "Summarize this document"
4. **Web scraping:** "Fetch the content from https://example.com"

---

## Known Issues / Future Fixes

1. **`slackevents.File` struct** — Field names may not match what I used. This is the #1 thing to verify before compiling. See section 2 above.

2. **`os/exec` import in wire.go** — Used for `extractPDF()`. If you get an "unused import" error, the `extractPDF` function uses `exec.CommandContext` directly. Make sure both `os/exec` and `context` are imported.

3. **`buildUserParam()` in anthropic.go** — Uses the Anthropic SDK's internal types for multimodal messages. If the SDK version updates, the `ContentBlockParamUnion` / `ImageBlockParam` types might change. Pin the SDK version.

4. **go-rod browser lifecycle** — Currently creates a fresh browser instance per fetch. For high-volume use, consider a browser pool. But for ZBOT's single-user use case, this is fine.

5. **AutoSave is aggressive** — Currently saves every reply > 200 chars. Sprint 2 spec mentions "Phase 2 will use LLM classification" — this is still a TODO.

6. **Scrape cache path** — Stored at `$WORKSPACE/.cache/scrape.db`. If workspace is on a network mount or read-only filesystem, this will fail gracefully (caching disabled).

---

## File Inventory (all changes)

### New Files
- `internal/tools/memory.go` — SearchMemoryTool
- `internal/tools/vision.go` — AnalyzeImageTool + PDFExtractTool
- `internal/scraper/proxy.go` — ProxyPool
- `internal/scraper/useragent.go` — RandomUserAgent()
- `internal/scraper/ratelimit.go` — DomainRateLimiter
- `internal/scraper/blocklist.go` — IsBlocked()
- `internal/scraper/retry.go` — Retry()
- `internal/scraper/cache.go` — ScrapeCache (SQLite)
- `internal/scraper/browser.go` — BrowserFetcher (go-rod)
- `internal/scraper/extract.go` — ExtractText()
- `cmd/memcli/main.go` — Memory CLI

### Modified Files
- `cmd/zbot/wire.go` — Full wiring of all new tools + scraper stack
- `internal/memory/store.go` — Namespace isolation + List/Stats/Count
- `internal/gateway/slack.go` — Attachment download + updated handler signature
- `internal/llm/anthropic.go` — Multimodal message support
- `internal/tools/web.go` — Upgraded FetchURLTool with scraper pipeline
