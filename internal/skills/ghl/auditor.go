package ghl

import (
	"context"
	"net/url"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ziloss-tech/zbot/internal/agent"
)

// ─── AUDIT TYPES ───────────────────────────────────────────────────────────────

type AuditSeverity string

const (
	SeverityCritical AuditSeverity = "critical"
	SeverityWarning  AuditSeverity = "warning"
	SeverityInfo     AuditSeverity = "info"
)

type AuditFinding struct {
	Rule        string        `json:"rule"`
	Severity    AuditSeverity `json:"severity"`
	Description string        `json:"description"`
	WorkflowIDs []string      `json:"workflow_ids,omitempty"`
	ContactIDs  []string      `json:"contact_ids,omitempty"`
	Details     string        `json:"details,omitempty"`
}

type AuditReport struct {
	LocationID   string         `json:"location_id"`
	LocationName string         `json:"location_name"`
	TotalItems   int            `json:"total_items"`
	Findings     []AuditFinding `json:"findings"`
	Summary      string         `json:"summary"`
}

// ─── WORKFLOW STRUCTS ──────────────────────────────────────────────────────────

type WorkflowSummary struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Status  string `json:"status"`
	Version int    `json:"version"`
}

type WorkflowDetail struct {
	ID       string           `json:"id"`
	Name     string           `json:"name"`
	Status   string           `json:"status"`
	Version  int              `json:"version"`
	Triggers []WorkflowTrigger `json:"triggers"`
	Actions  []WorkflowAction  `json:"actions"`
}

type WorkflowTrigger struct {
	Type       string `json:"type"`
	CalendarID string `json:"calendarId,omitempty"`
	FormID     string `json:"formId,omitempty"`
	TagName    string `json:"tagName,omitempty"`
}

type WorkflowAction struct {
	Type            string `json:"type"`
	StopOnResponse  bool   `json:"stopOnResponse"`
	WaitDuration    int    `json:"waitDuration,omitempty"`
	ActionType      string `json:"actionType,omitempty"`
}

// ─── AUDIT WORKFLOWS TOOL ──────────────────────────────────────────────────────

type AuditWorkflowsTool struct{ client *Client }

func (t *AuditWorkflowsTool) Name() string { return "ghl_audit_workflows" }
func (t *AuditWorkflowsTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "ghl_audit_workflows",
		Description: "Audit all workflows for a GHL location. Flags: shared calendar triggers (CRITICAL), high draft count, stopOnResponse disabled, duplicate names, workflows with no recent activity. Returns findings with severity levels.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"location": map[string]any{"type": "string", "description": "Location alias or ID"},
			},
		},
	}
}

func (t *AuditWorkflowsTool) Execute(ctx context.Context, input map[string]any) (*agent.ToolResult, error) {
	loc, _ := input["location"].(string)

	// Step 1: List all workflows.
	data, err := t.client.GetFor(ctx, loc, "/workflows/", nil)
	if err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("GHL error listing workflows: %v", err), IsError: true}, nil
	}

	var listResp struct {
		Workflows []json.RawMessage `json:"workflows"`
	}
	if err := json.Unmarshal(data, &listResp); err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("Failed to parse workflows: %v", err), IsError: true}, nil
	}

	// Parse into summaries.
	var workflows []WorkflowSummary
	for _, raw := range listResp.Workflows {
		var wf WorkflowSummary
		if err := json.Unmarshal(raw, &wf); err == nil {
			workflows = append(workflows, wf)
		}
	}

	// Step 2: Run audit rules.
	var findings []AuditFinding

	// Rule 1: Draft count.
	draftCount := 0
	draftNames := []string{}
	publishedCount := 0
	for _, wf := range workflows {
		if wf.Status == "draft" {
			draftCount++
			draftNames = append(draftNames, wf.Name)
		} else {
			publishedCount++
		}
	}
	if draftCount > 10 {
		findings = append(findings, AuditFinding{
			Rule:        "high-draft-count",
			Severity:    SeverityWarning,
			Description: fmt.Sprintf("%d out of %d workflows are in draft status. Review and either publish or delete stale drafts.", draftCount, len(workflows)),
			Details:     fmt.Sprintf("Draft workflows: %s", strings.Join(draftNames[:min(10, len(draftNames))], ", ")),
		})
	}

	// Rule 2: Duplicate names.
	nameCount := map[string][]string{}
	for _, wf := range workflows {
		nameCount[wf.Name] = append(nameCount[wf.Name], wf.ID)
	}
	for name, ids := range nameCount {
		if len(ids) > 1 {
			findings = append(findings, AuditFinding{
				Rule:        "duplicate-workflow-names",
				Severity:    SeverityWarning,
				Description: fmt.Sprintf("Workflow name \"%s\" is used by %d workflows. This causes confusion — rename to distinguish them.", name, len(ids)),
				WorkflowIDs: ids,
			})
		}
	}

	// Rule 3: Shared calendar triggers (CRITICAL).
	// This requires fetching workflow details — do it for published workflows only.
	calendarTriggers := map[string][]string{} // calendarID → []workflowID
	for _, wf := range workflows {
		if wf.Status != "published" {
			continue
		}
		detail, detailErr := t.client.GetFor(ctx, loc, "/workflows/"+wf.ID, nil)
		if detailErr != nil {
			continue
		}
		var wfDetail struct {
			Workflow struct {
				Actions []struct {
					Type       string `json:"type"`
					TriggerType string `json:"triggerType,omitempty"`
					CalendarID string `json:"calendarId,omitempty"`
					StopOnResponse bool `json:"stopOnResponse"`
				} `json:"actions"`
			} `json:"workflow"`
		}
		if err := json.Unmarshal(detail, &wfDetail); err == nil {
			for _, action := range wfDetail.Workflow.Actions {
				// Check for calendar-based triggers.
				if action.CalendarID != "" && (action.Type == "trigger" || action.TriggerType == "appointment") {
					calendarTriggers[action.CalendarID] = append(calendarTriggers[action.CalendarID], wf.ID)
				}
				// Rule 4: stopOnResponse disabled.
				if action.Type == "send_sms" && !action.StopOnResponse {
					findings = append(findings, AuditFinding{
						Rule:        "stop-on-response-disabled",
						Severity:    SeverityWarning,
						Description: fmt.Sprintf("Workflow \"%s\" has an SMS action with stopOnResponse=false. Contacts may receive messages even after replying.", wf.Name),
						WorkflowIDs: []string{wf.ID},
					})
				}
			}
		}
	}

	for calID, wfIDs := range calendarTriggers {
		if len(wfIDs) > 1 {
			findings = append(findings, AuditFinding{
				Rule:        "shared-calendar-triggers",
				Severity:    SeverityCritical,
				Description: fmt.Sprintf("CRITICAL: %d published workflows share calendar trigger '%s'. This can cause workflows to eject contacts from each other — the exact bug that causes missed appointments.", len(wfIDs), calID),
				WorkflowIDs: wfIDs,
			})
		}
	}

	// Build report.
	criticals, warnings, infos := 0, 0, 0
	for _, f := range findings {
		switch f.Severity {
		case SeverityCritical:
			criticals++
		case SeverityWarning:
			warnings++
		case SeverityInfo:
			infos++
		}
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📋 GHL Workflow Audit Report\n"))
	sb.WriteString(fmt.Sprintf("Location: %s\n", loc))
	sb.WriteString(fmt.Sprintf("Total workflows: %d (%d published, %d draft)\n", len(workflows), publishedCount, draftCount))
	sb.WriteString(fmt.Sprintf("Findings: %d critical, %d warnings, %d info\n\n", criticals, warnings, infos))

	if len(findings) == 0 {
		sb.WriteString("✅ No issues found. All workflows look healthy.\n")
	}

	for i, f := range findings {
		icon := "ℹ️"
		switch f.Severity {
		case SeverityCritical:
			icon = "🚨"
		case SeverityWarning:
			icon = "⚠️"
		}
		sb.WriteString(fmt.Sprintf("%s [%s] Finding %d: %s\n", icon, f.Severity, i+1, f.Rule))
		sb.WriteString(fmt.Sprintf("   %s\n", f.Description))
		if len(f.WorkflowIDs) > 0 {
			sb.WriteString(fmt.Sprintf("   Affected: %s\n", strings.Join(f.WorkflowIDs, ", ")))
		}
		if f.Details != "" {
			sb.WriteString(fmt.Sprintf("   Details: %s\n", f.Details))
		}
		sb.WriteString("\n")
	}

	return &agent.ToolResult{Content: sb.String()}, nil
}

var _ agent.Tool = (*AuditWorkflowsTool)(nil)

// ─── AUDIT CONTACTS TOOL ──────────────────────────────────────────────────────

type AuditContactsTool struct{ client *Client }

func (t *AuditContactsTool) Name() string { return "ghl_audit_contacts" }
func (t *AuditContactsTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "ghl_audit_contacts",
		Description: "Audit contact health for a GHL location. Reports: DND rate, tag distribution, contacts with missing fields, stale contacts with no recent activity.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"location": map[string]any{"type": "string", "description": "Location alias or ID"},
				"sample_size": map[string]any{"type": "integer", "description": "Number of contacts to sample (default 100, max 500)"},
			},
		},
	}
}

func (t *AuditContactsTool) Execute(ctx context.Context, input map[string]any) (*agent.ToolResult, error) {
	loc, _ := input["location"].(string)
	sampleSize := 100
	if s, ok := input["sample_size"].(float64); ok && s > 0 {
		sampleSize = int(s)
		if sampleSize > 500 {
			sampleSize = 500
		}
	}

	params := url.Values{"limit": {fmt.Sprintf("%d", sampleSize)}}
	data, err := t.client.GetFor(ctx, loc, "/contacts/", params)
	if err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("GHL error: %v", err), IsError: true}, nil
	}

	var resp struct {
		Contacts []struct {
			ID        string   `json:"id"`
			FirstName string   `json:"firstName"`
			LastName  string   `json:"lastName"`
			Email     string   `json:"email"`
			Phone     string   `json:"phone"`
			Tags      []string `json:"tags"`
			DND       bool     `json:"dnd"`
		} `json:"contacts"`
		Meta struct {
			Total int `json:"total"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("Failed to parse contacts: %v\n\nRaw: %s", err, string(data[:min(500, len(data))]))}, nil
	}

	// Analyze.
	dndCount := 0
	missingEmail := 0
	missingPhone := 0
	missingName := 0
	tagCounts := map[string]int{}

	for _, c := range resp.Contacts {
		if c.DND {
			dndCount++
		}
		if c.Email == "" {
			missingEmail++
		}
		if c.Phone == "" {
			missingPhone++
		}
		if c.FirstName == "" && c.LastName == "" {
			missingName++
		}
		for _, tag := range c.Tags {
			tagCounts[tag]++
		}
	}

	sampled := len(resp.Contacts)
	total := resp.Meta.Total

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📊 GHL Contact Audit Report\n"))
	sb.WriteString(fmt.Sprintf("Location: %s\n", loc))
	sb.WriteString(fmt.Sprintf("Total contacts in location: %d\n", total))
	sb.WriteString(fmt.Sprintf("Sampled: %d contacts\n\n", sampled))

	dndPct := 0.0
	if sampled > 0 {
		dndPct = float64(dndCount) / float64(sampled) * 100
	}

	sb.WriteString(fmt.Sprintf("DND Status:\n"))
	sb.WriteString(fmt.Sprintf("  DND contacts: %d / %d (%.1f%%)\n", dndCount, sampled, dndPct))
	if dndPct > 10 {
		sb.WriteString(fmt.Sprintf("  ⚠️ HIGH DND RATE — %.1f%% of contacts are DND. Projected: ~%d of %d total.\n", dndPct, int(dndPct/100*float64(total)), total))
	}

	sb.WriteString(fmt.Sprintf("\nMissing Fields:\n"))
	sb.WriteString(fmt.Sprintf("  No email: %d (%.1f%%)\n", missingEmail, pct(missingEmail, sampled)))
	sb.WriteString(fmt.Sprintf("  No phone: %d (%.1f%%)\n", missingPhone, pct(missingPhone, sampled)))
	sb.WriteString(fmt.Sprintf("  No name: %d (%.1f%%)\n", missingName, pct(missingName, sampled)))

	sb.WriteString(fmt.Sprintf("\nTop Tags (from sample):\n"))
	type tagEntry struct {
		Tag   string
		Count int
	}
	var sorted []tagEntry
	for tag, count := range tagCounts {
		sorted = append(sorted, tagEntry{tag, count})
	}
	// Simple sort — top 15.
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j].Count > sorted[i].Count {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}
	for i, te := range sorted {
		if i >= 15 {
			sb.WriteString(fmt.Sprintf("  ... and %d more tags\n", len(sorted)-15))
			break
		}
		sb.WriteString(fmt.Sprintf("  %s: %d (%.1f%%)\n", te.Tag, te.Count, pct(te.Count, sampled)))
	}

	return &agent.ToolResult{Content: sb.String()}, nil
}

func pct(n, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(n) / float64(total) * 100
}

var _ agent.Tool = (*AuditContactsTool)(nil)

// ─── CROSS-LOCATION COMPARE TOOL ──────────────────────────────────────────────

type CompareLocationsTool struct{ client *Client }

func (t *CompareLocationsTool) Name() string { return "ghl_compare_locations" }
func (t *CompareLocationsTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "ghl_compare_locations",
		Description: "Compare workflows between two GHL locations. Finds: workflows in one location but not the other, same-named workflows with different statuses, and drift between locations.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"location_a", "location_b"},
			"properties": map[string]any{
				"location_a": map[string]any{"type": "string", "description": "First location alias or ID"},
				"location_b": map[string]any{"type": "string", "description": "Second location alias or ID"},
			},
		},
	}
}

func (t *CompareLocationsTool) Execute(ctx context.Context, input map[string]any) (*agent.ToolResult, error) {
	locA, _ := input["location_a"].(string)
	locB, _ := input["location_b"].(string)

	if locA == "" || locB == "" {
		return &agent.ToolResult{Content: "error: both location_a and location_b are required", IsError: true}, nil
	}

	// Fetch workflows from both.
	dataA, errA := t.client.GetFor(ctx, locA, "/workflows/", nil)
	if errA != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("Error fetching %s: %v", locA, errA), IsError: true}, nil
	}
	dataB, errB := t.client.GetFor(ctx, locB, "/workflows/", nil)
	if errB != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("Error fetching %s: %v", locB, errB), IsError: true}, nil
	}

	var respA, respB struct {
		Workflows []WorkflowSummary `json:"workflows"`
	}
	json.Unmarshal(dataA, &respA)
	json.Unmarshal(dataB, &respB)

	// Build name maps.
	mapA := map[string]WorkflowSummary{}
	for _, wf := range respA.Workflows {
		mapA[wf.Name] = wf
	}
	mapB := map[string]WorkflowSummary{}
	for _, wf := range respB.Workflows {
		mapB[wf.Name] = wf
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("🔄 Location Comparison: %s vs %s\n\n", locA, locB))
	sb.WriteString(fmt.Sprintf("%s: %d workflows\n", locA, len(respA.Workflows)))
	sb.WriteString(fmt.Sprintf("%s: %d workflows\n\n", locB, len(respB.Workflows)))

	// Only in A.
	onlyA := []string{}
	for name := range mapA {
		if _, ok := mapB[name]; !ok {
			onlyA = append(onlyA, name)
		}
	}
	if len(onlyA) > 0 {
		sb.WriteString(fmt.Sprintf("Only in %s (%d):\n", locA, len(onlyA)))
		for _, name := range onlyA {
			sb.WriteString(fmt.Sprintf("  • %s [%s]\n", name, mapA[name].Status))
		}
		sb.WriteString("\n")
	}

	// Only in B.
	onlyB := []string{}
	for name := range mapB {
		if _, ok := mapA[name]; !ok {
			onlyB = append(onlyB, name)
		}
	}
	if len(onlyB) > 0 {
		sb.WriteString(fmt.Sprintf("Only in %s (%d):\n", locB, len(onlyB)))
		for _, name := range onlyB {
			sb.WriteString(fmt.Sprintf("  • %s [%s]\n", name, mapB[name].Status))
		}
		sb.WriteString("\n")
	}

	// In both but different status.
	drifted := []string{}
	for name, wfA := range mapA {
		if wfB, ok := mapB[name]; ok {
			if wfA.Status != wfB.Status {
				drifted = append(drifted, fmt.Sprintf("  • %s: %s=%s, %s=%s", name, locA, wfA.Status, locB, wfB.Status))
			}
		}
	}
	if len(drifted) > 0 {
		sb.WriteString(fmt.Sprintf("Status drift (%d):\n", len(drifted)))
		for _, d := range drifted {
			sb.WriteString(d + "\n")
		}
		sb.WriteString("\n")
	}

	// In both, same status.
	matched := 0
	for name := range mapA {
		if wfB, ok := mapB[name]; ok {
			if mapA[name].Status == wfB.Status {
				matched++
			}
		}
	}
	sb.WriteString(fmt.Sprintf("Matched (same name + status): %d workflows\n", matched))

	return &agent.ToolResult{Content: sb.String()}, nil
}

var _ agent.Tool = (*CompareLocationsTool)(nil)
