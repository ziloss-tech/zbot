# ZBOT Sprint 4 — Coworker Mission Brief
## Objective: Scraper + Anti-Block Layer

You are working on ZBOT, a personal AI agent for Jeremy Lerwick (CEO, Ziloss Technologies).
The codebase is at: ~/Desktop/zbot
GitHub: https://github.com/jeremylerwick-max/zbot (private)
GCP Project: ziloss (project number: 203743871797)

ZBOT can see images and analyze PDFs. Sprint 3 is done.
Your job is to make ZBOT scrape any website reliably — proxy rotation, JS rendering, anti-block.

---

## Current State (Sprint 3 Complete)

- Real Claude responses via Slack ✅
- Cross-session memory via pgvector ✅
- Vision: images + PDF analysis ✅
- go build ./... passes clean ✅

---

## Sprint 4 Tasks — Complete ALL of These

### TASK 1: Proxy Pool Client

Create: internal/scraper/proxy.go

```go
package scraper

import (
    "net/http"
    "net/url"
    "sync/atomic"
)

// ProxyPool rotates through a list of proxies round-robin.
// If no proxies configured, falls back to direct connection.
type ProxyPool struct {
    proxies []*url.URL
    idx     atomic.Uint64
}

func NewProxyPool(proxyURLs []string) *ProxyPool

// Next returns the next proxy in rotation, or nil for direct.
func (p *ProxyPool) Next() *url.URL

// NewHTTPClient returns an http.Client configured with the next proxy.
func (p *ProxyPool) NewHTTPClient(timeout time.Duration) *http.Client
```

Proxy URL format: http://user:pass@host:port or socks5://host:port
Store proxy list in GCP Secret Manager as "zbot-proxy-list" (newline-separated).
If secret doesn't exist or is empty, ProxyPool operates in direct mode (no proxy).

### TASK 2: User-Agent Rotation

Create: internal/scraper/useragent.go

```go
// Pool of realistic browser user agents — updated to 2025 versions.
var userAgents = []string{
    "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
    "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
    "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
    "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15",
    // add 10+ more realistic agents
}

func RandomUserAgent() string
```

Apply random user agent to every outbound HTTP request in the scraper.

### TASK 3: Per-Domain Rate Limiter

Create: internal/scraper/ratelimit.go

```go
// DomainRateLimiter enforces per-domain request rate limits.
// Default: 1 request per 2 seconds per domain.
// Prevents hammering any single site.
type DomainRateLimiter struct {
    mu      sync.Mutex
    buckets map[string]*rateBucket
    defaultDelay time.Duration
}

func NewDomainRateLimiter(defaultDelay time.Duration) *DomainRateLimiter

// Wait blocks until it's safe to make a request to the given domain.
func (r *DomainRateLimiter) Wait(ctx context.Context, domain string) error
```

### TASK 4: Domain Blocklist

Create: internal/scraper/blocklist.go

```go
// Hardcoded blocklist — never scrape these.
var hardBlocklist = []string{
    "facebook.com", "instagram.com", // login walls
    "localhost", "127.0.0.1",        // SSRF
    "169.254.",                       // AWS metadata
}

// IsBlocked checks if a URL's domain is on the blocklist.
func IsBlocked(rawURL string) bool
```

### TASK 5: Retry Logic with Exponential Backoff

Create: internal/scraper/retry.go

```go
// Retry executes fn up to maxAttempts times.
// Retries on: 429 (rate limit), 503 (unavailable), network errors.
// Does NOT retry on: 404, 403, 401 (permanent failures).
// Backoff: 1s, 2s, 4s, 8s... with ±20% jitter.
func Retry(ctx context.Context, maxAttempts int, fn func() (*http.Response, error)) (*http.Response, error)
```

### TASK 6: SQLite Scrape Cache

Create: internal/scraper/cache.go

Use: modernc.org/sqlite (pure Go SQLite, no CGO)

```go
// ScrapeCache caches URL responses for 24 hours.
// Prevents redundant fetches within a session.
type ScrapeCache struct {
    db *sql.DB
}

// Schema:
// CREATE TABLE scrape_cache (
//     url TEXT PRIMARY KEY,
//     content TEXT NOT NULL,
//     fetched_at TIMESTAMPTZ NOT NULL
// );

func NewScrapeCache(dbPath string) (*ScrapeCache, error)
func (c *ScrapeCache) Get(url string) (content string, found bool)
func (c *ScrapeCache) Set(url string, content string) error
func (c *ScrapeCache) Prune() error // delete entries older than 24 hours
```

Cache DB path: ~/zbot-workspace/.scrape-cache.db

### TASK 7: go-rod Headless Browser for JS Sites

Create: internal/scraper/browser.go

Use: github.com/go-rod/rod

```go
// BrowserFetcher fetches JavaScript-heavy pages using headless Chromium.
// Falls back gracefully if Chromium is not installed.
type BrowserFetcher struct {
    pool *rod.BrowserPool
}

func NewBrowserFetcher() (*BrowserFetcher, error)

// Fetch loads the URL in a headless browser and returns the rendered HTML.
// Waits for network idle (no requests for 500ms) before extracting content.
// Timeout: 30 seconds.
func (f *BrowserFetcher) Fetch(ctx context.Context, rawURL string) (string, error)
```

### TASK 8: HTML → Clean Text Extraction

Create: internal/scraper/extract.go

```go
// ExtractText strips HTML and extracts readable content.
// Removes: scripts, styles, nav, header, footer, ads.
// Returns clean article text suitable for the LLM context.
func ExtractText(html string) string
```

Use golang.org/x/net/html for parsing.
Target: extract the main content div, not boilerplate navigation.

### TASK 9: Wire into Upgraded fetch_url Tool

Update internal/tools/web.go → FetchURLTool:

Replace the basic http.Get with the full scraper stack:
1. Check blocklist — reject if blocked
2. Check cache — return cached content if fresh
3. Apply domain rate limiter — wait if needed
4. Try simple HTTP fetch with random user agent + proxy
5. On JS-detection (empty body or React placeholder) → retry with BrowserFetcher
6. On 429/503 → exponential backoff retry
7. Extract clean text
8. Cache the result

```go
type FetchURLTool struct {
    proxyPool   *scraper.ProxyPool
    rateLimiter *scraper.DomainRateLimiter
    cache       *scraper.ScrapeCache
    browser     *scraper.BrowserFetcher
}
```

---

## Definition of Done

1. Ask ZBOT: "research 10 competitor websites for GoHighLevel and summarize each"
2. ZBOT fetches all 10 with zero blocks, returns clean summaries.
3. Retry logic tested: manually return 429 from a test server → ZBOT retries with backoff.
4. Cache tested: fetch same URL twice → second fetch returns instantly from cache.
5. go build ./... passes clean.

---

## Go Dependencies

```bash
cd ~/Desktop/zbot
go get modernc.org/sqlite
go get github.com/go-rod/rod
go get golang.org/x/net/html
```

---

## Git Commit

```bash
cd ~/Desktop/zbot
git add -A
git commit -m "Sprint 4: Anti-block scraper — proxy rotation, rate limiting, cache, headless browser

- internal/scraper/proxy.go: Rotating proxy pool
- internal/scraper/useragent.go: Realistic browser user agent rotation
- internal/scraper/ratelimit.go: Per-domain token bucket rate limiter
- internal/scraper/blocklist.go: Domain blocklist (hardcoded + SSRF prevention)
- internal/scraper/retry.go: Exponential backoff on 429/503
- internal/scraper/cache.go: SQLite 24-hour scrape cache
- internal/scraper/browser.go: go-rod headless Chromium for JS sites
- internal/scraper/extract.go: HTML → clean text extraction
- internal/tools/web.go: FetchURLTool upgraded with full scraper stack"
git push origin main
```

## Important Notes

- Never put secrets in code. Proxy list in GCP Secret Manager as "zbot-proxy-list".
- go build ./... must pass after every change.
- If Chromium not installed, BrowserFetcher.Fetch() returns a clear error — do not crash.
- Cache DB is in the workspace, not committed to git (add *.db to .gitignore).
- Always respect IsBlocked() check FIRST before any network request.
