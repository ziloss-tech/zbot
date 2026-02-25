// File and memory tools for the ZBOT agent.
package tools

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jeremylerwick-max/zbot/internal/agent"
)

// ─── READ FILE ────────────────────────────────────────────────────────────────

// ReadFileTool reads files within the allowed workspace root.
// Path traversal is blocked — only paths inside AllowedRoot are accessible.
type ReadFileTool struct{ allowedRoot string }

func NewReadFileTool(allowedRoot string) *ReadFileTool { return &ReadFileTool{allowedRoot} }
func (t *ReadFileTool) Name() string                  { return "read_file" }

func (t *ReadFileTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "read_file",
		Description: "Read the contents of a file in the workspace.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"path"},
			"properties": map[string]any{
				"path": map[string]any{"type": "string", "description": "File path relative to workspace root"},
			},
		},
	}
}

func (t *ReadFileTool) Execute(ctx context.Context, input map[string]any) (*agent.ToolResult, error) {
	relPath, _ := input["path"].(string)
	abs, err := safePath(t.allowedRoot, relPath)
	if err != nil {
		return &agent.ToolResult{Content: err.Error(), IsError: true}, nil
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("read error: %v", err), IsError: true}, nil
	}
	const maxBytes = 512 << 10 // 512KB
	if len(data) > maxBytes {
		data = append(data[:maxBytes], []byte("\n[TRUNCATED — file exceeds 512KB]")...)
	}
	return &agent.ToolResult{Content: string(data)}, nil
}

var _ agent.Tool = (*ReadFileTool)(nil)

// ─── WRITE FILE ───────────────────────────────────────────────────────────────

// WriteFileTool creates or overwrites files inside the workspace root.
type WriteFileTool struct{ allowedRoot string }

func NewWriteFileTool(allowedRoot string) *WriteFileTool { return &WriteFileTool{allowedRoot} }
func (t *WriteFileTool) Name() string                   { return "write_file" }

func (t *WriteFileTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "write_file",
		Description: "Write or overwrite a file in the workspace. Creates parent directories if needed.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"path", "content"},
			"properties": map[string]any{
				"path":    map[string]any{"type": "string", "description": "File path relative to workspace root"},
				"content": map[string]any{"type": "string", "description": "Content to write"},
			},
		},
	}
}

func (t *WriteFileTool) Execute(ctx context.Context, input map[string]any) (*agent.ToolResult, error) {
	relPath, _ := input["path"].(string)
	content, _ := input["content"].(string)

	abs, err := safePath(t.allowedRoot, relPath)
	if err != nil {
		return &agent.ToolResult{Content: err.Error(), IsError: true}, nil
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o750); err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("mkdir error: %v", err), IsError: true}, nil
	}
	if err := os.WriteFile(abs, []byte(content), 0o640); err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("write error: %v", err), IsError: true}, nil
	}
	return &agent.ToolResult{Content: fmt.Sprintf("✓ Written %d bytes to %s", len(content), relPath)}, nil
}

var _ agent.Tool = (*WriteFileTool)(nil)

// ─── MEMORY SAVE ─────────────────────────────────────────────────────────────

// MemorySaveTool lets the agent explicitly persist a fact to long-term memory.
type MemorySaveTool struct{ store agent.MemoryStore }

func NewMemorySaveTool(store agent.MemoryStore) *MemorySaveTool { return &MemorySaveTool{store} }
func (t *MemorySaveTool) Name() string                          { return "save_memory" }

func (t *MemorySaveTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "save_memory",
		Description: "Save an important fact to long-term memory for future recall.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"fact"},
			"properties": map[string]any{
				"fact":   map[string]any{"type": "string", "description": "The fact to remember"},
				"source": map[string]any{"type": "string", "description": "Source: 'user', 'research', 'agent'"},
				"tags":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			},
		},
	}
}

func (t *MemorySaveTool) Execute(ctx context.Context, input map[string]any) (*agent.ToolResult, error) {
	content, _ := input["fact"].(string)
	source, _ := input["source"].(string)
	if source == "" {
		source = "agent"
	}
	var tags []string
	if raw, ok := input["tags"].([]any); ok {
		for _, v := range raw {
			if s, ok := v.(string); ok {
				tags = append(tags, s)
			}
		}
	}
	fact := agent.Fact{
		ID: randomID(), Content: content, Source: source,
		Tags: tags, CreatedAt: time.Now(),
	}
	if err := t.store.Save(ctx, fact); err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("error saving: %v", err), IsError: true}, nil
	}
	return &agent.ToolResult{Content: fmt.Sprintf("✓ Saved to memory: %q", content)}, nil
}

var _ agent.Tool = (*MemorySaveTool)(nil)

// ─── HELPERS ──────────────────────────────────────────────────────────────────

// safePath joins root + relPath and confirms the result stays within root.
// Blocks path traversal attacks (e.g. ../../etc/passwd).
func safePath(root, relPath string) (string, error) {
	abs := filepath.Clean(filepath.Join(root, relPath))
	if !strings.HasPrefix(abs, filepath.Clean(root)+string(os.PathSeparator)) && abs != filepath.Clean(root) {
		return "", fmt.Errorf("access denied: %q is outside workspace", relPath)
	}
	return abs, nil
}

// randomID returns a 16-char hex string using crypto/rand.
func randomID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
