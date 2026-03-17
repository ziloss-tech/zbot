package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zbot-ai/zbot/internal/agent"
)

// ─── FILE SYSTEM TOOLS ───────────────────────────────────────────────────────
// All file operations are scoped to a project directory.
// The agent can NEVER access paths outside its allowed root.

// FileReadTool reads a file from the allowed workspace.
type FileReadTool struct {
	allowedRoot string
}

func NewFileReadTool(allowedRoot string) *FileReadTool {
	return &FileReadTool{allowedRoot: allowedRoot}
}

func (t *FileReadTool) Name() string { return "file_read" }

func (t *FileReadTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "file_read",
		Description: "Read the contents of a file. Path is relative to the workspace root.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "File path relative to workspace (e.g. 'data/report.csv')",
				},
			},
			"required": []string{"path"},
		},
	}
}

func (t *FileReadTool) Execute(_ context.Context, input map[string]any) (*agent.ToolResult, error) {
	path, _ := input["path"].(string)
	resolved, err := t.resolveSafe(path)
	if err != nil {
		return &agent.ToolResult{Content: err.Error(), IsError: true}, nil
	}

	data, err := os.ReadFile(resolved)
	if err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("error reading file: %v", err), IsError: true}, nil
	}

	// Limit to 100KB to avoid flooding context.
	const limit = 100 * 1024
	content := string(data)
	if len(data) > limit {
		content = string(data[:limit]) + fmt.Sprintf("\n\n[TRUNCATED — file is %d bytes, showing first 100KB]", len(data))
	}

	return &agent.ToolResult{Content: content}, nil
}

// FileWriteTool creates or overwrites a file in the workspace.
type FileWriteTool struct {
	allowedRoot string
}

func NewFileWriteTool(allowedRoot string) *FileWriteTool {
	return &FileWriteTool{allowedRoot: allowedRoot}
}

func (t *FileWriteTool) Name() string { return "file_write" }

func (t *FileWriteTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "file_write",
		Description: "Write content to a file. Creates the file and any parent directories if they don't exist.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "File path relative to workspace",
				},
				"content": map[string]any{
					"type":        "string",
					"description": "Content to write to the file",
				},
				"mode": map[string]any{
					"type":        "string",
					"enum":        []string{"overwrite", "append"},
					"description": "Write mode: 'overwrite' (default) or 'append'",
					"default":     "overwrite",
				},
			},
			"required": []string{"path", "content"},
		},
	}
}

func (t *FileWriteTool) Execute(_ context.Context, input map[string]any) (*agent.ToolResult, error) {
	path, _ := input["path"].(string)
	content, _ := input["content"].(string)
	mode, _ := input["mode"].(string)

	resolved, err := t.resolveSafe(path)
	if err != nil {
		return &agent.ToolResult{Content: err.Error(), IsError: true}, nil
	}

	// Ensure parent directories exist.
	if err := os.MkdirAll(filepath.Dir(resolved), 0755); err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("error creating directories: %v", err), IsError: true}, nil
	}

	flag := os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	if mode == "append" {
		flag = os.O_CREATE | os.O_WRONLY | os.O_APPEND
	}

	f, err := os.OpenFile(resolved, flag, 0644)
	if err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("error opening file: %v", err), IsError: true}, nil
	}
	defer f.Close()

	if _, err := f.WriteString(content); err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("error writing file: %v", err), IsError: true}, nil
	}

	return &agent.ToolResult{
		Content: fmt.Sprintf("✅ Written %d bytes to %s", len(content), path),
	}, nil
}

// FileListTool lists files in a workspace directory.
type FileListTool struct {
	allowedRoot string
}

func NewFileListTool(allowedRoot string) *FileListTool {
	return &FileListTool{allowedRoot: allowedRoot}
}

func (t *FileListTool) Name() string { return "file_list" }

func (t *FileListTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "file_list",
		Description: "List files and directories in a workspace path.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Directory path relative to workspace (default: '.')",
					"default":     ".",
				},
			},
		},
	}
}

func (t *FileListTool) Execute(_ context.Context, input map[string]any) (*agent.ToolResult, error) {
	path, _ := input["path"].(string)
	if path == "" {
		path = "."
	}

	resolved, err := t.resolveSafe(path)
	if err != nil {
		return &agent.ToolResult{Content: err.Error(), IsError: true}, nil
	}

	entries, err := os.ReadDir(resolved)
	if err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("error listing: %v", err), IsError: true}, nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Contents of %s\n\n", path))
	for _, e := range entries {
		info, _ := e.Info()
		if e.IsDir() {
			sb.WriteString(fmt.Sprintf("📁 %s/\n", e.Name()))
		} else {
			sb.WriteString(fmt.Sprintf("📄 %s (%s)\n", e.Name(), formatSize(info.Size())))
		}
	}
	return &agent.ToolResult{Content: sb.String()}, nil
}

// resolveSafe resolves a relative path against allowedRoot,
// ensuring it doesn't escape via path traversal (../../etc).
func (t *FileReadTool) resolveSafe(path string) (string, error) {
	return resolveSafe(t.allowedRoot, path)
}
func (t *FileWriteTool) resolveSafe(path string) (string, error) {
	return resolveSafe(t.allowedRoot, path)
}
func (t *FileListTool) resolveSafe(path string) (string, error) {
	return resolveSafe(t.allowedRoot, path)
}

func resolveSafe(root, path string) (string, error) {
	abs, err := filepath.Abs(filepath.Join(root, filepath.Clean("/"+path)))
	if err != nil {
		return "", fmt.Errorf("invalid path: %w", err)
	}
	absRoot, _ := filepath.Abs(root)
	if !strings.HasPrefix(abs, absRoot) {
		return "", fmt.Errorf("error: path %q escapes workspace root", path)
	}
	return abs, nil
}

func formatSize(b int64) string {
	switch {
	case b < 1024:
		return fmt.Sprintf("%dB", b)
	case b < 1024*1024:
		return fmt.Sprintf("%.1fKB", float64(b)/1024)
	default:
		return fmt.Sprintf("%.1fMB", float64(b)/(1024*1024))
	}
}
