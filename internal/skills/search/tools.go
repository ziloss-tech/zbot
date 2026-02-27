package search

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jeremylerwick-max/zbot/internal/agent"
	"golang.org/x/net/html"
)

var httpClient = &http.Client{Timeout: 15 * time.Second}

// ─── WEB SEARCH ───────────────────────────────────────────────────────────────

type WebSearchTool struct{}

func (t *WebSearchTool) Name() string { return "web_search" }
func (t *WebSearchTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "web_search",
		Description: "Search the web via DuckDuckGo. Returns titles, snippets, and URLs for top results.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"query"},
			"properties": map[string]any{
				"query":       map[string]any{"type": "string", "description": "Search query"},
				"max_results": map[string]any{"type": "integer", "description": "Max results (default 8, max 20)"},
			},
		},
	}
}

func (t *WebSearchTool) Execute(ctx context.Context, input map[string]any) (*agent.ToolResult, error) {
	query, _ := input["query"].(string)
	if query == "" {
		return &agent.ToolResult{Content: "query is required", IsError: true}, nil
	}
	maxResults := 8
	if v, ok := input["max_results"].(float64); ok && v > 0 {
		maxResults = int(v)
	}
	if maxResults > 20 {
		maxResults = 20
	}

	searchURL := "https://html.duckduckgo.com/html/?q=" + url.QueryEscape(query)
	req, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	if err != nil {
		return &agent.ToolResult{Content: err.Error(), IsError: true}, nil
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36")
	req.Header.Set("Accept", "text/html")

	resp, err := httpClient.Do(req)
	if err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("search failed: %v", err), IsError: true}, nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return &agent.ToolResult{Content: err.Error(), IsError: true}, nil
	}

	results := parseDDGResults(string(body), maxResults)
	if len(results) == 0 {
		return &agent.ToolResult{Content: fmt.Sprintf("No results found for: %s", query)}, nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Search results for: %q\n\n", query))
	for i, r := range results {
		sb.WriteString(fmt.Sprintf("%d. **%s**\n   URL: %s\n   %s\n\n", i+1, r.title, r.url, r.snippet))
	}
	return &agent.ToolResult{Content: sb.String()}, nil
}

type searchResult struct{ title, url, snippet string }

func parseDDGResults(body string, max int) []searchResult {
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		return nil
	}
	var results []searchResult
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if len(results) >= max {
			return
		}
		if n.Type == html.ElementNode && n.Data == "div" {
			for _, a := range n.Attr {
				if a.Key == "class" && strings.Contains(a.Val, "result__body") {
					if r := extractResult(n); r.title != "" && r.url != "" {
						results = append(results, r)
					}
					return
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return results
}

func extractResult(n *html.Node) searchResult {
	var r searchResult
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			for _, a := range n.Attr {
				if a.Key == "class" {
					switch {
					case strings.Contains(a.Val, "result__a"):
						if r.title == "" {
							r.title = textContent(n)
						}
						for _, aa := range n.Attr {
							if aa.Key == "href" && r.url == "" {
								href := aa.Val
								if strings.HasPrefix(href, "//duckduckgo.com/l/?uddg=") {
									if u, err := url.QueryUnescape(strings.TrimPrefix(href, "//duckduckgo.com/l/?uddg=")); err == nil {
										r.url = u
									}
								} else if strings.HasPrefix(href, "http") {
									r.url = href
								}
							}
						}
					case strings.Contains(a.Val, "result__snippet"):
						if r.snippet == "" {
							r.snippet = textContent(n)
						}
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return r
}

func textContent(n *html.Node) string {
	var sb strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			sb.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return strings.TrimSpace(sb.String())
}

// ─── SCRAPE PAGE ──────────────────────────────────────────────────────────────

type ScrapePageTool struct{}

func (t *ScrapePageTool) Name() string { return "scrape_page" }
func (t *ScrapePageTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "scrape_page",
		Description: "Fetch a web page and extract readable text. Use after web_search to get full page details.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"url"},
			"properties": map[string]any{
				"url":       map[string]any{"type": "string", "description": "Full URL to fetch"},
				"max_chars": map[string]any{"type": "integer", "description": "Max characters to return (default 8000)"},
			},
		},
	}
}

func (t *ScrapePageTool) Execute(ctx context.Context, input map[string]any) (*agent.ToolResult, error) {
	pageURL, _ := input["url"].(string)
	if pageURL == "" {
		return &agent.ToolResult{Content: "url is required", IsError: true}, nil
	}
	maxChars := 8000
	if v, ok := input["max_chars"].(float64); ok && v > 0 {
		maxChars = int(v)
	}
	if maxChars > 20000 {
		maxChars = 20000
	}

	req, err := http.NewRequestWithContext(ctx, "GET", pageURL, nil)
	if err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("invalid URL: %v", err), IsError: true}, nil
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36")
	req.Header.Set("Accept", "text/html")

	resp, err := httpClient.Do(req)
	if err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("fetch failed: %v", err), IsError: true}, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return &agent.ToolResult{Content: fmt.Sprintf("HTTP %d from %s", resp.StatusCode, pageURL), IsError: true}, nil
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return &agent.ToolResult{Content: err.Error(), IsError: true}, nil
	}

	text := extractPageText(string(body))
	text = strings.TrimSpace(text)

	if utf8.RuneCountInString(text) > maxChars {
		runes := []rune(text)
		text = string(runes[:maxChars]) + "\n\n[truncated]"
	}
	if text == "" {
		return &agent.ToolResult{Content: fmt.Sprintf("No readable text extracted from %s", pageURL)}, nil
	}
	return &agent.ToolResult{Content: fmt.Sprintf("Content from %s:\n\n%s", pageURL, text)}, nil
}

func extractPageText(body string) string {
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		return body
	}
	var sb strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "script", "style", "nav", "footer", "noscript", "iframe", "svg":
				return
			}
		}
		if n.Type == html.TextNode {
			if t := strings.TrimSpace(n.Data); t != "" {
				sb.WriteString(t)
				sb.WriteString(" ")
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
		if n.Type == html.ElementNode {
			switch n.Data {
			case "p", "div", "h1", "h2", "h3", "h4", "h5", "h6", "li", "br", "tr":
				sb.WriteString("\n")
			}
		}
	}
	walk(doc)
	return sb.String()
}
