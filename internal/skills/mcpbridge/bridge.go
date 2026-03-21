// Package mcpbridge dynamically loads MCP (Model Context Protocol) servers
// and exposes their tools as native ZBOT skills.
//
// This is the "Zapier replacement" layer — any open-source MCP server from
// the MCP registry (github.com/mcp) can be consumed as a ZBOT skill without
// writing integration code.
//
// How it works:
//   1. User configures an MCP server in config.yaml or via vault_put
//   2. mcpbridge spawns the server as a subprocess (stdio transport)
//   3. Sends MCP initialize + tools/list to discover available tools
//   4. Wraps each MCP tool as an agent.Tool with proper JSON Schema
//   5. Registers the collection as a ZBOT Skill
//   6. Claude can now use all tools from that MCP server natively
//
// Credits:
//   This package is inspired by and builds upon the open-source MCP ecosystem.
//   Thanks to Anthropic for the MCP specification (modelcontextprotocol.io),
//   and to the maintainers of the open-source MCP servers that make this
//   plug-and-play integration possible:
//   - microsoft/markitdown-mcp, microsoft/playwright-mcp
//   - github/github-mcp-server
//   - firecrawl, supabase-community, bytebase/dbhub
//   - wonderwhy-er/desktop-commander
//   - and many others in the MCP registry
//
// Architecture:
//   mcpbridge uses JSON-RPC 2.0 over stdio, matching the MCP transport spec.
//   Each MCP server runs as a child process. Communication is synchronous
//   request-response over stdin/stdout with newline-delimited JSON.
package mcpbridge

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ziloss-tech/zbot/internal/agent"
)

// ─── MCP JSON-RPC TYPES ─────────────────────────────────────────────────────

type jsonRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// MCPToolDef is a tool definition from an MCP server's tools/list response.
type MCPToolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

type toolsListResult struct {
	Tools []MCPToolDef `json:"tools"`
}

type callToolResult struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	IsError bool `json:"isError"`
}

// ─── SERVER CONFIG ──────────────────────────────────────────────────────────

// ServerConfig defines how to launch an MCP server.
type ServerConfig struct {
	// Name is the skill name (e.g. "stripe", "notion", "playwright").
	Name string `json:"name" yaml:"name"`

	// Command is the executable to run (e.g. "npx", "uvx", "node").
	Command string `json:"command" yaml:"command"`

	// Args are command-line arguments (e.g. ["-y", "@stripe/mcp-server"]).
	Args []string `json:"args" yaml:"args"`

	// Env are additional environment variables (e.g. {"STRIPE_SECRET_KEY": "sk-..."}).
	// Values starting with "vault:" are resolved from the ZBOT vault at startup.
	Env map[string]string `json:"env,omitempty" yaml:"env,omitempty"`

	// Description overrides the auto-generated skill description.
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
}

// ─── MCP CLIENT (PER-SERVER) ────────────────────────────────────────────────

// Client manages a single MCP server subprocess.
type Client struct {
	name    string
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  *bufio.Scanner
	mu      sync.Mutex
	nextID  atomic.Int64
	logger  *slog.Logger
	tools   []MCPToolDef
	running bool
}

// NewClient starts an MCP server subprocess and initializes the connection.
func NewClient(ctx context.Context, cfg ServerConfig, logger *slog.Logger) (*Client, error) {
	cmd := exec.CommandContext(ctx, cfg.Command, cfg.Args...)

	// Merge environment.
	cmd.Env = os.Environ()
	for k, v := range cfg.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("mcpbridge: stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("mcpbridge: stdout pipe: %w", err)
	}

	// Capture stderr for debugging.
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("mcpbridge: start %s: %w", cfg.Name, err)
	}

	c := &Client{
		name:    cfg.Name,
		cmd:     cmd,
		stdin:   stdin,
		stdout:  bufio.NewScanner(stdout),
		logger:  logger,
		running: true,
	}

	// Set a larger buffer for scanner (some MCP servers return large tool lists).
	c.stdout.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	// Initialize the MCP connection.
	if err := c.initialize(ctx); err != nil {
		c.Close()
		return nil, fmt.Errorf("mcpbridge: initialize %s: %w", cfg.Name, err)
	}

	// Discover tools.
	tools, err := c.listTools(ctx)
	if err != nil {
		c.Close()
		return nil, fmt.Errorf("mcpbridge: list tools %s: %w", cfg.Name, err)
	}
	c.tools = tools

	logger.Info("MCP server connected",
		"name", cfg.Name,
		"tools", len(tools),
		"pid", cmd.Process.Pid,
	)

	return c, nil
}

func (c *Client) initialize(ctx context.Context) error {
	resp, err := c.call(ctx, "initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "zbot",
			"version": "1.0.0",
		},
	})
	if err != nil {
		return err
	}
	_ = resp // We don't need the server capabilities yet.

	// Send initialized notification (no response expected).
	return c.notify(ctx, "notifications/initialized", nil)
}

func (c *Client) listTools(ctx context.Context) ([]MCPToolDef, error) {
	resp, err := c.call(ctx, "tools/list", nil)
	if err != nil {
		return nil, err
	}

	var result toolsListResult
	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, fmt.Errorf("parse tools/list: %w", err)
	}
	return result.Tools, nil
}

// CallTool invokes an MCP tool and returns the text result.
func (c *Client) CallTool(ctx context.Context, name string, args map[string]any) (string, bool, error) {
	resp, err := c.call(ctx, "tools/call", map[string]any{
		"name":      name,
		"arguments": args,
	})
	if err != nil {
		return "", true, err
	}

	var result callToolResult
	if err := json.Unmarshal(resp, &result); err != nil {
		return "", true, fmt.Errorf("parse tools/call: %w", err)
	}

	// Concatenate all text content blocks.
	var sb strings.Builder
	for _, c := range result.Content {
		if c.Type == "text" {
			if sb.Len() > 0 {
				sb.WriteString("\n")
			}
			sb.WriteString(c.Text)
		}
	}

	return sb.String(), result.IsError, nil
}

// call sends a JSON-RPC request and waits for the response.
func (c *Client) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	id := c.nextID.Add(1)
	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	// Write request + newline.
	if _, err := c.stdin.Write(append(data, '\n')); err != nil {
		return nil, fmt.Errorf("write to MCP server: %w", err)
	}

	// Read response lines until we get one with our ID.
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if !c.stdout.Scan() {
			if err := c.stdout.Err(); err != nil {
				return nil, fmt.Errorf("read from MCP server: %w", err)
			}
			return nil, fmt.Errorf("MCP server closed stdout")
		}

		line := c.stdout.Text()
		if line == "" {
			continue
		}

		var resp jsonRPCResponse
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			// Could be a notification or log line — skip.
			c.logger.Debug("MCP non-JSON line", "server", c.name, "line", line[:min(len(line), 200)])
			continue
		}

		// Skip notifications (no ID).
		if resp.ID == 0 && resp.Result == nil && resp.Error == nil {
			continue
		}

		if resp.ID != id {
			// Response for a different request — shouldn't happen in sync mode but skip.
			continue
		}

		if resp.Error != nil {
			return nil, fmt.Errorf("MCP error %d: %s", resp.Error.Code, resp.Error.Message)
		}

		return resp.Result, nil
	}

	return nil, fmt.Errorf("MCP server timeout (30s)")
}

// notify sends a JSON-RPC notification (no response expected).
func (c *Client) notify(ctx context.Context, method string, params any) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      0, // notifications don't have an ID in MCP
		Method:  method,
		Params:  params,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal notification: %w", err)
	}

	_, err = c.stdin.Write(append(data, '\n'))
	return err
}

// Close shuts down the MCP server subprocess.
func (c *Client) Close() error {
	c.running = false
	if c.stdin != nil {
		c.stdin.Close()
	}
	if c.cmd != nil && c.cmd.Process != nil {
		c.cmd.Process.Kill()
		c.cmd.Wait()
	}
	c.logger.Info("MCP server stopped", "name", c.name)
	return nil
}

// Tools returns the discovered MCP tool definitions.
func (c *Client) Tools() []MCPToolDef {
	return c.tools
}

// ─── ZBOT TOOL WRAPPER ──────────────────────────────────────────────────────

// MCPTool wraps a single MCP tool as a ZBOT agent.Tool.
type MCPTool struct {
	client *Client
	def    MCPToolDef
}

func (t *MCPTool) Name() string { return t.def.Name }

func (t *MCPTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        t.def.Name,
		Description: t.def.Description,
		InputSchema: t.def.InputSchema,
	}
}

func (t *MCPTool) Execute(ctx context.Context, input map[string]any) (*agent.ToolResult, error) {
	content, isError, err := t.client.CallTool(ctx, t.def.Name, input)
	if err != nil {
		return &agent.ToolResult{
			Content: fmt.Sprintf("MCP tool %s error: %v", t.def.Name, err),
			IsError: true,
		}, nil
	}

	// Truncate very large responses to stay within context limits.
	if len(content) > 100*1024 {
		content = content[:100*1024] + "\n[TRUNCATED — response exceeds 100KB]"
	}

	return &agent.ToolResult{
		Content: content,
		IsError: isError,
	}, nil
}

var _ agent.Tool = (*MCPTool)(nil)

// ─── ZBOT SKILL WRAPPER ─────────────────────────────────────────────────────

// Skill wraps an MCP server's tools as a ZBOT skill.
type Skill struct {
	name        string
	description string
	client      *Client
	tools       []agent.Tool
}

// NewSkill creates a ZBOT skill from a connected MCP client.
func NewSkill(client *Client, description string) *Skill {
	tools := make([]agent.Tool, len(client.tools))
	for i, def := range client.tools {
		tools[i] = &MCPTool{client: client, def: def}
	}

	desc := description
	if desc == "" {
		desc = fmt.Sprintf("MCP server: %s (%d tools)", client.name, len(tools))
	}

	return &Skill{
		name:        client.name,
		description: desc,
		client:      client,
		tools:       tools,
	}
}

func (s *Skill) Name() string        { return s.name }
func (s *Skill) Description() string { return s.description }
func (s *Skill) Tools() []agent.Tool { return s.tools }

func (s *Skill) SystemPromptAddendum() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("### %s (MCP, %d tools)\n", s.name, len(s.tools)))
	for _, t := range s.tools {
		def := t.Definition()
		sb.WriteString(fmt.Sprintf("- %s: %s\n", def.Name, truncate(def.Description, 80)))
	}
	return sb.String()
}

// Close shuts down the underlying MCP server.
func (s *Skill) Close() error {
	return s.client.Close()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
