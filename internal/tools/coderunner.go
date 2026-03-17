// Package tools — CodeRunnerTool executes code in isolated Docker containers.
// Every execution gets a disposable container. Container is destroyed after use.
// Supported: Python 3, Go, JavaScript (Node), Bash (restricted).
package tools

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/zbot-ai/zbot/internal/agent"
)

// CodeRunnerTool runs code in a sandboxed Docker container.
type CodeRunnerTool struct {
	workspaceRoot string // host path mounted read-write in container
}

func NewCodeRunnerTool(workspaceRoot string) *CodeRunnerTool {
	return &CodeRunnerTool{workspaceRoot: workspaceRoot}
}

func (t *CodeRunnerTool) Name() string { return "run_code" }

func (t *CodeRunnerTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "run_code",
		Description: "Execute code in a secure sandboxed environment. Supports Python 3, Go, JavaScript (Node.js), and Bash. Files written to /workspace are accessible via file_read/file_write tools.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"language": map[string]any{
					"type":        "string",
					"enum":        []string{"python", "go", "javascript", "bash"},
					"description": "Programming language to execute",
				},
				"code": map[string]any{
					"type":        "string",
					"description": "The code to execute",
				},
				"timeout_seconds": map[string]any{
					"type":        "integer",
					"description": "Maximum execution time in seconds (default 30, max 120)",
					"default":     30,
				},
			},
			"required": []string{"language", "code"},
		},
	}
}

// languageConfig maps language name → Docker image + run command.
var languageConfig = map[string]struct {
	image   string
	command []string
}{
	"python":     {image: "python:3.12-slim", command: []string{"python3", "-c"}},
	"go":         {image: "golang:1.22-alpine", command: []string{"go", "run", "-"}},
	"javascript": {image: "node:20-alpine", command: []string{"node", "-e"}},
	"bash":       {image: "alpine:3.19", command: []string{"sh", "-c"}},
}

func (t *CodeRunnerTool) Execute(ctx context.Context, input map[string]any) (*agent.ToolResult, error) {
	language, _ := input["language"].(string)
	code, _ := input["code"].(string)
	timeoutSec := 30
	if ts, ok := input["timeout_seconds"].(float64); ok {
		timeoutSec = int(ts)
	}
	if timeoutSec > 120 {
		timeoutSec = 120
	}

	cfg, ok := languageConfig[language]
	if !ok {
		return &agent.ToolResult{
			Content: fmt.Sprintf("unsupported language: %q. Supported: python, go, javascript, bash", language),
			IsError: true,
		}, nil
	}

	if code == "" {
		return &agent.ToolResult{Content: "error: code is required", IsError: true}, nil
	}

	// Build the docker run command.
	// Security constraints:
	//   --rm                : destroy container immediately after exit
	//   --network=none      : no internet access (tools that need it override this)
	//   --memory=512m       : 512MB RAM limit
	//   --cpus=1            : 1 CPU core max
	//   --read-only         : read-only root filesystem
	//   --tmpfs /tmp        : writeable temp dir only
	//   --user=1000:1000    : non-root user
	//   -v workspace:/workspace : project files accessible
	dockerArgs := []string{
		"run", "--rm",
		"--network=none",
		"--memory=512m",
		"--cpus=1",
		"--read-only",
		"--tmpfs", "/tmp:rw,noexec,nosuid,size=64m",
		"--user=1000:1000",
		"-v", t.workspaceRoot + ":/workspace:rw",
		"--workdir=/workspace",
		cfg.image,
	}
	dockerArgs = append(dockerArgs, cfg.command...)
	dockerArgs = append(dockerArgs, code)

	// Create a timed context for the execution.
	execCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(execCtx, "docker", dockerArgs...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	startTime := time.Now()
	runErr := cmd.Run()
	duration := time.Since(startTime)

	// Build result output.
	var result strings.Builder
	result.WriteString(fmt.Sprintf("**Language:** %s | **Duration:** %dms\n\n", language, duration.Milliseconds()))

	if stdout.Len() > 0 {
		result.WriteString("**Output:**\n```\n")
		output := stdout.String()
		if len(output) > 10000 {
			output = output[:10000] + "\n[OUTPUT TRUNCATED]"
		}
		result.WriteString(output)
		result.WriteString("\n```\n")
	}

	if stderr.Len() > 0 {
		result.WriteString("**Stderr:**\n```\n")
		result.WriteString(stderr.String())
		result.WriteString("\n```\n")
	}

	isError := false
	if runErr != nil {
		if execCtx.Err() == context.DeadlineExceeded {
			result.WriteString(fmt.Sprintf("\n⏱️ **Timeout:** execution exceeded %ds limit", timeoutSec))
		} else {
			result.WriteString(fmt.Sprintf("\n❌ **Exit error:** %v", runErr))
		}
		isError = true
	}

	return &agent.ToolResult{
		Content: result.String(),
		IsError: isError,
	}, nil
}
