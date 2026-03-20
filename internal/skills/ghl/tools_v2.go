package ghl

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/ziloss-tech/zbot/internal/agent"
)

// ─── LIST LOCATIONS ────────────────────────────────────────────────────────────

type ListLocationsTool struct{ client *Client }

func (t *ListLocationsTool) Name() string { return "ghl_list_locations" }
func (t *ListLocationsTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "ghl_list_locations",
		Description: "List all configured GHL locations/sub-accounts. Shows aliases and location IDs. Use the alias in the 'location' field of other GHL tools to target a specific location.",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
	}
}

func (t *ListLocationsTool) Execute(ctx context.Context, input map[string]any) (*agent.ToolResult, error) {
	locs := t.client.ListLocations()
	var sb strings.Builder
	sb.WriteString("Configured GHL Locations:\n")
	for alias, desc := range locs {
		sb.WriteString(fmt.Sprintf("  • %s — %s\n", alias, desc))
	}
	return &agent.ToolResult{Content: sb.String()}, nil
}

var _ agent.Tool = (*ListLocationsTool)(nil)

// ─── LIST WORKFLOWS ────────────────────────────────────────────────────────────

type ListWorkflowsTool struct{ client *Client }

func (t *ListWorkflowsTool) Name() string { return "ghl_list_workflows" }
func (t *ListWorkflowsTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "ghl_list_workflows",
		Description: "List all workflows for a GHL location. Returns workflow ID, name, status (draft/published), and version.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"location": map[string]any{"type": "string", "description": "Location alias or ID (default: primary location)"},
			},
		},
	}
}

func (t *ListWorkflowsTool) Execute(ctx context.Context, input map[string]any) (*agent.ToolResult, error) {
	loc, _ := input["location"].(string)
	data, err := t.client.GetFor(ctx, loc, "/workflows/", nil)
	if err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("GHL error: %v", err), IsError: true}, nil
	}

	var resp struct {
		Workflows []struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			Status  string `json:"status"`
			Version int    `json:"version"`
		} `json:"workflows"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return &agent.ToolResult{Content: formatJSON(data)}, nil
	}

	var sb strings.Builder
	published, draft := 0, 0
	for _, wf := range resp.Workflows {
		status := "📝 draft"
		if wf.Status == "published" {
			status = "✅ published"
			published++
		} else {
			draft++
		}
		sb.WriteString(fmt.Sprintf("• %s — %s (v%d) [%s]\n", wf.Name, wf.ID, wf.Version, status))
	}
	summary := fmt.Sprintf("Total: %d workflows (%d published, %d draft)\n\n", len(resp.Workflows), published, draft)
	return &agent.ToolResult{Content: summary + sb.String()}, nil
}

var _ agent.Tool = (*ListWorkflowsTool)(nil)

// ─── SEARCH CONTACTS (advanced) ────────────────────────────────────────────────

type SearchContactsTool struct{ client *Client }

func (t *SearchContactsTool) Name() string { return "ghl_search_contacts" }
func (t *SearchContactsTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "ghl_search_contacts",
		Description: "Advanced contact search with filters: tags, DND status, date range, custom fields. More powerful than ghl_get_contacts.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"location":    map[string]any{"type": "string", "description": "Location alias or ID (default: primary)"},
				"query":       map[string]any{"type": "string", "description": "Search by name, email, or phone"},
				"tags":        map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Filter by tags (contacts must have ALL listed tags)"},
				"dnd":         map[string]any{"type": "boolean", "description": "Filter by DND status: true=only DND contacts, false=only non-DND"},
				"start_after": map[string]any{"type": "string", "description": "Filter contacts created after this date (YYYY-MM-DD)"},
				"start_before": map[string]any{"type": "string", "description": "Filter contacts created before this date (YYYY-MM-DD)"},
				"limit":       map[string]any{"type": "integer", "description": "Max results (default 20, max 100)"},
			},
		},
	}
}

func (t *SearchContactsTool) Execute(ctx context.Context, input map[string]any) (*agent.ToolResult, error) {
	loc, _ := input["location"].(string)
	params := url.Values{}

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

	// Build filter for tags, DND, dates.
	// GHL search endpoint uses POST with filters body for advanced queries.
	body := map[string]any{}

	if tags, ok := input["tags"].([]any); ok && len(tags) > 0 {
		tagStrs := make([]string, 0, len(tags))
		for _, t := range tags {
			if s, ok := t.(string); ok {
				tagStrs = append(tagStrs, s)
			}
		}
		if len(tagStrs) > 0 {
			body["tags"] = tagStrs
		}
	}

	if dnd, ok := input["dnd"].(bool); ok {
		body["dnd"] = dnd
	}

	if after, ok := input["start_after"].(string); ok && after != "" {
		body["startAfter"] = after
	}
	if before, ok := input["start_before"].(string); ok && before != "" {
		body["startBefore"] = before
	}

	// If we have filters, use the search endpoint.
	if len(body) > 0 {
		cfg := t.client.resolveLocation(loc)
		body["locationId"] = cfg.ID
		if q, ok := input["query"].(string); ok && q != "" {
			body["query"] = q
		}
		body["limit"] = limit

		data, err := t.client.PostFor(ctx, loc, "/contacts/search", body)
		if err != nil {
			return &agent.ToolResult{Content: fmt.Sprintf("GHL search error: %v", err), IsError: true}, nil
		}
		return &agent.ToolResult{Content: formatJSON(data)}, nil
	}

	// Simple search — use GET.
	data, err := t.client.GetFor(ctx, loc, "/contacts/", params)
	if err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("GHL error: %v", err), IsError: true}, nil
	}
	return &agent.ToolResult{Content: formatJSON(data)}, nil
}

var _ agent.Tool = (*SearchContactsTool)(nil)

// ─── GET CUSTOM FIELDS ─────────────────────────────────────────────────────────

type GetCustomFieldsTool struct{ client *Client }

func (t *GetCustomFieldsTool) Name() string { return "ghl_get_custom_fields" }
func (t *GetCustomFieldsTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "ghl_get_custom_fields",
		Description: "List all custom fields configured for a GHL location. Shows field name, key, type, and options.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"location": map[string]any{"type": "string", "description": "Location alias or ID (default: primary)"},
			},
		},
	}
}

func (t *GetCustomFieldsTool) Execute(ctx context.Context, input map[string]any) (*agent.ToolResult, error) {
	loc, _ := input["location"].(string)
	data, err := t.client.GetFor(ctx, loc, "/locations/custom-fields", nil)
	if err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("GHL error: %v", err), IsError: true}, nil
	}
	return &agent.ToolResult{Content: formatJSON(data)}, nil
}

var _ agent.Tool = (*GetCustomFieldsTool)(nil)

// ─── GET CALENDARS ─────────────────────────────────────────────────────────────

type GetCalendarsTool struct{ client *Client }

func (t *GetCalendarsTool) Name() string { return "ghl_get_calendars" }
func (t *GetCalendarsTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "ghl_get_calendars",
		Description: "List all calendars and appointment types for a GHL location.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"location": map[string]any{"type": "string", "description": "Location alias or ID (default: primary)"},
			},
		},
	}
}

func (t *GetCalendarsTool) Execute(ctx context.Context, input map[string]any) (*agent.ToolResult, error) {
	loc, _ := input["location"].(string)
	data, err := t.client.GetFor(ctx, loc, "/calendars/", nil)
	if err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("GHL error: %v", err), IsError: true}, nil
	}
	return &agent.ToolResult{Content: formatJSON(data)}, nil
}

var _ agent.Tool = (*GetCalendarsTool)(nil)

// ─── GET OPPORTUNITIES ─────────────────────────────────────────────────────────

type GetOpportunitiesTool struct{ client *Client }

func (t *GetOpportunitiesTool) Name() string { return "ghl_get_opportunities" }
func (t *GetOpportunitiesTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "ghl_get_opportunities",
		Description: "Search opportunities in a GHL pipeline. Filter by pipeline ID, stage, contact, or status.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"location":    map[string]any{"type": "string", "description": "Location alias or ID"},
				"pipeline_id": map[string]any{"type": "string", "description": "Pipeline ID to filter by"},
				"stage_id":    map[string]any{"type": "string", "description": "Stage ID to filter by"},
				"contact_id":  map[string]any{"type": "string", "description": "Contact ID to filter by"},
				"status":      map[string]any{"type": "string", "description": "Status: open, won, lost, abandoned"},
				"limit":       map[string]any{"type": "integer", "description": "Max results (default 20)"},
			},
		},
	}
}

func (t *GetOpportunitiesTool) Execute(ctx context.Context, input map[string]any) (*agent.ToolResult, error) {
	loc, _ := input["location"].(string)
	params := url.Values{}

	if pid, ok := input["pipeline_id"].(string); ok && pid != "" {
		params.Set("pipelineId", pid)
	}
	if sid, ok := input["stage_id"].(string); ok && sid != "" {
		params.Set("pipelineStageId", sid)
	}
	if cid, ok := input["contact_id"].(string); ok && cid != "" {
		params.Set("contactId", cid)
	}
	if status, ok := input["status"].(string); ok && status != "" {
		params.Set("status", status)
	}

	limit := 20
	if l, ok := input["limit"].(float64); ok && l > 0 {
		limit = int(l)
	}
	params.Set("limit", fmt.Sprintf("%d", limit))

	data, err := t.client.GetFor(ctx, loc, "/opportunities/search", params)
	if err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("GHL error: %v", err), IsError: true}, nil
	}
	return &agent.ToolResult{Content: formatJSON(data)}, nil
}

var _ agent.Tool = (*GetOpportunitiesTool)(nil)

// ─── GET WORKFLOW ──────────────────────────────────────────────────────────────

type GetWorkflowTool struct{ client *Client }

func (t *GetWorkflowTool) Name() string { return "ghl_get_workflow" }
func (t *GetWorkflowTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "ghl_get_workflow",
		Description: "Get full details for a single workflow by ID — triggers, actions, conditions.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"workflow_id"},
			"properties": map[string]any{
				"location":    map[string]any{"type": "string", "description": "Location alias or ID"},
				"workflow_id": map[string]any{"type": "string", "description": "Workflow ID"},
			},
		},
	}
}

func (t *GetWorkflowTool) Execute(ctx context.Context, input map[string]any) (*agent.ToolResult, error) {
	loc, _ := input["location"].(string)
	wfID, _ := input["workflow_id"].(string)
	if wfID == "" {
		return &agent.ToolResult{Content: "error: workflow_id is required", IsError: true}, nil
	}

	data, err := t.client.GetFor(ctx, loc, "/workflows/"+wfID, nil)
	if err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("GHL error: %v", err), IsError: true}, nil
	}
	return &agent.ToolResult{Content: formatJSON(data)}, nil
}

var _ agent.Tool = (*GetWorkflowTool)(nil)

// ─── BULK UPDATE CONTACTS ──────────────────────────────────────────────────────

type BulkUpdateContactsTool struct{ client *Client }

func (t *BulkUpdateContactsTool) Name() string { return "ghl_bulk_update_contacts" }
func (t *BulkUpdateContactsTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "ghl_bulk_update_contacts",
		Description: "Bulk update contacts — add/remove tags, update DND, set custom fields. SAFETY: max 50 per batch, dry_run=true by default. Set dry_run=false + confirm=true to actually execute.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"contact_ids"},
			"properties": map[string]any{
				"location":    map[string]any{"type": "string", "description": "Location alias or ID"},
				"contact_ids": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Contact IDs to update (max 50)"},
				"add_tags":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Tags to add"},
				"remove_tags": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Tags to remove"},
				"dnd":         map[string]any{"type": "boolean", "description": "Set DND status"},
				"custom_fields": map[string]any{"type": "object", "description": "Custom field values to set"},
				"dry_run":     map[string]any{"type": "boolean", "description": "Preview changes without applying (default: true)"},
				"confirm":     map[string]any{"type": "boolean", "description": "Must be true alongside dry_run=false to execute"},
			},
		},
	}
}

func (t *BulkUpdateContactsTool) Execute(ctx context.Context, input map[string]any) (*agent.ToolResult, error) {
	contactIDs, _ := input["contact_ids"].([]any)
	if len(contactIDs) == 0 {
		return &agent.ToolResult{Content: "error: contact_ids is required", IsError: true}, nil
	}
	if len(contactIDs) > 50 {
		return &agent.ToolResult{Content: fmt.Sprintf("error: max 50 contacts per batch, got %d. Split into multiple calls.", len(contactIDs)), IsError: true}, nil
	}

	dryRun := true
	if dr, ok := input["dry_run"].(bool); ok {
		dryRun = dr
	}
	confirm, _ := input["confirm"].(bool)

	if !dryRun && !confirm {
		return &agent.ToolResult{Content: "⚠️ SAFETY: dry_run=false requires confirm=true to execute. This is a destructive operation."}, nil
	}

	if dryRun {
		return &agent.ToolResult{Content: fmt.Sprintf("🔍 DRY RUN: Would update %d contacts.\nChanges: %s\n\nTo execute, call again with dry_run=false and confirm=true.", len(contactIDs), formatChanges(input))}, nil
	}

	// Execute the updates.
	loc, _ := input["location"].(string)
	succeeded, failed := 0, 0
	var errors []string

	for _, cid := range contactIDs {
		contactID, ok := cid.(string)
		if !ok {
			continue
		}

		body := make(map[string]any)
		if tags, ok := input["add_tags"].([]any); ok {
			body["tags"] = tags
		}
		if dnd, ok := input["dnd"].(bool); ok {
			body["dnd"] = dnd
		}
		if cf, ok := input["custom_fields"]; ok {
			body["customFields"] = cf
		}

		_, err := t.client.PutFor(ctx, loc, "/contacts/"+contactID, body)
		if err != nil {
			failed++
			errors = append(errors, fmt.Sprintf("%s: %v", contactID, err))
		} else {
			succeeded++
		}
	}

	result := fmt.Sprintf("✅ Bulk update complete: %d succeeded, %d failed out of %d total.", succeeded, failed, len(contactIDs))
	if len(errors) > 0 {
		result += "\n\nErrors:\n"
		for _, e := range errors {
			result += "  • " + e + "\n"
		}
	}
	return &agent.ToolResult{Content: result}, nil
}

func formatChanges(input map[string]any) string {
	var parts []string
	if tags, ok := input["add_tags"].([]any); ok && len(tags) > 0 {
		parts = append(parts, fmt.Sprintf("add tags: %v", tags))
	}
	if tags, ok := input["remove_tags"].([]any); ok && len(tags) > 0 {
		parts = append(parts, fmt.Sprintf("remove tags: %v", tags))
	}
	if dnd, ok := input["dnd"].(bool); ok {
		parts = append(parts, fmt.Sprintf("set DND=%v", dnd))
	}
	if cf, ok := input["custom_fields"].(map[string]any); ok && len(cf) > 0 {
		parts = append(parts, fmt.Sprintf("custom fields: %v", cf))
	}
	if len(parts) == 0 {
		return "(no changes specified)"
	}
	return strings.Join(parts, ", ")
}

var _ agent.Tool = (*BulkUpdateContactsTool)(nil)
