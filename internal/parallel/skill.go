package parallel

import (
	"context"
	"encoding/json"
	"time"
	"fmt"
	"strings"

	"github.com/ziloss-tech/zbot/internal/agent"
)

// ─── PARALLEL CODING SKILL ──────────────────────────────────────────────────

// Skill wraps the parallel dispatcher as a ZBOT skill.
type Skill struct {
	dispatcher *Dispatcher
}

// NewSkill creates a parallel coding skill.
func NewSkill(dispatcher *Dispatcher) *Skill {
	return &Skill{dispatcher: dispatcher}
}

func (s *Skill) Name() string        { return "parallel_code" }
func (s *Skill) Description() string { return "Parallel coding — dispatch tasks to local Qwen models" }

func (s *Skill) Tools() []agent.Tool {
	return []agent.Tool{
		&DispatchTool{dispatcher: s.dispatcher},
		&ManifestStatusTool{},
	}
}

func (s *Skill) SystemPromptAddendum() string {
	return `### Parallel Coding (2 tools)
You can dispatch coding tasks to local Qwen models running on Ollama.
- parallel_dispatch: Execute a task manifest (JSON) — runs tasks in parallel on cheap local models.
- parallel_status: Check the status of a completed manifest run.

Workflow: You (the orchestrator) design the architecture, write interfaces, write tests,
then produce a TaskManifest JSON. The dispatcher farms individual files to Qwen Coder.
Qwen implements each file in parallel. Tests verify correctness.`
}

// ─── DISPATCH TOOL ──────────────────────────────────────────────────────────

type DispatchTool struct {
	dispatcher *Dispatcher
}

func (t *DispatchTool) Name() string { return "parallel_dispatch" }
func (t *DispatchTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "parallel_dispatch",
		Description: "Execute a parallel coding task manifest. Farms individual coding tasks to local Qwen models. Returns results for all tasks.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"manifest"},
			"properties": map[string]any{
				"manifest": map[string]any{
					"type":        "object",
					"description": "TaskManifest JSON with project_name, base_dir, shared_context, and tasks array",
				},
			},
		},
	}
}

func (t *DispatchTool) Execute(ctx context.Context, input map[string]any) (*agent.ToolResult, error) {
	manifestRaw, ok := input["manifest"]
	if !ok {
		return &agent.ToolResult{Content: "error: manifest is required", IsError: true}, nil
	}

	// Marshal and unmarshal to get a proper TaskManifest.
	data, err := json.Marshal(manifestRaw)
	if err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("error: invalid manifest: %v", err), IsError: true}, nil
	}

	var manifest TaskManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("error: parse manifest: %v", err), IsError: true}, nil
	}

	if len(manifest.Tasks) == 0 {
		return &agent.ToolResult{Content: "error: manifest has no tasks", IsError: true}, nil
	}

	results, err := t.dispatcher.Run(ctx, manifest)
	if err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("dispatch error: %v", err), IsError: true}, nil
	}

	// Format results summary.
	var sb strings.Builder
	success, failed := 0, 0
	for _, r := range results {
		if r.Status == "success" {
			success++
		} else {
			failed++
		}
	}
	sb.WriteString("## Parallel Dispatch Results\n\n")
	sb.WriteString(fmt.Sprintf("**Project:** %s\n", manifest.ProjectName))
	sb.WriteString(fmt.Sprintf("**Model:** %s\n", t.dispatcher.coderClient.ModelName()))
	sb.WriteString(fmt.Sprintf("**Tasks:** %d total, %d success, %d failed\n\n", len(results), success, failed))

	for _, r := range results {
		icon := "✅"
		if r.Status != "success" {
			icon = "❌"
		}
		sb.WriteString(fmt.Sprintf("%s **%s** → `%s` (%d attempts, %s)\n", icon, r.TaskID, r.OutputFile, r.Attempts, r.Duration.Round(time.Millisecond)))
		if r.Error != "" {
			sb.WriteString(fmt.Sprintf("   Error: %s\n", truncateStr(r.Error, 200)))
		}
	}

	return &agent.ToolResult{Content: sb.String()}, nil
}

var _ agent.Tool = (*DispatchTool)(nil)

// ─── MANIFEST STATUS TOOL ───────────────────────────────────────────────────

type ManifestStatusTool struct{}

func (t *ManifestStatusTool) Name() string { return "parallel_status" }
func (t *ManifestStatusTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "parallel_status",
		Description: "Load and display the status of a task manifest file.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"path"},
			"properties": map[string]any{
				"path": map[string]any{"type": "string", "description": "Path to the manifest JSON file"},
			},
		},
	}
}

func (t *ManifestStatusTool) Execute(ctx context.Context, input map[string]any) (*agent.ToolResult, error) {
	path, _ := input["path"].(string)
	if path == "" {
		return &agent.ToolResult{Content: "error: path is required", IsError: true}, nil
	}

	manifest, err := LoadManifest(path)
	if err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("error: %v", err), IsError: true}, nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Manifest: %s\n\n", manifest.ProjectName))
	sb.WriteString(fmt.Sprintf("**Base dir:** %s\n", manifest.BaseDir))
	sb.WriteString(fmt.Sprintf("**Tasks:** %d\n", len(manifest.Tasks)))
	sb.WriteString(fmt.Sprintf("**Orchestrator:** %s\n", manifest.OrchestratorModel))
	sb.WriteString(fmt.Sprintf("**Created:** %s\n\n", manifest.CreatedAt.Format("2006-01-02 15:04")))

	for _, task := range manifest.Tasks {
		deps := "none"
		if len(task.DependsOn) > 0 {
			deps = strings.Join(task.DependsOn, ", ")
		}
		sb.WriteString(fmt.Sprintf("- **%s** → `%s` (deps: %s)\n", task.ID, task.OutputFile, deps))
	}

	return &agent.ToolResult{Content: sb.String()}, nil
}

var _ agent.Tool = (*ManifestStatusTool)(nil)
