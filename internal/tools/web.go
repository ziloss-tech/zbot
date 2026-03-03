// Package tools implements all agent tools.
// Each tool is a self-contained struct implementing agent.Tool.
// Tools are registered at startup and injected into the agent.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jeremylerwick-max/zbot/internal/agent"
	"github.com/jeremylerwick-max/zbot/internal/scraper"
)

// ─── WEB SEARCH ──────────────────────────────────────────────────────────────

// WebSearchTool searches the web via Brave Search API.
// Tier 1 in the research stack — fast, clean, API-based.
type WebSearchTool struct {
	apiKey     string
	httpClient *http.Client
}

func NewWebSearchTool(apiKey string) *WebSearchTool {
	return &WebSearchTool{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

func (t *WebSearchTool) Name() string { return "web_search" }

func (t *WebSearchTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "web_search",
		Description: "Search the web for current information. Returns titles, URLs, and snippets.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "The search query",
				},
				"count": map[string]any{
					"type":        "integer",
					"description": "Number of results to return (1-20, default 10)",
					"default":     10,
				},
			},
			"required": []string{"query"},
		},
	}
}

func (t *WebSearchTool) Execute(ctx context.Context, input map[string]any) (*agent.ToolResult, error) {
	query, _ := input["query"].(string)
	if query == "" {
		return &agent.ToolResult{Content: "error: query is required", IsError: true}, nil
	}

	count := 10
	if c, ok := input["count"].(float64); ok && c > 0 {
		count = int(c)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("https://api.search.brave.com/res/v1/web/search?q=%s&count=%d", urlEncode(query), count),
		nil)
	if err != nil {
		return nil, fmt.Errorf("web_search: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Subscription-Token", t.apiKey)

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("web_search: request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return &agent.ToolResult{
			Content: fmt.Sprintf("search API error %d: %s", resp.StatusCode, body),
			IsError: true,
		}, nil
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 256<<10)) // 256KB limit
	if err != nil {
		return nil, fmt.Errorf("web_search: read body: %w", err)
	}

	// Parse Brave Search response.
	var result braveSearchResult
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("web_search: parse response: %w", err)
	}

	// Format as clean markdown for the model.
	output := fmt.Sprintf("## Search Results for: %q\n\n", query)
	for i, r := range result.Web.Results {
		output += fmt.Sprintf("### %d. %s\n**URL:** %s\n%s\n\n", i+1, r.Title, r.URL, r.Description)
	}
	if len(result.Web.Results) == 0 {
		output += "_No results found._"
	}

	return &agent.ToolResult{Content: output}, nil
}

type braveSearchResult struct {
	Web struct {
		Results []struct {
			Title       string `json:"title"`
			URL         string `json:"url"`
			Description string `json:"description"`
		} `json:"results"`
	} `json:"web"`
}

// ─── URL FETCH (UPGRADED WITH FULL SCRAPER STACK) ─────────────────────────

// FetchURLTool fetches and extracts text content from a URL.
// Uses the full anti-block scraper stack: blocklist → cache → rate limit →
// proxy + user agent rotation → retry → JS fallback → text extraction.
type FetchURLTool struct {
	proxyPool   *scraper.ProxyPool
	rateLimiter *scraper.DomainRateLimiter
	cache       *scraper.ScrapeCache
	browser     *scraper.BrowserFetcher
}

// NewFetchURLTool creates a basic FetchURLTool without the scraper stack.
// Used for backward compatibility. Call NewFetchURLToolFull for the full stack.
func NewFetchURLTool() *FetchURLTool {
	return &FetchURLTool{}
}

// NewFetchURLToolFull creates a FetchURLTool with the full scraper stack.
func NewFetchURLToolFull(
	proxyPool *scraper.ProxyPool,
	rateLimiter *scraper.DomainRateLimiter,
	cache *scraper.ScrapeCache,
	browser *scraper.BrowserFetcher,
) *FetchURLTool {
	return &FetchURLTool{
		proxyPool:   proxyPool,
		rateLimiter: rateLimiter,
		cache:       cache,
		browser:     browser,
	}
}

func (t *FetchURLTool) Name() string { return "fetch_url" }

func (t *FetchURLTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "fetch_url",
		Description: "Fetch and read the text content of a specific URL. Use for reading articles, docs, or any web page. Handles JavaScript-heavy sites automatically.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"url": map[string]any{
					"type":        "string",
					"description": "The URL to fetch",
				},
			},
			"required": []string{"url"},
		},
	}
}

func (t *FetchURLTool) Execute(ctx context.Context, input map[string]any) (*agent.ToolResult, error) {
	rawURL, _ := input["url"].(string)
	if rawURL == "" {
		return &agent.ToolResult{Content: "error: url is required", IsError: true}, nil
	}

	// 1. Check blocklist — reject if blocked.
	if scraper.IsBlocked(rawURL) {
		return &agent.ToolResult{
			Content: fmt.Sprintf("error: URL %q is blocked (private network or disallowed domain)", rawURL),
			IsError: true,
		}, nil
	}

	// 2. Check cache — return cached content if fresh.
	if t.cache != nil {
		if content, found := t.cache.Get(rawURL); found {
			return &agent.ToolResult{Content: content + "\n\n_[cached]_"}, nil
		}
	}

	// 3. Extract domain for rate limiting.
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("error: invalid URL: %v", err), IsError: true}, nil
	}
	domain := parsedURL.Hostname()

	// 4. Apply domain rate limiter — wait if needed.
	if t.rateLimiter != nil {
		if err := t.rateLimiter.Wait(ctx, domain); err != nil {
			return &agent.ToolResult{Content: fmt.Sprintf("error: rate limit wait cancelled: %v", err), IsError: true}, nil
		}
	}

	// 5. Try simple HTTP fetch with random user agent + proxy rotation + retry.
	var htmlContent string
	fetchErr := error(nil)

	resp, err := scraper.Retry(ctx, 3, func() (*http.Response, error) {
		var client *http.Client
		if t.proxyPool != nil {
			client = t.proxyPool.NewHTTPClient(20 * time.Second)
		} else {
			client = &http.Client{Timeout: 20 * time.Second}
		}

		req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if reqErr != nil {
			return nil, reqErr
		}
		req.Header.Set("User-Agent", scraper.RandomUserAgent())
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
		req.Header.Set("Accept-Language", "en-US,en;q=0.5")

		return client.Do(req)
	})

	if err != nil {
		fetchErr = err
	} else {
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			fetchErr = fmt.Errorf("HTTP %d", resp.StatusCode)
		} else {
			body, readErr := io.ReadAll(io.LimitReader(resp.Body, 512<<10))
			if readErr != nil {
				fetchErr = readErr
			} else {
				htmlContent = string(body)
			}
		}
	}

	// 6. Check if we got usable content — if empty/minimal, try headless browser.
	if fetchErr != nil || isJSPlaceholder(htmlContent) {
		if t.browser != nil && t.browser.Available() {
			browserHTML, browserErr := t.browser.Fetch(ctx, rawURL)
			if browserErr == nil && len(browserHTML) > len(htmlContent) {
				htmlContent = browserHTML
				fetchErr = nil
			}
		}
	}

	// If still no content, return the error.
	if fetchErr != nil && htmlContent == "" {
		return &agent.ToolResult{
			Content: fmt.Sprintf("fetch error for %s: %v", rawURL, fetchErr),
			IsError: true,
		}, nil
	}

	// 7. Extract clean text from HTML.
	cleanText := scraper.ExtractText(htmlContent)

	// Truncate if too long for the model context.
	if len(cleanText) > 100*1024 {
		cleanText = cleanText[:100*1024] + "\n[TRUNCATED — content exceeds 100KB]"
	}

	if cleanText == "" {
		return &agent.ToolResult{Content: "No readable content extracted from the page."}, nil
	}

	// 8. Cache the result.
	if t.cache != nil {
		_ = t.cache.Set(rawURL, cleanText)
	}

	return &agent.ToolResult{Content: cleanText}, nil
}

// isJSPlaceholder detects pages that are JS-rendered with no real content.
func isJSPlaceholder(html string) bool {
	if len(html) < 500 {
		return true
	}
	lower := strings.ToLower(html)
	placeholders := []string{
		`<div id="root"></div>`,
		`<div id="app"></div>`,
		`<app-root>`,
		`<div id="__next">`,
		"loading...",
		"please enable javascript",
		"you need to enable javascript",
	}
	for _, p := range placeholders {
		if strings.Contains(lower, p) {
			textLen := len(scraper.ExtractText(html))
			if textLen < 200 {
				return true
			}
		}
	}
	return false
}

var _ agent.Tool = (*WebSearchTool)(nil)
var _ agent.Tool = (*FetchURLTool)(nil)

// ─── HELPERS ─────────────────────────────────────────────────────────────────

func urlEncode(s string) string {
	result := make([]byte, 0, len(s)*3)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') ||
			c == '-' || c == '_' || c == '.' || c == '~' {
			result = append(result, c)
		} else if c == ' ' {
			result = append(result, '+')
		} else {
			result = append(result, '%', hexChar(c>>4), hexChar(c&0xf))
		}
	}
	return string(result)
}

func hexChar(c byte) byte {
	if c < 10 {
		return '0' + c
	}
	return 'a' + c - 10
}
