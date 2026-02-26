// Package ghl implements the GoHighLevel CRM skill for ZBOT.
package ghl

import (
	"github.com/jeremylerwick-max/zbot/internal/agent"
)

// Skill wraps the GHL client and tools into a skills.Skill implementation.
type Skill struct {
	client *Client
}

// NewSkill creates a GHL skill ready for registration.
func NewSkill(apiKey, locationID string) *Skill {
	return &Skill{client: NewClient(apiKey, locationID)}
}

func (s *Skill) Name() string        { return "ghl" }
func (s *Skill) Description() string { return "GoHighLevel CRM — contacts, conversations, pipeline, messaging" }

func (s *Skill) Tools() []agent.Tool {
	return []agent.Tool{
		&GetContactsTool{client: s.client},
		&GetContactTool{client: s.client},
		&UpdateContactTool{client: s.client},
		&GetConversationsTool{client: s.client},
		&SendMessageTool{client: s.client},
		&GetPipelineTool{client: s.client},
	}
}

func (s *Skill) SystemPromptAddendum() string {
	return `### GHL (GoHighLevel CRM)
You have direct access to Jeremy's GoHighLevel CRM. You can:
- Search and view contacts (ghl_get_contacts, ghl_get_contact)
- Update contact tags and custom fields (ghl_update_contact)
- View SMS/email conversations (ghl_get_conversations)
- Send SMS messages (ghl_send_message) — ALWAYS preview first, require explicit confirmation
- View pipeline stages and opportunities (ghl_get_pipeline)
Location ID: fRrP1e3LGLFewc5dQDhS`
}
