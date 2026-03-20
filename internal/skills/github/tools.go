package github

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/ziloss-tech/zbot/internal/agent"
)

const defaultRepo = "your-username/zbot"

func repoOrDefault(input map[string]any) string {
	if r, ok := input["repo"].(string); ok && r != "" {
		return r
	}
	return defaultRepo
}

// ─── LIST ISSUES ─────────────────────────────────────────────────────────────

type ListIssuesTool struct{ client *Client }

func (t *ListIssuesTool) Name() string { return "github_list_issues" }
func (t *ListIssuesTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "github_list_issues",
		Description: "List open issues for a GitHub repo.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{},
			"properties": map[string]any{
				"repo":   map[string]any{"type": "string", "description": "owner/repo (default: your-username/zbot)"},
				"state":  map[string]any{"type": "string", "description": "Issue state: open, closed, all (default: open)"},
				"labels": map[string]any{"type": "string", "description": "Comma-separated label filter"},
			},
		},
	}
}

func (t *ListIssuesTool) Execute(ctx context.Context, input map[string]any) (*agent.ToolResult, error) {
	repo := repoOrDefault(input)
	state := "open"
	if s, ok := input["state"].(string); ok && s != "" {
		state = s
	}
	path := fmt.Sprintf("/repos/%s/issues?state=%s&per_page=30", repo, state)
	if labels, ok := input["labels"].(string); ok && labels != "" {
		path += "&labels=" + labels
	}

	data, err := t.client.Get(ctx, path)
	if err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("GitHub error: %v", err), IsError: true}, nil
	}
	return &agent.ToolResult{Content: string(data)}, nil
}

var _ agent.Tool = (*ListIssuesTool)(nil)

// ─── GET ISSUE ───────────────────────────────────────────────────────────────

type GetIssueTool struct{ client *Client }

func (t *GetIssueTool) Name() string { return "github_get_issue" }
func (t *GetIssueTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "github_get_issue",
		Description: "Get full details for a single GitHub issue including body and comments.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"number"},
			"properties": map[string]any{
				"repo":   map[string]any{"type": "string", "description": "owner/repo (default: your-username/zbot)"},
				"number": map[string]any{"type": "integer", "description": "Issue number"},
			},
		},
	}
}

func (t *GetIssueTool) Execute(ctx context.Context, input map[string]any) (*agent.ToolResult, error) {
	repo := repoOrDefault(input)
	number, _ := input["number"].(float64)
	if number == 0 {
		return &agent.ToolResult{Content: "error: number is required", IsError: true}, nil
	}

	data, err := t.client.Get(ctx, fmt.Sprintf("/repos/%s/issues/%d", repo, int(number)))
	if err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("GitHub error: %v", err), IsError: true}, nil
	}
	return &agent.ToolResult{Content: string(data)}, nil
}

var _ agent.Tool = (*GetIssueTool)(nil)

// ─── CREATE ISSUE ────────────────────────────────────────────────────────────

type CreateIssueTool struct{ client *Client }

func (t *CreateIssueTool) Name() string { return "github_create_issue" }
func (t *CreateIssueTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "github_create_issue",
		Description: "Create a new GitHub issue.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"title"},
			"properties": map[string]any{
				"repo":      map[string]any{"type": "string", "description": "owner/repo (default: your-username/zbot)"},
				"title":     map[string]any{"type": "string", "description": "Issue title"},
				"body":      map[string]any{"type": "string", "description": "Issue body (markdown)"},
				"labels":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Labels to add"},
				"assignees": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "GitHub usernames to assign"},
			},
		},
	}
}

func (t *CreateIssueTool) Execute(ctx context.Context, input map[string]any) (*agent.ToolResult, error) {
	repo := repoOrDefault(input)
	title, _ := input["title"].(string)
	if title == "" {
		return &agent.ToolResult{Content: "error: title is required", IsError: true}, nil
	}

	payload := map[string]any{"title": title}
	if body, ok := input["body"].(string); ok {
		payload["body"] = body
	}
	if labels, ok := input["labels"]; ok {
		payload["labels"] = labels
	}
	if assignees, ok := input["assignees"]; ok {
		payload["assignees"] = assignees
	}

	jsonBody, _ := json.Marshal(payload)
	data, err := t.client.Post(ctx, fmt.Sprintf("/repos/%s/issues", repo), jsonBody)
	if err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("GitHub error: %v", err), IsError: true}, nil
	}
	return &agent.ToolResult{Content: string(data)}, nil
}

var _ agent.Tool = (*CreateIssueTool)(nil)

// ─── LIST PRS ────────────────────────────────────────────────────────────────

type ListPRsTool struct{ client *Client }

func (t *ListPRsTool) Name() string { return "github_list_prs" }
func (t *ListPRsTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "github_list_prs",
		Description: "List pull requests for a GitHub repo.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{},
			"properties": map[string]any{
				"repo":  map[string]any{"type": "string", "description": "owner/repo (default: your-username/zbot)"},
				"state": map[string]any{"type": "string", "description": "PR state: open, closed, all (default: open)"},
			},
		},
	}
}

func (t *ListPRsTool) Execute(ctx context.Context, input map[string]any) (*agent.ToolResult, error) {
	repo := repoOrDefault(input)
	state := "open"
	if s, ok := input["state"].(string); ok && s != "" {
		state = s
	}

	data, err := t.client.Get(ctx, fmt.Sprintf("/repos/%s/pulls?state=%s&per_page=30", repo, state))
	if err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("GitHub error: %v", err), IsError: true}, nil
	}
	return &agent.ToolResult{Content: string(data)}, nil
}

var _ agent.Tool = (*ListPRsTool)(nil)

// ─── GET FILE ────────────────────────────────────────────────────────────────

type GetFileTool struct{ client *Client }

func (t *GetFileTool) Name() string { return "github_get_file" }
func (t *GetFileTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "github_get_file",
		Description: "Get decoded file content from a GitHub repo.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"path"},
			"properties": map[string]any{
				"repo": map[string]any{"type": "string", "description": "owner/repo (default: your-username/zbot)"},
				"path": map[string]any{"type": "string", "description": "File path in the repo (e.g. cmd/zbot/main.go)"},
				"ref":  map[string]any{"type": "string", "description": "Branch or commit SHA (default: main)"},
			},
		},
	}
}

func (t *GetFileTool) Execute(ctx context.Context, input map[string]any) (*agent.ToolResult, error) {
	repo := repoOrDefault(input)
	path, _ := input["path"].(string)
	if path == "" {
		return &agent.ToolResult{Content: "error: path is required", IsError: true}, nil
	}

	apiPath := fmt.Sprintf("/repos/%s/contents/%s", repo, path)
	if ref, ok := input["ref"].(string); ok && ref != "" {
		apiPath += "?ref=" + ref
	}

	data, err := t.client.Get(ctx, apiPath)
	if err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("GitHub error: %v", err), IsError: true}, nil
	}

	// Decode base64 content from GitHub response.
	var fileResp struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
		Name     string `json:"name"`
		Path     string `json:"path"`
		Size     int    `json:"size"`
	}
	if err := json.Unmarshal(data, &fileResp); err != nil {
		return &agent.ToolResult{Content: string(data)}, nil
	}

	if fileResp.Encoding == "base64" {
		decoded, err := base64.StdEncoding.DecodeString(fileResp.Content)
		if err != nil {
			return &agent.ToolResult{Content: fmt.Sprintf("base64 decode error: %v", err), IsError: true}, nil
		}
		content := string(decoded)
		if len(content) > 100*1024 {
			content = content[:100*1024] + "\n[TRUNCATED — file exceeds 100KB]"
		}
		return &agent.ToolResult{Content: fmt.Sprintf("# %s (%d bytes)\n\n%s", fileResp.Path, fileResp.Size, content)}, nil
	}

	return &agent.ToolResult{Content: string(data)}, nil
}

var _ agent.Tool = (*GetFileTool)(nil)
