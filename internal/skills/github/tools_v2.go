package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/ziloss-tech/zbot/internal/agent"
)

// ─── SEARCH REPOS ────────────────────────────────────────────────────────────

type SearchReposTool struct{ client *Client }

func (t *SearchReposTool) Name() string { return "github_search_repos" }
func (t *SearchReposTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "github_search_repos",
		Description: "Search GitHub repositories by keyword, topic, language, or user.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"query"},
			"properties": map[string]any{
				"query": map[string]any{"type": "string", "description": "Search query (e.g. 'ai agent golang', 'user:ziloss-tech', 'topic:llm')"},
				"sort":  map[string]any{"type": "string", "description": "Sort by: stars, forks, updated (default: best match)"},
				"limit": map[string]any{"type": "integer", "description": "Max results (default 10, max 30)"},
			},
		},
	}
}

func (t *SearchReposTool) Execute(ctx context.Context, input map[string]any) (*agent.ToolResult, error) {
	query, _ := input["query"].(string)
	if query == "" {
		return &agent.ToolResult{Content: "error: query is required", IsError: true}, nil
	}
	limit := 10
	if l, ok := input["limit"].(float64); ok && l > 0 {
		limit = int(l)
		if limit > 30 {
			limit = 30
		}
	}
	path := fmt.Sprintf("/search/repositories?q=%s&per_page=%d", url.QueryEscape(query), limit)
	if sort, ok := input["sort"].(string); ok && sort != "" {
		path += "&sort=" + sort
	}
	data, err := t.client.Get(ctx, path)
	if err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("GitHub error: %v", err), IsError: true}, nil
	}

	// Trim to essential fields
	var raw struct {
		TotalCount int `json:"total_count"`
		Items      []struct {
			FullName    string `json:"full_name"`
			Description string `json:"description"`
			Stars       int    `json:"stargazers_count"`
			Language    string `json:"language"`
			HTMLURL     string `json:"html_url"`
			UpdatedAt   string `json:"updated_at"`
		} `json:"items"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return &agent.ToolResult{Content: string(data)}, nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d repos (showing %d):\n\n", raw.TotalCount, len(raw.Items)))
	for _, r := range raw.Items {
		sb.WriteString(fmt.Sprintf("• **%s** ⭐%d [%s]\n  %s\n  %s\n\n", r.FullName, r.Stars, r.Language, r.Description, r.HTMLURL))
	}
	return &agent.ToolResult{Content: sb.String()}, nil
}

var _ agent.Tool = (*SearchReposTool)(nil)

// ─── SEARCH CODE ─────────────────────────────────────────────────────────────

type SearchCodeTool struct{ client *Client }

func (t *SearchCodeTool) Name() string { return "github_search_code" }
func (t *SearchCodeTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "github_search_code",
		Description: "Search code across GitHub repos. Finds files containing a specific term.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"query"},
			"properties": map[string]any{
				"query": map[string]any{"type": "string", "description": "Code search (e.g. 'handleWebhook repo:ziloss-tech/zbot', 'language:go func main')"},
				"limit": map[string]any{"type": "integer", "description": "Max results (default 10, max 30)"},
			},
		},
	}
}

func (t *SearchCodeTool) Execute(ctx context.Context, input map[string]any) (*agent.ToolResult, error) {
	query, _ := input["query"].(string)
	if query == "" {
		return &agent.ToolResult{Content: "error: query is required", IsError: true}, nil
	}
	limit := 10
	if l, ok := input["limit"].(float64); ok && l > 0 {
		limit = int(l)
		if limit > 30 {
			limit = 30
		}
	}
	path := fmt.Sprintf("/search/code?q=%s&per_page=%d", url.QueryEscape(query), limit)
	data, err := t.client.Get(ctx, path)
	if err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("GitHub error: %v", err), IsError: true}, nil
	}
	return &agent.ToolResult{Content: string(data)}, nil
}

var _ agent.Tool = (*SearchCodeTool)(nil)

// ─── LIST COMMITS ────────────────────────────────────────────────────────────

type ListCommitsTool struct{ client *Client }

func (t *ListCommitsTool) Name() string { return "github_list_commits" }
func (t *ListCommitsTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "github_list_commits",
		Description: "List recent commits for a GitHub repo, optionally filtered by branch or path.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{},
			"properties": map[string]any{
				"repo":   map[string]any{"type": "string", "description": "owner/repo (default: your-username/zbot)"},
				"branch": map[string]any{"type": "string", "description": "Branch name (default: main)"},
				"path":   map[string]any{"type": "string", "description": "Filter commits touching this file path"},
				"limit":  map[string]any{"type": "integer", "description": "Max results (default 20)"},
			},
		},
	}
}

func (t *ListCommitsTool) Execute(ctx context.Context, input map[string]any) (*agent.ToolResult, error) {
	repo := repoOrDefault(input)
	limit := 20
	if l, ok := input["limit"].(float64); ok && l > 0 {
		limit = int(l)
	}
	path := fmt.Sprintf("/repos/%s/commits?per_page=%d", repo, limit)
	if branch, ok := input["branch"].(string); ok && branch != "" {
		path += "&sha=" + branch
	}
	if fp, ok := input["path"].(string); ok && fp != "" {
		path += "&path=" + url.QueryEscape(fp)
	}
	data, err := t.client.Get(ctx, path)
	if err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("GitHub error: %v", err), IsError: true}, nil
	}

	// Trim to essentials
	var commits []struct {
		SHA    string `json:"sha"`
		Commit struct {
			Message string `json:"message"`
			Author  struct {
				Name string `json:"name"`
				Date string `json:"date"`
			} `json:"author"`
		} `json:"commit"`
	}
	if err := json.Unmarshal(data, &commits); err != nil {
		return &agent.ToolResult{Content: string(data)}, nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Recent commits for %s:\n\n", repo))
	for _, c := range commits {
		msg := c.Commit.Message
		if idx := strings.Index(msg, "\n"); idx > 0 {
			msg = msg[:idx]
		}
		sb.WriteString(fmt.Sprintf("• `%s` %s — %s (%s)\n", c.SHA[:7], msg, c.Commit.Author.Name, c.Commit.Author.Date))
	}
	return &agent.ToolResult{Content: sb.String()}, nil
}

var _ agent.Tool = (*ListCommitsTool)(nil)

// ─── CREATE PULL REQUEST ─────────────────────────────────────────────────────

type CreatePRTool struct{ client *Client }

func (t *CreatePRTool) Name() string { return "github_create_pr" }
func (t *CreatePRTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "github_create_pr",
		Description: "Create a pull request on GitHub.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"title", "head", "base"},
			"properties": map[string]any{
				"repo":  map[string]any{"type": "string", "description": "owner/repo (default: your-username/zbot)"},
				"title": map[string]any{"type": "string", "description": "PR title"},
				"body":  map[string]any{"type": "string", "description": "PR description (markdown)"},
				"head":  map[string]any{"type": "string", "description": "Branch with your changes (e.g. feature-branch)"},
				"base":  map[string]any{"type": "string", "description": "Branch to merge into (e.g. main)"},
				"draft": map[string]any{"type": "boolean", "description": "Create as draft PR (default: false)"},
			},
		},
	}
}

func (t *CreatePRTool) Execute(ctx context.Context, input map[string]any) (*agent.ToolResult, error) {
	repo := repoOrDefault(input)
	title, _ := input["title"].(string)
	head, _ := input["head"].(string)
	base, _ := input["base"].(string)
	if title == "" || head == "" || base == "" {
		return &agent.ToolResult{Content: "error: title, head, and base are required", IsError: true}, nil
	}
	payload := map[string]any{"title": title, "head": head, "base": base}
	if body, ok := input["body"].(string); ok {
		payload["body"] = body
	}
	if draft, ok := input["draft"].(bool); ok {
		payload["draft"] = draft
	}
	jsonBody, _ := json.Marshal(payload)
	data, err := t.client.Post(ctx, fmt.Sprintf("/repos/%s/pulls", repo), jsonBody)
	if err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("GitHub error: %v", err), IsError: true}, nil
	}
	return &agent.ToolResult{Content: string(data)}, nil
}

var _ agent.Tool = (*CreatePRTool)(nil)

// ─── LIST BRANCHES ───────────────────────────────────────────────────────────

type ListBranchesTool struct{ client *Client }

func (t *ListBranchesTool) Name() string { return "github_list_branches" }
func (t *ListBranchesTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "github_list_branches",
		Description: "List branches for a GitHub repo.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{},
			"properties": map[string]any{
				"repo": map[string]any{"type": "string", "description": "owner/repo (default: your-username/zbot)"},
			},
		},
	}
}

func (t *ListBranchesTool) Execute(ctx context.Context, input map[string]any) (*agent.ToolResult, error) {
	repo := repoOrDefault(input)
	data, err := t.client.Get(ctx, fmt.Sprintf("/repos/%s/branches?per_page=100", repo))
	if err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("GitHub error: %v", err), IsError: true}, nil
	}
	return &agent.ToolResult{Content: string(data)}, nil
}

var _ agent.Tool = (*ListBranchesTool)(nil)

// ─── LIST REPO TREE ──────────────────────────────────────────────────────────

type ListTreeTool struct{ client *Client }

func (t *ListTreeTool) Name() string { return "github_list_tree" }
func (t *ListTreeTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "github_list_tree",
		Description: "List the file tree of a GitHub repo at a given path (like ls). Shows files and directories.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{},
			"properties": map[string]any{
				"repo": map[string]any{"type": "string", "description": "owner/repo (default: your-username/zbot)"},
				"path": map[string]any{"type": "string", "description": "Directory path (empty for root)"},
				"ref":  map[string]any{"type": "string", "description": "Branch or commit SHA (default: main)"},
			},
		},
	}
}

func (t *ListTreeTool) Execute(ctx context.Context, input map[string]any) (*agent.ToolResult, error) {
	repo := repoOrDefault(input)
	dirPath, _ := input["path"].(string)
	apiPath := fmt.Sprintf("/repos/%s/contents/%s", repo, dirPath)
	if ref, ok := input["ref"].(string); ok && ref != "" {
		apiPath += "?ref=" + ref
	}
	data, err := t.client.Get(ctx, apiPath)
	if err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("GitHub error: %v", err), IsError: true}, nil
	}

	var items []struct {
		Name string `json:"name"`
		Type string `json:"type"` // "file" or "dir"
		Size int    `json:"size"`
		Path string `json:"path"`
	}
	if err := json.Unmarshal(data, &items); err != nil {
		return &agent.ToolResult{Content: string(data)}, nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s/%s:\n\n", repo, dirPath))
	for _, item := range items {
		prefix := "📄"
		if item.Type == "dir" {
			prefix = "📁"
		}
		sb.WriteString(fmt.Sprintf("%s %s", prefix, item.Name))
		if item.Type == "file" {
			sb.WriteString(fmt.Sprintf(" (%d bytes)", item.Size))
		}
		sb.WriteString("\n")
	}
	return &agent.ToolResult{Content: sb.String()}, nil
}

var _ agent.Tool = (*ListTreeTool)(nil)

// ─── COMMENT ON ISSUE ────────────────────────────────────────────────────────

type CommentIssueTool struct{ client *Client }

func (t *CommentIssueTool) Name() string { return "github_comment_issue" }
func (t *CommentIssueTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "github_comment_issue",
		Description: "Add a comment to a GitHub issue or PR.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"number", "body"},
			"properties": map[string]any{
				"repo":   map[string]any{"type": "string", "description": "owner/repo (default: your-username/zbot)"},
				"number": map[string]any{"type": "integer", "description": "Issue or PR number"},
				"body":   map[string]any{"type": "string", "description": "Comment body (markdown)"},
			},
		},
	}
}

func (t *CommentIssueTool) Execute(ctx context.Context, input map[string]any) (*agent.ToolResult, error) {
	repo := repoOrDefault(input)
	number, _ := input["number"].(float64)
	body, _ := input["body"].(string)
	if number == 0 || body == "" {
		return &agent.ToolResult{Content: "error: number and body are required", IsError: true}, nil
	}
	payload, _ := json.Marshal(map[string]string{"body": body})
	data, err := t.client.Post(ctx, fmt.Sprintf("/repos/%s/issues/%d/comments", repo, int(number)), payload)
	if err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("GitHub error: %v", err), IsError: true}, nil
	}
	return &agent.ToolResult{Content: string(data)}, nil
}

var _ agent.Tool = (*CommentIssueTool)(nil)

// ─── GET REPO INFO ───────────────────────────────────────────────────────────

type GetRepoTool struct{ client *Client }

func (t *GetRepoTool) Name() string { return "github_get_repo" }
func (t *GetRepoTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "github_get_repo",
		Description: "Get detailed information about a GitHub repo (stars, forks, topics, license, etc).",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"repo"},
			"properties": map[string]any{
				"repo": map[string]any{"type": "string", "description": "owner/repo"},
			},
		},
	}
}

func (t *GetRepoTool) Execute(ctx context.Context, input map[string]any) (*agent.ToolResult, error) {
	repo, _ := input["repo"].(string)
	if repo == "" {
		return &agent.ToolResult{Content: "error: repo is required", IsError: true}, nil
	}
	data, err := t.client.Get(ctx, fmt.Sprintf("/repos/%s", repo))
	if err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("GitHub error: %v", err), IsError: true}, nil
	}
	return &agent.ToolResult{Content: string(data)}, nil
}

var _ agent.Tool = (*GetRepoTool)(nil)

// ─── PATCH (for client) ─────────────────────────────────────────────────────

// Patch performs an authenticated PATCH request.
func (c *Client) Patch(ctx context.Context, path string, jsonBody []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "PATCH", apiBase+path, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("github.Patch: %w", err)
	}
	c.setHeaders(req)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github.Patch: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, fmt.Errorf("github.Patch: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("github.Patch %s: HTTP %d — %s", path, resp.StatusCode, string(respBody))
	}
	return respBody, nil
}
