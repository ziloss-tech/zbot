package ghl

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/ziloss-tech/zbot/internal/agent"
)

// ─── GET CONTACTS ──────────────────────────────────────────────────────────────

type GetContactsTool struct{ client *Client }

func (t *GetContactsTool) Name() string { return "ghl_get_contacts" }
func (t *GetContactsTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "ghl_get_contacts",
		Description: "Search GHL contacts by name, email, phone, or tag. Returns a list of matching contacts.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{},
			"properties": map[string]any{
				"query": map[string]any{"type": "string", "description": "Search query (name, email, phone)"},
				"limit": map[string]any{"type": "integer", "description": "Max results (default 20, max 100)"},
			},
		},
	}
}

func (t *GetContactsTool) Execute(ctx context.Context, input map[string]any) (*agent.ToolResult, error) {
	params := url.Values{"locationId": {t.client.LocationID()}}
	if q, ok := input["query"].(string); ok && q != "" {
		params.Set("query", q)
	}
	limit := 20
	if l, ok := input["limit"].(float64); ok && l > 0 {
		limit = int(l)
		if limit > 100 {
			limit = 100
		}
	}
	params.Set("limit", fmt.Sprintf("%d", limit))

	data, err := t.client.Get(ctx, "/contacts/", params)
	if err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("GHL error: %v", err), IsError: true}, nil
	}

	return &agent.ToolResult{Content: string(data)}, nil
}

var _ agent.Tool = (*GetContactsTool)(nil)

// ─── GET CONTACT ───────────────────────────────────────────────────────────────

type GetContactTool struct{ client *Client }

func (t *GetContactTool) Name() string { return "ghl_get_contact" }
func (t *GetContactTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "ghl_get_contact",
		Description: "Get full details for a single GHL contact by ID.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"contact_id"},
			"properties": map[string]any{
				"contact_id": map[string]any{"type": "string", "description": "GHL contact ID"},
			},
		},
	}
}

func (t *GetContactTool) Execute(ctx context.Context, input map[string]any) (*agent.ToolResult, error) {
	contactID, _ := input["contact_id"].(string)
	if contactID == "" {
		return &agent.ToolResult{Content: "error: contact_id is required", IsError: true}, nil
	}

	data, err := t.client.Get(ctx, "/contacts/"+contactID, nil)
	if err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("GHL error: %v", err), IsError: true}, nil
	}

	return &agent.ToolResult{Content: string(data)}, nil
}

var _ agent.Tool = (*GetContactTool)(nil)

// ─── UPDATE CONTACT ────────────────────────────────────────────────────────────

type UpdateContactTool struct{ client *Client }

func (t *UpdateContactTool) Name() string { return "ghl_update_contact" }
func (t *UpdateContactTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "ghl_update_contact",
		Description: "Update a GHL contact's tags, custom fields, or pipeline stage.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"contact_id"},
			"properties": map[string]any{
				"contact_id": map[string]any{"type": "string", "description": "GHL contact ID"},
				"tags":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Tags to set"},
				"custom_fields": map[string]any{"type": "object", "description": "Custom field values to update"},
			},
		},
	}
}

func (t *UpdateContactTool) Execute(ctx context.Context, input map[string]any) (*agent.ToolResult, error) {
	contactID, _ := input["contact_id"].(string)
	if contactID == "" {
		return &agent.ToolResult{Content: "error: contact_id is required", IsError: true}, nil
	}

	body := make(map[string]any)
	if tags, ok := input["tags"]; ok {
		body["tags"] = tags
	}
	if cf, ok := input["custom_fields"]; ok {
		body["customFields"] = cf
	}

	data, err := t.client.Put(ctx, "/contacts/"+contactID, body)
	if err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("GHL error: %v", err), IsError: true}, nil
	}

	return &agent.ToolResult{Content: string(data)}, nil
}

var _ agent.Tool = (*UpdateContactTool)(nil)

// ─── GET CONVERSATIONS ─────────────────────────────────────────────────────────

type GetConversationsTool struct{ client *Client }

func (t *GetConversationsTool) Name() string { return "ghl_get_conversations" }
func (t *GetConversationsTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "ghl_get_conversations",
		Description: "Get recent SMS/email conversations for a GHL contact.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"contact_id"},
			"properties": map[string]any{
				"contact_id": map[string]any{"type": "string", "description": "GHL contact ID"},
			},
		},
	}
}

func (t *GetConversationsTool) Execute(ctx context.Context, input map[string]any) (*agent.ToolResult, error) {
	contactID, _ := input["contact_id"].(string)
	if contactID == "" {
		return &agent.ToolResult{Content: "error: contact_id is required", IsError: true}, nil
	}

	params := url.Values{
		"locationId": {t.client.LocationID()},
		"contactId":  {contactID},
	}
	data, err := t.client.Get(ctx, "/conversations/search", params)
	if err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("GHL error: %v", err), IsError: true}, nil
	}

	return &agent.ToolResult{Content: string(data)}, nil
}

var _ agent.Tool = (*GetConversationsTool)(nil)

// ─── SEND MESSAGE ──────────────────────────────────────────────────────────────

type SendMessageTool struct{ client *Client }

func (t *SendMessageTool) Name() string { return "ghl_send_message" }
func (t *SendMessageTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "ghl_send_message",
		Description: "Send an SMS message to a GHL contact. IMPORTANT: Returns a preview first. You must call this again with confirm=true to actually send.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"contact_id", "message"},
			"properties": map[string]any{
				"contact_id": map[string]any{"type": "string", "description": "GHL contact ID"},
				"message":    map[string]any{"type": "string", "description": "SMS message text"},
				"confirm":    map[string]any{"type": "boolean", "description": "Set to true to actually send (false = preview only)"},
			},
		},
	}
}

func (t *SendMessageTool) Execute(ctx context.Context, input map[string]any) (*agent.ToolResult, error) {
	contactID, _ := input["contact_id"].(string)
	message, _ := input["message"].(string)
	confirm, _ := input["confirm"].(bool)

	if contactID == "" || message == "" {
		return &agent.ToolResult{Content: "error: contact_id and message are required", IsError: true}, nil
	}

	// Safety: require explicit confirmation before sending.
	if !confirm {
		return &agent.ToolResult{Content: fmt.Sprintf("📱 SMS Preview — To: contact %s\n\n%s\n\n⚠️ Call ghl_send_message again with confirm=true to actually send this message.", contactID, message)}, nil
	}

	body := map[string]any{
		"type":      "SMS",
		"contactId": contactID,
		"message":   message,
	}

	data, err := t.client.Post(ctx, "/conversations/messages", body)
	if err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("GHL send error: %v", err), IsError: true}, nil
	}

	return &agent.ToolResult{Content: "✅ SMS sent.\n" + string(data)}, nil
}

var _ agent.Tool = (*SendMessageTool)(nil)

// ─── GET PIPELINE ──────────────────────────────────────────────────────────────

type GetPipelineTool struct{ client *Client }

func (t *GetPipelineTool) Name() string { return "ghl_get_pipeline" }
func (t *GetPipelineTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "ghl_get_pipeline",
		Description: "Get pipeline stages and opportunity counts from GHL.",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}
}

func (t *GetPipelineTool) Execute(ctx context.Context, input map[string]any) (*agent.ToolResult, error) {
	params := url.Values{"locationId": {t.client.LocationID()}}
	data, err := t.client.Get(ctx, "/opportunities/pipelines", params)
	if err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("GHL error: %v", err), IsError: true}, nil
	}
	return &agent.ToolResult{Content: string(data)}, nil
}

var _ agent.Tool = (*GetPipelineTool)(nil)

// ─── JSON FORMATTING HELPER ────────────────────────────────────────────────────

func formatJSON(raw []byte) string {
	var pretty json.RawMessage
	if err := json.Unmarshal(raw, &pretty); err != nil {
		return string(raw)
	}
	out, err := json.MarshalIndent(pretty, "", "  ")
	if err != nil {
		return string(raw)
	}
	return string(out)
}
