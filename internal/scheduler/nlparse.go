package scheduler

import (
	"context"
	"fmt"
	"strings"

	"github.com/ziloss-tech/zbot/internal/agent"
)

// ParseSchedule converts natural language to a cron expression using Claude.
// Examples:
//
//	"every morning at 8am"       → "0 8 * * *"
//	"every Monday at 9am"        → "0 9 * * 1"
//	"every weekday at 6pm"       → "0 18 * * 1-5"
//	"every hour"                 → "0 * * * *"
//	"every 30 minutes"           → "*/30 * * * *"
func ParseSchedule(ctx context.Context, llm agent.LLMClient, natural string) (string, error) {
	prompt := fmt.Sprintf(`Convert this schedule description to a standard 5-field cron expression.
Respond with ONLY the cron expression, nothing else. No explanation, no markdown.

Examples:
- "every morning at 8am" → 0 8 * * *
- "every Monday at 9am" → 0 9 * * 1
- "every weekday at 6pm" → 0 18 * * 1-5
- "every hour" → 0 * * * *
- "every 30 minutes" → */30 * * * *
- "every day at noon" → 0 12 * * *
- "every Sunday at 10pm" → 0 22 * * 0
- "twice a day at 8am and 6pm" → 0 8,18 * * *

Schedule: "%s"`, natural)

	messages := []agent.Message{
		{Role: agent.RoleUser, Content: prompt},
	}

	result, err := llm.Complete(ctx, messages, nil)
	if err != nil {
		return "", fmt.Errorf("NL schedule parse failed: %w", err)
	}

	// Clean the response — strip whitespace, markdown, etc.
	cronExpr := strings.TrimSpace(result.Content)
	cronExpr = strings.Trim(cronExpr, "`\"'")
	cronExpr = strings.TrimSpace(cronExpr)

	// Validate the cron expression before returning.
	if err := ValidateCron(cronExpr); err != nil {
		return "", fmt.Errorf("LLM returned invalid cron %q: %w", cronExpr, err)
	}

	return cronExpr, nil
}
