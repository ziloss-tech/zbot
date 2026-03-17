// Package email implements the SMTP email skill for ZBOT.
package email

import (
	"context"
	"fmt"
	"net/smtp"
	"strings"

	"github.com/zbot-ai/zbot/internal/agent"
)

// Skill provides send_email with confirmation.
type Skill struct {
	host string
	port int
	user string
	pass string
	from string
}

// NewSkill creates an email skill.
func NewSkill(host string, port int, user, pass, from string) *Skill {
	return &Skill{host: host, port: port, user: user, pass: pass, from: from}
}

func (s *Skill) Name() string        { return "email" }
func (s *Skill) Description() string { return "Send email via SMTP" }
func (s *Skill) Tools() []agent.Tool { return []agent.Tool{&SendEmailTool{skill: s}} }

func (s *Skill) SystemPromptAddendum() string {
	return `### Email
You can send emails on Jeremy's behalf using send_email.
ALWAYS return a preview first and require explicit confirmation before actually sending.
From address: ` + s.from
}

// ─── SEND EMAIL TOOL ──────────────────────────────────────────────────────────

type SendEmailTool struct{ skill *Skill }

func (t *SendEmailTool) Name() string { return "send_email" }
func (t *SendEmailTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "send_email",
		Description: "Send an email. Returns a preview first — call again with confirm=true to actually send.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"to", "subject", "body"},
			"properties": map[string]any{
				"to":      map[string]any{"type": "string", "description": "Recipient email address"},
				"subject": map[string]any{"type": "string", "description": "Email subject"},
				"body":    map[string]any{"type": "string", "description": "Email body (plain text)"},
				"cc":      map[string]any{"type": "string", "description": "CC recipients (comma-separated)"},
				"confirm": map[string]any{"type": "boolean", "description": "Set to true to actually send (false = preview only)"},
			},
		},
	}
}

func (t *SendEmailTool) Execute(ctx context.Context, input map[string]any) (*agent.ToolResult, error) {
	to, _ := input["to"].(string)
	subject, _ := input["subject"].(string)
	body, _ := input["body"].(string)
	cc, _ := input["cc"].(string)
	confirm, _ := input["confirm"].(bool)

	if to == "" || subject == "" || body == "" {
		return &agent.ToolResult{Content: "error: to, subject, and body are required", IsError: true}, nil
	}

	// Preview mode.
	if !confirm {
		preview := fmt.Sprintf("📧 Email Preview\nFrom: %s\nTo: %s\n", t.skill.from, to)
		if cc != "" {
			preview += fmt.Sprintf("CC: %s\n", cc)
		}
		preview += fmt.Sprintf("Subject: %s\n\n%s\n\n⚠️ Call send_email again with confirm=true to actually send.", subject, body)
		return &agent.ToolResult{Content: preview}, nil
	}

	// Build email message.
	recipients := []string{to}
	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\n", t.skill.from, to)
	if cc != "" {
		msg += fmt.Sprintf("Cc: %s\r\n", cc)
		for _, addr := range strings.Split(cc, ",") {
			addr = strings.TrimSpace(addr)
			if addr != "" {
				recipients = append(recipients, addr)
			}
		}
	}
	msg += fmt.Sprintf("Subject: %s\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s", subject, body)

	// Send via SMTP with TLS.
	addr := fmt.Sprintf("%s:%d", t.skill.host, t.skill.port)
	auth := smtp.PlainAuth("", t.skill.user, t.skill.pass, t.skill.host)

	if err := smtp.SendMail(addr, auth, t.skill.from, recipients, []byte(msg)); err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("SMTP error: %v", err), IsError: true}, nil
	}

	return &agent.ToolResult{Content: fmt.Sprintf("✅ Email sent to %s (subject: %s)", to, subject)}, nil
}

var _ agent.Tool = (*SendEmailTool)(nil)
