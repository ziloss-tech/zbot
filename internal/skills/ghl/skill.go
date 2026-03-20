package ghl

import (
	"fmt"

	"github.com/ziloss-tech/zbot/internal/agent"
)

// Skill wraps the GHL client and tools into a skills.Skill implementation.
type Skill struct {
	client *Client
}

// NewSkill creates a GHL skill with a single default location.
func NewSkill(apiKey, locationID string) *Skill {
	return &Skill{client: NewClient(apiKey, locationID)}
}

// NewMultiLocationSkill creates a GHL skill with multiple locations.
func NewMultiLocationSkill(locations map[string]LocationConfig, defaultAlias string) *Skill {
	if len(locations) == 0 {
		return &Skill{client: NewClient("", "")}
	}

	defaultCfg := locations[defaultAlias]
	c := NewClient(defaultCfg.Token, defaultCfg.ID)
	for alias, cfg := range locations {
		if alias != defaultAlias {
			c.AddLocation(alias, cfg)
		}
	}
	return &Skill{client: c}
}

// Client returns the underlying GHL client (for use by auditor/automation).
func (s *Skill) Client() *Client { return s.client }

func (s *Skill) Name() string        { return "ghl" }
func (s *Skill) Description() string { return "GoHighLevel CRM — contacts, workflows, pipelines, conversations, calendars, bulk operations" }

func (s *Skill) Tools() []agent.Tool {
	return []agent.Tool{
		&GetContactsTool{client: s.client},
		&GetContactTool{client: s.client},
		&UpdateContactTool{client: s.client},
		&GetConversationsTool{client: s.client},
		&SendMessageTool{client: s.client},
		&GetPipelineTool{client: s.client},
		&ListLocationsTool{client: s.client},
		&ListWorkflowsTool{client: s.client},
		&GetWorkflowTool{client: s.client},
		&SearchContactsTool{client: s.client},
		&GetCustomFieldsTool{client: s.client},
		&GetCalendarsTool{client: s.client},
		&GetOpportunitiesTool{client: s.client},
		&BulkUpdateContactsTool{client: s.client},
		// Sprint 2: Auditor tools.
		&AuditWorkflowsTool{client: s.client},
		&AuditContactsTool{client: s.client},
		&CompareLocationsTool{client: s.client},
	}
}

func (s *Skill) SystemPromptAddendum() string {
	locs := s.client.ListLocations()
	locList := ""
	for alias, desc := range locs {
		locList += "  - " + alias + ": " + desc + "\n"
	}

	return fmt.Sprintf(`### GHL (GoHighLevel CRM)
You have direct access to GoHighLevel CRM with %d locations configured:
%s
Available tools:
- ghl_list_locations — see all configured locations
- ghl_get_contacts / ghl_search_contacts — find contacts (search_contacts has tag, DND, date filters)
- ghl_get_contact — full contact details by ID
- ghl_update_contact — update tags, custom fields for a single contact
- ghl_bulk_update_contacts — batch update up to 50 contacts (dry_run=true by default, requires confirm=true)
- ghl_get_conversations — SMS/email history for a contact
- ghl_send_message — send SMS (preview first, requires confirm=true)
- ghl_list_workflows / ghl_get_workflow — list and inspect workflows
- ghl_get_pipeline / ghl_get_opportunities — pipeline stages and opportunities
- ghl_get_custom_fields — list all custom fields
- ghl_get_calendars — list calendars and appointment types

SAFETY RULES:
- All write operations preview first, require explicit confirmation
- Bulk updates: max 50 per batch, dry_run=true default, 3-phase safety for large operations
- Always specify location for multi-location queries
`, len(locs), locList)
}
