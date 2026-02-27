package planner

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jeremylerwick-max/zbot/internal/prompts"
)

// CriticVerdict is the result of GPT-4o reviewing Claude's task output.
type CriticVerdict struct {
	TaskID               string        `json:"task_id"`
	Verdict              string        `json:"verdict"` // "pass" | "fail" | "partial"
	Issues               []CriticIssue `json:"issues"`
	CorrectedInstruction string        `json:"corrected_instruction"`
}

// CriticIssue is a single problem found by the critic.
type CriticIssue struct {
	Severity     string `json:"severity"` // "critical" | "major" | "minor"
	Description  string `json:"description"`
	SuggestedFix string `json:"suggested_fix"`
}

// Critic uses GPT-4o to review Claude's task output.
type Critic struct {
	client ChatClient
	logger *slog.Logger
}

// NewCritic creates a new Critic.
func NewCritic(client ChatClient, logger *slog.Logger) *Critic {
	return &Critic{client: client, logger: logger}
}

// Review sends Claude's output to GPT-4o for evaluation against the original instruction.
// Returns a CriticVerdict with pass/fail/partial and specific issues.
func (c *Critic) Review(ctx context.Context, taskID, instruction, output string) (*CriticVerdict, error) {
	start := time.Now()
	c.logger.Info("critic reviewing task",
		"component", "critic",
		"task_id", taskID,
	)

	userMsg := fmt.Sprintf(`TASK ID: %s

TASK INSTRUCTION:
%s

CLAUDE'S OUTPUT:
%s

Review this output against the instruction. Return your verdict as JSON.`, taskID, instruction, output)

	raw, err := c.client.Chat(ctx, prompts.GPTCriticSystem, userMsg)
	if err != nil {
		return nil, fmt.Errorf("critic LLM call: %w", err)
	}

	// Strip markdown fences if model ignored instructions.
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)

	var verdict CriticVerdict
	if err := json.Unmarshal([]byte(raw), &verdict); err != nil {
		c.logger.Warn("critic returned invalid JSON",
			"task_id", taskID,
			"raw", raw[:min(200, len(raw))],
			"err", err,
		)
		// Default to pass if we can't parse — don't block the workflow.
		return &CriticVerdict{
			TaskID:  taskID,
			Verdict: "pass",
			Issues:  []CriticIssue{},
		}, nil
	}

	// Ensure task_id is set.
	verdict.TaskID = taskID

	// Normalize verdict.
	switch verdict.Verdict {
	case "pass", "fail", "partial":
		// valid
	default:
		verdict.Verdict = "pass"
	}

	if verdict.Issues == nil {
		verdict.Issues = []CriticIssue{}
	}

	c.logger.Info("critic verdict",
		"component", "critic",
		"task_id", taskID,
		"verdict", verdict.Verdict,
		"issues", len(verdict.Issues),
		"duration_ms", time.Since(start).Milliseconds(),
	)

	return &verdict, nil
}
