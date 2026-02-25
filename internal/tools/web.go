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
	"time"

	"github.com/jeremylerwick-max/zbot/internal/agent"
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
	req.Header.Set("Accept-Encoding", "gzip")
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

// ─── URL FETCH ────────────────────────────────────────────────────────────────

// FetchURLTool fetches and extracts text content from a URL.
// Tier 3 in the research stack — for specific URLs the model wants to read.
type FetchURLTool struct {
	httpClient *http.Client
}

func NewFetchURLTool() *FetchURLTool {
	return &FetchURLTool{
		httpClient: &http.Client{Timeout: 20 * time.Second},
	}
}

func (t *FetchURLTool) Name() string { return "fetch_url" }

func (t *FetchURLTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "fetch_url",
		Description: "Fetch and read the text content of a specific URL. Use for reading articles, docs, or any web page.",
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
	url, _ := input["url"].(string)
	if url == "" {
		return &agent.ToolResult{Content: "error: url is required", IsError: true}, nil
	}

	// Basic URL validation — block localhost/private IPs (SSRF prevention).
	if isBlockedURL(url) {
		return &agent.ToolResult{
			Content: fmt.Sprintf("error: URL %q is blocked (private network or disallowed)", url),
			IsError: true,
		}, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("fetch_url: build request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; ZBOTBot/1.0)")

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch_url: request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return &agent.ToolResult{
			Content: fmt.Sprintf("fetch error: HTTP %d for %s", resp.StatusCode, url),
			IsError: true,
		}, nil
	}

	// Read up to 512KB — enough for any article, not enough to OOM on large files.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 512<<10))
	if err != nil {
		return nil, fmt.Errorf("fetch_url: read body: %w", err)
	}

	// TODO Phase 3: Strip HTML tags, extract main content.
	// For now, return raw (model handles HTML ok for text extraction).
	return &agent.ToolResult{Content: string(body)}, nil
}

// isBlockedURL prevents SSRF by rejecting private/localhost URLs.
func isBlockedURL(url string) bool {
	blocked := []string{
		"localhost", "127.0.0.1", "0.0.0.0",
		"169.254.", "192.168.", "10.", "172.16.",
		"::1", "[::1]",
	}
	for _, b := range blocked {
		if len(url) > len(b) {
			lower := toLower(url[7:]) // skip "http://"
			if len(lower) >= len(b) && lower[:len(b)] == b {
				return true
			}
		}
	}
	return false
}

func toLower(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 32
		}
	}
	return string(b)
}

func urlEncode(s string) string {
	// Simple URL encoding — Phase 1 will use net/url.QueryEscape.
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
