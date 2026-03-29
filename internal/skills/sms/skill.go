// Package sms implements the Twilio SMS notification skill for ZBOT.
// Enables the agent to send text messages — used for daily summaries,
// alerts, and proactive notifications.
package sms

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/ziloss-tech/zbot/internal/agent"
)

// Skill provides send_sms tool.
type Skill struct {
	accountSID string
	authToken  string
	fromNumber string
}

// NewSkill creates an SMS skill with Twilio credentials.
func NewSkill(accountSID, authToken, fromNumber string) *Skill {
	return &Skill{accountSID: accountSID, authToken: authToken, fromNumber: fromNumber}
}

func (s *Skill) Name() string        { return "sms" }
func (s *Skill) Description() string { return "Send SMS via Twilio" }
func (s *Skill) Tools() []agent.Tool { return []agent.Tool{&SendSMSTool{skill: s}} }

func (s *Skill) SystemPromptAddendum() string {
	return `### SMS Notifications
You can send text messages using send_sms. Use this for:
- Daily summaries and reports
- Urgent alerts (bills due, anomalies detected)
- Task completion notifications
- Proactive updates about scheduled jobs
Default recipient is Jeremy: +19492300036
Keep messages concise — SMS has a 1600 char limit.`
}

// ─── SEND SMS TOOL ────────────────────────────────────────────────────────────

type SendSMSTool struct{ skill *Skill }

func (t *SendSMSTool) Name() string { return "send_sms" }

func (t *SendSMSTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "send_sms",
		Description: "Send an SMS text message via Twilio. Use for notifications, alerts, and daily summaries.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"to":      map[string]any{"type": "string", "description": "Phone number in E.164 format (e.g. +19492300036). Defaults to Jeremy if empty."},
				"message": map[string]any{"type": "string", "description": "Message text (max 1600 chars)"},
			},
			"required": []string{"message"},
		},
	}
}

func (t *SendSMSTool) Execute(ctx context.Context, input map[string]any) (*agent.ToolResult, error) {
	to, _ := input["to"].(string)
	message, _ := input["message"].(string)

	if message == "" {
		return &agent.ToolResult{Content: "Error: message is required", IsError: true}, nil
	}
	if len(message) > 1600 {
		message = message[:1600]
	}
	// Default to Jeremy's number
	if to == "" {
		to = "+19492300036"
	}

	// Twilio REST API — no SDK needed, just an HTTP POST
	apiURL := fmt.Sprintf("https://api.twilio.com/2010-04-01/Accounts/%s/Messages.json", t.skill.accountSID)
	data := url.Values{}
	data.Set("To", to)
	data.Set("From", t.skill.fromNumber)
	data.Set("Body", message)

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, strings.NewReader(data.Encode()))
	if err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("Error creating request: %v", err), IsError: true}, nil
	}
	req.SetBasicAuth(t.skill.accountSID, t.skill.authToken)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("Error sending SMS: %v", err), IsError: true}, nil
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return &agent.ToolResult{Content: fmt.Sprintf("Twilio error (%d): %s", resp.StatusCode, string(body)[:200]), IsError: true}, nil
	}

	return &agent.ToolResult{Content: fmt.Sprintf("SMS sent to %s: %s", to, message[:min(len(message), 50)])}, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
