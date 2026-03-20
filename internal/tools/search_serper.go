package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/ziloss-tech/zbot/internal/agent"
)

// SerperSearchTool searches the web via Serper.dev API (Google results).
// 16x cheaper than Brave: $0.30/1K queries vs $5/1K.
// Returns the same clean format so the model can't tell the difference.
type SerperSearchTool struct {
	apiKey     string
	httpClient *http.Client
}

func NewSerperSearchTool(apiKey string) *SerperSearchTool {
	return &SerperSearchTool{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

func (t *SerperSearchTool) Name() string { return "web_search" }

func (t *SerperSearchTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "web_search",
		Description: "Search the web for current information. Returns titles, URLs, and snippets from Google.",
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

func (t *SerperSearchTool) Execute(ctx context.Context, input map[string]any) (*agent.ToolResult, error) {
	query, _ := input["query"].(string)
	if query == "" {
		return &agent.ToolResult{Content: "error: query is required", IsError: true}, nil
	}

	count := 10
	if c, ok := input["count"].(float64); ok && c > 0 {
		count = int(c)
	}

	// Serper uses POST with JSON body.
	reqBody, _ := json.Marshal(map[string]any{
		"q":   query,
		"num": count,
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://google.serper.dev/search",
		bytes_NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("serper: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-KEY", t.apiKey)

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("serper: request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return &agent.ToolResult{
			Content: fmt.Sprintf("serper API error %d: %s", resp.StatusCode, body),
			IsError: true,
		}, nil
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
	if err != nil {
		return nil, fmt.Errorf("serper: read body: %w", err)
	}

	var result serperSearchResult
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("serper: parse response: %w", err)
	}

	// Format identically to Brave results — model sees the same format.
	output := fmt.Sprintf("## Search Results for: %q\n\n", query)
	for i, r := range result.Organic {
		output += fmt.Sprintf("### %d. %s\n**URL:** %s\n%s\n\n", i+1, r.Title, r.Link, r.Snippet)
	}
	if len(result.Organic) == 0 {
		output += "_No results found._"
	}

	return &agent.ToolResult{Content: output}, nil
}

type serperSearchResult struct {
	Organic []struct {
		Title   string `json:"title"`
		Link    string `json:"link"`
		Snippet string `json:"snippet"`
	} `json:"organic"`
}

// bytes_NewReader is a helper to avoid importing bytes just for this.
func bytes_NewReader(b []byte) io.Reader {
	return &bytesReader{data: b, pos: 0}
}

type bytesReader struct {
	data []byte
	pos  int
}

func (r *bytesReader) Read(p []byte) (n int, err error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n = copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

var _ agent.Tool = (*SerperSearchTool)(nil)
