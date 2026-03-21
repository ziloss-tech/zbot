// Package parallel implements the orchestrated parallel coding pattern.
//
// Architecture (from ORCHESTRATED_PARALLEL_CODING.md):
//   1. An expensive orchestrator (Opus/Sonnet) produces a TaskManifest
//      containing bounded, context-pinned coding tasks with test specs.
//   2. N cheap/local coding models (Qwen, DeepSeek, Codestral via Ollama
//      or any OpenAI-compatible endpoint) execute tasks in parallel.
//   3. An integration runner collects results, runs tests, and reports
//      failures back for correction.
//
// The orchestrator's job is to REDUCE each task's context requirement
// to fit inside a small model's window. Tests are the contract.
//
// Credits:
//   This pattern is inspired by how large human engineering teams work:
//   architect designs specs, engineers implement in parallel, CI catches
//   integration failures. We just replace "20 engineers" with "20 Qwen
//   instances on a Mac Studio."
package parallel

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ziloss-tech/zbot/internal/agent"
)

// ─── TASK MANIFEST ──────────────────────────────────────────────────────────

// TaskManifest is produced by the orchestrator (Opus/Sonnet) and consumed
// by the parallel dispatcher. It contains everything a cheap coder needs.
type TaskManifest struct {
	// ProjectName is the top-level identifier.
	ProjectName string `json:"project_name"`

	// BaseDir is the workspace root where files are written.
	BaseDir string `json:"base_dir"`

	// SharedContext is read by ALL coders — interfaces, types, constants.
	// Keep this small. The orchestrator's job is to minimize this.
	SharedContext []ContextFile `json:"shared_context"`

	// Tasks are the individual coding assignments.
	Tasks []CodingTask `json:"tasks"`

	// CreatedAt is when the orchestrator produced this manifest.
	CreatedAt time.Time `json:"created_at"`

	// OrchestratorModel records which model produced the manifest.
	OrchestratorModel string `json:"orchestrator_model"`
}

// ContextFile is a file that provides context to the coder.
type ContextFile struct {
	Path    string `json:"path"`    // relative to BaseDir
	Content string `json:"content"` // file content (pre-loaded by orchestrator)
}

// CodingTask is a single bounded coding assignment for a cheap model.
type CodingTask struct {
	// ID is a unique task identifier.
	ID string `json:"id"`

	// OutputFile is the file to create (relative to BaseDir).
	OutputFile string `json:"output_file"`

	// Instruction tells the coder exactly what to implement.
	// Should reference the interface/trait it implements and the tests it must pass.
	Instruction string `json:"instruction"`

	// AdditionalContext are task-specific files beyond SharedContext.
	AdditionalContext []ContextFile `json:"additional_context,omitempty"`

	// TestFile is the test file that defines "done."
	// If tests pass, the task is complete.
	TestFile string `json:"test_file,omitempty"`

	// TestCommand is how to verify this task (e.g. "go test ./internal/foo/").
	TestCommand string `json:"test_command,omitempty"`

	// MaxRetries is how many times to re-prompt the coder on failure.
	MaxRetries int `json:"max_retries,omitempty"`

	// DependsOn lists task IDs that must complete before this one starts.
	DependsOn []string `json:"depends_on,omitempty"`
}

// ─── TASK RESULT ────────────────────────────────────────────────────────────

// TaskResult is the output of a single coding task.
type TaskResult struct {
	TaskID     string        `json:"task_id"`
	OutputFile string        `json:"output_file"`
	Code       string        `json:"code"`
	Status     string        `json:"status"` // "success", "failed", "timeout"
	Error      string        `json:"error,omitempty"`
	Attempts   int           `json:"attempts"`
	Duration   time.Duration `json:"duration"`
	Model      string        `json:"model"`
	Tokens     int           `json:"tokens,omitempty"`
}

// ─── DISPATCHER ─────────────────────────────────────────────────────────────

// Dispatcher manages parallel coding task execution.
type Dispatcher struct {
	coderClient agent.LLMClient // cheap/local model (Qwen via Ollama)
	maxParallel int             // max concurrent coding tasks
	logger      *slog.Logger
}

// NewDispatcher creates a parallel coding dispatcher.
// coderClient should be a cheap/local model (e.g. Qwen 32B via Ollama).
// maxParallel controls concurrency (limited by RAM — each Qwen instance
// uses ~20GB on MLX, so 512GB RAM ÷ 20GB = ~25 max, but Ollama serializes).
func NewDispatcher(coderClient agent.LLMClient, maxParallel int, logger *slog.Logger) *Dispatcher {
	if maxParallel <= 0 {
		maxParallel = 4
	}
	return &Dispatcher{
		coderClient: coderClient,
		maxParallel: maxParallel,
		logger:      logger,
	}
}

// Run executes all tasks in the manifest, respecting dependencies and
// concurrency limits. Returns results for all tasks.
func (d *Dispatcher) Run(ctx context.Context, manifest TaskManifest) ([]TaskResult, error) {
	d.logger.Info("parallel dispatch starting",
		"project", manifest.ProjectName,
		"tasks", len(manifest.Tasks),
		"max_parallel", d.maxParallel,
		"coder_model", d.coderClient.ModelName(),
	)

	// Ensure base directory exists.
	if err := os.MkdirAll(manifest.BaseDir, 0o750); err != nil {
		return nil, fmt.Errorf("create base dir: %w", err)
	}

	// Write shared context files.
	for _, cf := range manifest.SharedContext {
		path := filepath.Join(manifest.BaseDir, cf.Path)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			return nil, fmt.Errorf("create dir for %s: %w", cf.Path, err)
		}
		if err := os.WriteFile(path, []byte(cf.Content), 0o644); err != nil {
			return nil, fmt.Errorf("write shared context %s: %w", cf.Path, err)
		}
	}

	// Build dependency graph.
	taskMap := make(map[string]*CodingTask)
	for i := range manifest.Tasks {
		taskMap[manifest.Tasks[i].ID] = &manifest.Tasks[i]
	}

	// Track completed tasks.
	var mu sync.Mutex
	completed := make(map[string]bool)
	results := make([]TaskResult, 0, len(manifest.Tasks))

	// Semaphore for concurrency control.
	sem := make(chan struct{}, d.maxParallel)

	// Process tasks in waves (respecting dependencies).
	remaining := len(manifest.Tasks)
	for remaining > 0 {
		// Find tasks whose dependencies are met.
		var ready []*CodingTask
		mu.Lock()
		for i := range manifest.Tasks {
			t := &manifest.Tasks[i]
			if completed[t.ID] {
				continue
			}
			depsOK := true
			for _, dep := range t.DependsOn {
				if !completed[dep] {
					depsOK = false
					break
				}
			}
			if depsOK {
				ready = append(ready, t)
			}
		}
		mu.Unlock()

		if len(ready) == 0 {
			// Deadlock — remaining tasks have unmet dependencies.
			d.logger.Error("dependency deadlock", "remaining", remaining)
			break
		}

		// Dispatch ready tasks in parallel.
		var wg sync.WaitGroup
		for _, task := range ready {
			task := task // capture
			wg.Add(1)
			sem <- struct{}{} // acquire semaphore

			go func() {
				defer wg.Done()
				defer func() { <-sem }() // release semaphore

				result := d.executeTask(ctx, manifest, *task)

				mu.Lock()
				results = append(results, result)
				completed[task.ID] = true
				remaining--
				mu.Unlock()

				status := "✅"
				if result.Status != "success" {
					status = "❌"
				}
				d.logger.Info("task complete",
					"task", task.ID,
					"status", status,
					"file", task.OutputFile,
					"attempts", result.Attempts,
					"duration", result.Duration.Round(time.Millisecond),
				)
			}()
		}
		wg.Wait()
	}

	// Summary.
	success := 0
	for _, r := range results {
		if r.Status == "success" {
			success++
		}
	}
	d.logger.Info("parallel dispatch complete",
		"project", manifest.ProjectName,
		"total", len(manifest.Tasks),
		"success", success,
		"failed", len(manifest.Tasks)-success,
	)

	return results, nil
}

// executeTask runs a single coding task with retries.
func (d *Dispatcher) executeTask(ctx context.Context, manifest TaskManifest, task CodingTask) TaskResult {
	start := time.Now()
	maxRetries := task.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 2
	}

	// Build the prompt with all context.
	prompt := d.buildPrompt(manifest, task)

	var lastError string
	for attempt := 1; attempt <= maxRetries+1; attempt++ {
		messages := []agent.Message{
			{Role: agent.RoleSystem, Content: coderSystemPrompt},
			{Role: agent.RoleUser, Content: prompt},
		}

		// If retrying, add the error feedback.
		if attempt > 1 && lastError != "" {
			messages = append(messages, agent.Message{
				Role:    agent.RoleAssistant,
				Content: "I'll fix the code based on the error.",
			})
			messages = append(messages, agent.Message{
				Role:    agent.RoleUser,
				Content: fmt.Sprintf("The previous code failed:\n\n```\n%s\n```\n\nFix the code. Output ONLY the corrected Go file, no explanation.", lastError),
			})
		}

		result, err := d.coderClient.Complete(ctx, messages, nil)
		if err != nil {
			lastError = fmt.Sprintf("LLM error: %v", err)
			continue
		}

		// Extract code from response (strip markdown fences if present).
		code := extractCode(result.Content)

		// Write the file.
		outputPath := filepath.Join(manifest.BaseDir, task.OutputFile)
		if err := os.MkdirAll(filepath.Dir(outputPath), 0o750); err != nil {
			lastError = fmt.Sprintf("mkdir error: %v", err)
			continue
		}
		if err := os.WriteFile(outputPath, []byte(code), 0o644); err != nil {
			lastError = fmt.Sprintf("write error: %v", err)
			continue
		}

		// If there's a test command, run it to verify.
		if task.TestCommand != "" {
			testResult := runTest(ctx, manifest.BaseDir, task.TestCommand)
			if !testResult.Passed {
				lastError = testResult.Output
				d.logger.Info("task test failed, retrying",
					"task", task.ID,
					"attempt", attempt,
					"error_preview", truncateStr(lastError, 200),
				)
				continue
			}
		}

		// Success!
		return TaskResult{
			TaskID:     task.ID,
			OutputFile: task.OutputFile,
			Code:       code,
			Status:     "success",
			Attempts:   attempt,
			Duration:   time.Since(start),
			Model:      d.coderClient.ModelName(),
			Tokens:     result.InputTokens + result.OutputTokens,
		}
	}

	return TaskResult{
		TaskID:     task.ID,
		OutputFile: task.OutputFile,
		Status:     "failed",
		Error:      lastError,
		Attempts:   maxRetries + 1,
		Duration:   time.Since(start),
		Model:      d.coderClient.ModelName(),
	}
}

// buildPrompt constructs the full coding prompt with all context.
func (d *Dispatcher) buildPrompt(manifest TaskManifest, task CodingTask) string {
	var sb strings.Builder

	sb.WriteString("## Project: " + manifest.ProjectName + "\n\n")

	// Shared context.
	if len(manifest.SharedContext) > 0 {
		sb.WriteString("## Shared Context Files\n\n")
		for _, cf := range manifest.SharedContext {
			sb.WriteString(fmt.Sprintf("### %s\n```go\n%s\n```\n\n", cf.Path, cf.Content))
		}
	}

	// Task-specific context.
	if len(task.AdditionalContext) > 0 {
		sb.WriteString("## Additional Context\n\n")
		for _, cf := range task.AdditionalContext {
			sb.WriteString(fmt.Sprintf("### %s\n```go\n%s\n```\n\n", cf.Path, cf.Content))
		}
	}

	// The instruction.
	sb.WriteString("## Your Task\n\n")
	sb.WriteString(fmt.Sprintf("**Output file:** `%s`\n\n", task.OutputFile))
	sb.WriteString(task.Instruction)
	sb.WriteString("\n\n")

	if task.TestFile != "" {
		sb.WriteString(fmt.Sprintf("**Test file:** `%s`\n", task.TestFile))
		sb.WriteString(fmt.Sprintf("**Test command:** `%s`\n", task.TestCommand))
		sb.WriteString("Your code MUST pass these tests.\n\n")
	}

	sb.WriteString("Output ONLY the complete Go source file. No explanation, no markdown fences, just the code.")

	return sb.String()
}

const coderSystemPrompt = `You are a Go programmer. You receive a task with context files and an instruction.
You output ONLY the complete Go source file — nothing else. No explanation, no markdown,
no commentary. Just valid, compilable Go code.

Rules:
- Match the package name from the context
- Implement all interfaces referenced in the instruction
- Handle errors properly (no ignoring err returns)
- Include necessary imports
- If tests are provided, make sure your code passes them`

// ─── HELPERS ────────────────────────────────────────────────────────────────

// extractCode strips markdown code fences from LLM output.
func extractCode(raw string) string {
	raw = strings.TrimSpace(raw)

	// Remove ```go ... ``` fences.
	if strings.HasPrefix(raw, "```") {
		lines := strings.Split(raw, "\n")
		// Remove first line (```go or ```)
		if len(lines) > 1 {
			lines = lines[1:]
		}
		// Remove last line if it's ```
		if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "```" {
			lines = lines[:len(lines)-1]
		}
		raw = strings.Join(lines, "\n")
	}

	return strings.TrimSpace(raw)
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// ─── TEST RUNNER ────────────────────────────────────────────────────────────

type testResult struct {
	Passed bool
	Output string
}

func runTest(ctx context.Context, baseDir, command string) testResult {
	// Use os/exec to run the test command.
	// Import is at the top level but we keep this simple.
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return testResult{Passed: true}
	}

	cmd := execCommand(ctx, parts[0], parts[1:]...)
	cmd.Dir = baseDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return testResult{Passed: false, Output: string(output)}
	}
	return testResult{Passed: true, Output: string(output)}
}

// ─── MANIFEST I/O ───────────────────────────────────────────────────────────

// SaveManifest writes a task manifest to a JSON file.
func SaveManifest(manifest TaskManifest, path string) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// LoadManifest reads a task manifest from a JSON file.
func LoadManifest(path string) (*TaskManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m TaskManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return &m, nil
}
