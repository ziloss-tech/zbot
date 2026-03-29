// Package factory implements the self-tool-builder for ZBOT.
// This is the crown jewel: Cortex can write Python scripts that become
// registered tools at runtime. ZBOT builds its own capabilities.
//
// Architecture:
//   - Cortex calls create_skill with a name, description, and Python script
//   - Factory writes the script to ~/.zbot/skills/{name}.py
//   - Factory writes a manifest JSON alongside it
//   - A wrapper tool is registered that executes the script
//   - On startup, LoadExistingSkills scans the directory and re-registers all
//
// Why Python scripts instead of Go plugins:
//   - Python can call mlx-whisper, pandas, requests, etc. immediately
//   - No compilation step — instant availability
//   - Cortex already writes good Python
//   - Scripts are inspectable and editable by humans
//   - Go plugin system is fragile and version-sensitive
package factory

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ziloss-tech/zbot/internal/agent"
)

// SkillManifest describes a dynamic skill stored on disk.
type SkillManifest struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	ScriptPath  string            `json:"script_path"`
	InputSchema map[string]any    `json:"input_schema"`
	CreatedAt   time.Time         `json:"created_at"`
	CreatedBy   string            `json:"created_by"` // "cortex" or "user"
	Version     int               `json:"version"`
}

// Factory manages dynamic skill creation and execution.
type Factory struct {
	skillsDir    string // ~/.zbot/skills/
	logger       *slog.Logger
	mu           sync.RWMutex
	dynamicTools map[string]*DynamicTool // name → live tool
}

// NewFactory creates a skill factory. Creates the skills directory if needed.
func NewFactory(skillsDir string, logger *slog.Logger) (*Factory, error) {
	if err := os.MkdirAll(skillsDir, 0755); err != nil {
		return nil, fmt.Errorf("factory: create skills dir: %w", err)
	}
	return &Factory{
		skillsDir:    skillsDir,
		logger:       logger,
		dynamicTools: make(map[string]*DynamicTool),
	}, nil
}

func (f *Factory) Name() string        { return "factory" }
func (f *Factory) Description() string { return "Create and manage dynamic tools at runtime" }

func (f *Factory) Tools() []agent.Tool {
	return []agent.Tool{
		&CreateSkillTool{factory: f},
		&ListSkillsTool{factory: f},
	}
}

func (f *Factory) SystemPromptAddendum() string {
	f.mu.RLock()
	count := len(f.dynamicTools)
	var names []string
	for n := range f.dynamicTools {
		names = append(names, n)
	}
	f.mu.RUnlock()

	addendum := `### Self-Improvement: Skill Factory
You can CREATE NEW TOOLS at runtime using create_skill. When you need a capability
you don't have (transcription, file watching, API integration, data processing),
write a Python script and register it as a tool. The script becomes permanently
available — it persists across restarts.

Guidelines for creating skills:
- Scripts receive input as a JSON string via sys.argv[1]
- Scripts print their result to stdout (text or JSON)
- Scripts should handle errors gracefully and print error messages
- Use subprocess for calling system tools (mlx_whisper, ffmpeg, etc.)
- Use requests/urllib for API calls
- Keep scripts focused — one tool per script
- Name skills clearly: transcribe_audio, check_quickbooks, scan_emails

You can also schedule your own tasks using the existing scheduler.
`
	if count > 0 {
		addendum += fmt.Sprintf("\nCurrently loaded dynamic skills (%d): %s", count, strings.Join(names, ", "))
	}
	return addendum
}

// GetDynamicTools returns all currently registered dynamic tools.
func (f *Factory) GetDynamicTools() []agent.Tool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	tools := make([]agent.Tool, 0, len(f.dynamicTools))
	for _, t := range f.dynamicTools {
		tools = append(tools, t)
	}
	return tools
}

// LoadExistingSkills scans the skills directory and registers all saved skills.
// Called at startup so previously created tools are immediately available.
func (f *Factory) LoadExistingSkills() error {
	entries, err := os.ReadDir(f.skillsDir)
	if err != nil {
		return fmt.Errorf("factory: read skills dir: %w", err)
	}
	loaded := 0
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		manifestPath := filepath.Join(f.skillsDir, entry.Name())
		data, err := os.ReadFile(manifestPath)
		if err != nil {
			f.logger.Warn("factory: skip bad manifest", "path", manifestPath, "err", err)
			continue
		}
		var manifest SkillManifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			f.logger.Warn("factory: skip bad manifest JSON", "path", manifestPath, "err", err)
			continue
		}
		// Verify script exists
		if _, err := os.Stat(manifest.ScriptPath); err != nil {
			f.logger.Warn("factory: script missing", "name", manifest.Name, "path", manifest.ScriptPath)
			continue
		}
		tool := &DynamicTool{manifest: manifest, logger: f.logger}
		f.mu.Lock()
		f.dynamicTools[manifest.Name] = tool
		f.mu.Unlock()
		loaded++
	}
	f.logger.Info("factory: loaded dynamic skills", "count", loaded)
	return nil
}

// ─── DYNAMIC TOOL (wrapper around a Python script) ───────────────────────────

// DynamicTool wraps a Python script as an agent.Tool.
type DynamicTool struct {
	manifest SkillManifest
	logger   *slog.Logger
}

func (t *DynamicTool) Name() string { return t.manifest.Name }

func (t *DynamicTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        t.manifest.Name,
		Description: t.manifest.Description,
		InputSchema: t.manifest.InputSchema,
	}
}

func (t *DynamicTool) Execute(ctx context.Context, input map[string]any) (*agent.ToolResult, error) {
	// Serialize input to JSON for the script
	inputJSON, err := json.Marshal(input)
	if err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("Error serializing input: %v", err), IsError: true}, nil
	}

	// Execute: python3 script.py '{"input": "json"}'
	cmd := exec.CommandContext(ctx, "python3", t.manifest.ScriptPath, string(inputJSON))
	cmd.Env = append(os.Environ(), "PYTHONUNBUFFERED=1")

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.logger.Warn("dynamic skill failed", "name", t.manifest.Name, "err", err, "output", string(output))
		return &agent.ToolResult{
			Content: fmt.Sprintf("Script error: %v\nOutput: %s", err, string(output)),
			IsError: true,
		}, nil
	}

	result := strings.TrimSpace(string(output))
	t.logger.Debug("dynamic skill executed", "name", t.manifest.Name, "output_len", len(result))
	return &agent.ToolResult{Content: result}, nil
}

// ─── CREATE SKILL TOOL ───────────────────────────────────────────────────────

type CreateSkillTool struct{ factory *Factory }

func (t *CreateSkillTool) Name() string { return "create_skill" }

func (t *CreateSkillTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "create_skill",
		Description: "Create a new dynamic tool by writing a Python script. The tool becomes immediately available and persists across restarts. Use this when you need a capability you don't have.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name":        map[string]any{"type": "string", "description": "Tool name (snake_case, e.g. transcribe_audio)"},
				"description": map[string]any{"type": "string", "description": "What this tool does (shown to the LLM)"},
				"script":      map[string]any{"type": "string", "description": "Python script content. Receives input JSON via sys.argv[1], prints result to stdout."},
				"input_schema": map[string]any{"type": "object", "description": "JSON Schema for the tool's input parameters"},
			},
			"required": []string{"name", "description", "script"},
		},
	}
}

func (t *CreateSkillTool) Execute(ctx context.Context, input map[string]any) (*agent.ToolResult, error) {
	name, _ := input["name"].(string)
	desc, _ := input["description"].(string)
	script, _ := input["script"].(string)
	inputSchema, _ := input["input_schema"].(map[string]any)

	if name == "" || script == "" {
		return &agent.ToolResult{Content: "Error: name and script are required", IsError: true}, nil
	}

	// Sanitize name
	name = strings.ReplaceAll(strings.ToLower(name), " ", "_")
	name = strings.ReplaceAll(name, "-", "_")

	// Default input schema if not provided
	if inputSchema == nil {
		inputSchema = map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		}
	}

	// Write script
	scriptPath := filepath.Join(t.factory.skillsDir, name+".py")
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("Error writing script: %v", err), IsError: true}, nil
	}

	// Determine version
	version := 1
	t.factory.mu.RLock()
	if existing, ok := t.factory.dynamicTools[name]; ok {
		version = existing.manifest.Version + 1
	}
	t.factory.mu.RUnlock()

	// Write manifest
	manifest := SkillManifest{
		Name:        name,
		Description: desc,
		ScriptPath:  scriptPath,
		InputSchema: inputSchema,
		CreatedAt:   time.Now(),
		CreatedBy:   "cortex",
		Version:     version,
	}
	manifestData, _ := json.MarshalIndent(manifest, "", "  ")
	manifestPath := filepath.Join(t.factory.skillsDir, name+".json")
	if err := os.WriteFile(manifestPath, manifestData, 0644); err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("Error writing manifest: %v", err), IsError: true}, nil
	}

	// Register tool immediately
	tool := &DynamicTool{manifest: manifest, logger: t.factory.logger}
	t.factory.mu.Lock()
	t.factory.dynamicTools[name] = tool
	t.factory.mu.Unlock()

	t.factory.logger.Info("factory: skill created", "name", name, "version", version, "script", scriptPath)

	return &agent.ToolResult{
		Content: fmt.Sprintf("Skill '%s' created (v%d) and registered. Script: %s\nTool is immediately available for use.", name, version, scriptPath),
	}, nil
}

// ─── LIST SKILLS TOOL ────────────────────────────────────────────────────────

type ListSkillsTool struct{ factory *Factory }

func (t *ListSkillsTool) Name() string { return "list_skills" }

func (t *ListSkillsTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "list_skills",
		Description: "List all dynamic skills that have been created by the factory.",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}
}

func (t *ListSkillsTool) Execute(_ context.Context, _ map[string]any) (*agent.ToolResult, error) {
	t.factory.mu.RLock()
	defer t.factory.mu.RUnlock()

	if len(t.factory.dynamicTools) == 0 {
		return &agent.ToolResult{Content: "No dynamic skills created yet. Use create_skill to build new tools."}, nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Dynamic skills (%d):\n", len(t.factory.dynamicTools)))
	for name, tool := range t.factory.dynamicTools {
		sb.WriteString(fmt.Sprintf("  - %s (v%d): %s\n    Script: %s\n    Created: %s by %s\n",
			name, tool.manifest.Version, tool.manifest.Description,
			tool.manifest.ScriptPath,
			tool.manifest.CreatedAt.Format("2006-01-02 15:04"),
			tool.manifest.CreatedBy,
		))
	}
	return &agent.ToolResult{Content: sb.String()}, nil
}
