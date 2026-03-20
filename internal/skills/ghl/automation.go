package ghl

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ziloss-tech/zbot/internal/agent"
)

// ─── ADD CONTACT TO WORKFLOW ───────────────────────────────────────────────────

type AddToWorkflowTool struct{ client *Client }

func (t *AddToWorkflowTool) Name() string { return "ghl_add_to_workflow" }
func (t *AddToWorkflowTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "ghl_add_to_workflow",
		Description: "Enroll a contact into a GHL workflow. Requires confirm=true to execute.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"contact_id", "workflow_id"},
			"properties": map[string]any{
				"location":    map[string]any{"type": "string", "description": "Location alias or ID"},
				"contact_id":  map[string]any{"type": "string", "description": "Contact ID to enroll"},
				"workflow_id": map[string]any{"type": "string", "description": "Workflow ID to enroll into"},
				"confirm":     map[string]any{"type": "boolean", "description": "Must be true to execute"},
			},
		},
	}
}

func (t *AddToWorkflowTool) Execute(ctx context.Context, input map[string]any) (*agent.ToolResult, error) {
	contactID, _ := input["contact_id"].(string)
	workflowID, _ := input["workflow_id"].(string)
	loc, _ := input["location"].(string)
	confirm, _ := input["confirm"].(bool)

	if contactID == "" || workflowID == "" {
		return &agent.ToolResult{Content: "error: contact_id and workflow_id are required", IsError: true}, nil
	}

	if !confirm {
		return &agent.ToolResult{Content: fmt.Sprintf("⚠️ Preview: Would enroll contact %s into workflow %s.\nCall again with confirm=true to execute.", contactID, workflowID)}, nil
	}

	body := map[string]any{"contactId": contactID}
	data, err := t.client.PostFor(ctx, loc, "/workflows/"+workflowID+"/enroll", body)
	if err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("GHL error: %v", err), IsError: true}, nil
	}
	return &agent.ToolResult{Content: "✅ Contact enrolled.\n" + string(data)}, nil
}

var _ agent.Tool = (*AddToWorkflowTool)(nil)

// ─── REMOVE CONTACT FROM WORKFLOW ──────────────────────────────────────────────

type RemoveFromWorkflowTool struct{ client *Client }

func (t *RemoveFromWorkflowTool) Name() string { return "ghl_remove_from_workflow" }
func (t *RemoveFromWorkflowTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "ghl_remove_from_workflow",
		Description: "Remove a contact from a GHL workflow. Requires confirm=true.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"contact_id", "workflow_id"},
			"properties": map[string]any{
				"location":    map[string]any{"type": "string", "description": "Location alias or ID"},
				"contact_id":  map[string]any{"type": "string", "description": "Contact ID"},
				"workflow_id": map[string]any{"type": "string", "description": "Workflow ID"},
				"confirm":     map[string]any{"type": "boolean", "description": "Must be true to execute"},
			},
		},
	}
}

func (t *RemoveFromWorkflowTool) Execute(ctx context.Context, input map[string]any) (*agent.ToolResult, error) {
	contactID, _ := input["contact_id"].(string)
	workflowID, _ := input["workflow_id"].(string)
	loc, _ := input["location"].(string)
	confirm, _ := input["confirm"].(bool)

	if contactID == "" || workflowID == "" {
		return &agent.ToolResult{Content: "error: contact_id and workflow_id are required", IsError: true}, nil
	}
	if !confirm {
		return &agent.ToolResult{Content: fmt.Sprintf("⚠️ Preview: Would remove contact %s from workflow %s.\nCall again with confirm=true.", contactID, workflowID)}, nil
	}

	data, err := t.client.DeleteFor(ctx, loc, "/workflows/"+workflowID+"/enroll/"+contactID)
	if err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("GHL error: %v", err), IsError: true}, nil
	}
	return &agent.ToolResult{Content: "✅ Contact removed from workflow.\n" + string(data)}, nil
}

var _ agent.Tool = (*RemoveFromWorkflowTool)(nil)

// ─── DND REVIEW TOOL (3-Phase Safety Protocol) ────────────────────────────────

type DNDReviewTool struct{ client *Client }

func (t *DNDReviewTool) Name() string { return "ghl_dnd_review" }
func (t *DNDReviewTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "ghl_dnd_review",
		Description: `Review and fix incorrectly DND'd contacts using the 3-phase safety protocol.
Phase 1 (auto): Analyze — pull DND contacts, identify likely incorrect ones, show sample.
Phase 2 (requires confirm): Test — fix 5 contacts, verify SMS delivery.
Phase 3 (requires confirm): Full run — fix all identified contacts in batches of 50.
Each phase requires explicit user confirmation to proceed to the next.`,
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"phase"},
			"properties": map[string]any{
				"location":    map[string]any{"type": "string", "description": "Location alias or ID"},
				"phase":       map[string]any{"type": "integer", "description": "Phase to execute: 1 (analyze), 2 (test 5), 3 (full run)"},
				"tags_filter": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Only review DND contacts with these tags (e.g., ['Inbound', 'SMS'])"},
				"confirm":     map[string]any{"type": "boolean", "description": "Required for phases 2 and 3"},
				"snapshot_id": map[string]any{"type": "string", "description": "Snapshot ID from phase 1 (required for phases 2 and 3)"},
			},
		},
	}
}

func (t *DNDReviewTool) Execute(ctx context.Context, input map[string]any) (*agent.ToolResult, error) {
	loc, _ := input["location"].(string)
	phase, _ := input["phase"].(float64)
	confirm, _ := input["confirm"].(bool)

	switch int(phase) {
	case 1:
		return t.phase1Analyze(ctx, loc, input)
	case 2:
		if !confirm {
			return &agent.ToolResult{Content: "⚠️ Phase 2 requires confirm=true. This will modify 5 contacts."}, nil
		}
		return t.phase2Test(ctx, loc, input)
	case 3:
		if !confirm {
			return &agent.ToolResult{Content: "⚠️ Phase 3 requires confirm=true. This will modify ALL identified contacts."}, nil
		}
		return t.phase3FullRun(ctx, loc, input)
	default:
		return &agent.ToolResult{Content: "error: phase must be 1, 2, or 3", IsError: true}, nil
	}
}

func (t *DNDReviewTool) phase1Analyze(ctx context.Context, loc string, input map[string]any) (*agent.ToolResult, error) {
	// Pull contacts with DND=true.
	body := map[string]any{
		"locationId": t.client.resolveLocation(loc).ID,
		"dnd":        true,
		"limit":      100,
	}

	if tags, ok := input["tags_filter"].([]any); ok && len(tags) > 0 {
		tagStrs := make([]string, 0)
		for _, tag := range tags {
			if s, ok := tag.(string); ok {
				tagStrs = append(tagStrs, s)
			}
		}
		body["tags"] = tagStrs
	}

	data, err := t.client.PostFor(ctx, loc, "/contacts/search", body)
	if err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("GHL error: %v", err), IsError: true}, nil
	}

	var resp struct {
		Contacts []struct {
			ID        string `json:"id"`
			FirstName string `json:"firstName"`
			LastName  string `json:"lastName"`
			Phone     string `json:"phone"`
			DND       bool   `json:"dnd"`
		} `json:"contacts"`
		Meta struct {
			Total int `json:"total"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("Parse error: %v", err), IsError: true}, nil
	}

	// Generate snapshot ID.
	snapshotID := fmt.Sprintf("dnd-review-%d", time.Now().Unix())

	var sb strings.Builder
	sb.WriteString("📋 DND Review — Phase 1: Analysis\n\n")
	sb.WriteString(fmt.Sprintf("Total DND contacts found: %d\n", resp.Meta.Total))
	sb.WriteString(fmt.Sprintf("Sampled: %d\n\n", len(resp.Contacts)))

	sb.WriteString("Sample (first 10):\n")
	for i, c := range resp.Contacts {
		if i >= 10 {
			break
		}
		sb.WriteString(fmt.Sprintf("  %d. %s %s — %s [DND=%v]\n", i+1, c.FirstName, c.LastName, c.Phone, c.DND))
	}

	sb.WriteString(fmt.Sprintf("\nSnapshot ID: %s\n", snapshotID))
	sb.WriteString(fmt.Sprintf("\n➡️ To proceed to Phase 2 (test 5 contacts), call:\n"))
	sb.WriteString(fmt.Sprintf("   ghl_dnd_review phase=2 confirm=true snapshot_id=%s\n", snapshotID))

	return &agent.ToolResult{Content: sb.String()}, nil
}

func (t *DNDReviewTool) phase2Test(ctx context.Context, loc string, input map[string]any) (*agent.ToolResult, error) {
	// Pull 5 DND contacts and fix them.
	body := map[string]any{
		"locationId": t.client.resolveLocation(loc).ID,
		"dnd":        true,
		"limit":      5,
	}

	data, err := t.client.PostFor(ctx, loc, "/contacts/search", body)
	if err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("GHL error: %v", err), IsError: true}, nil
	}

	var resp struct {
		Contacts []struct {
			ID        string `json:"id"`
			FirstName string `json:"firstName"`
			LastName  string `json:"lastName"`
			Phone     string `json:"phone"`
		} `json:"contacts"`
	}
	json.Unmarshal(data, &resp)

	succeeded, failed := 0, 0
	var sb strings.Builder
	sb.WriteString("📋 DND Review — Phase 2: Testing 5 Contacts\n\n")

	for _, c := range resp.Contacts {
		updateBody := map[string]any{"dnd": false}
		_, err := t.client.PutFor(ctx, loc, "/contacts/"+c.ID, updateBody)
		if err != nil {
			failed++
			sb.WriteString(fmt.Sprintf("  ❌ %s %s (%s): %v\n", c.FirstName, c.LastName, c.ID, err))
		} else {
			succeeded++
			sb.WriteString(fmt.Sprintf("  ✅ %s %s (%s): DND removed\n", c.FirstName, c.LastName, c.ID))
		}
	}

	sb.WriteString(fmt.Sprintf("\nResults: %d succeeded, %d failed\n", succeeded, failed))
	sb.WriteString("\n➡️ Verify SMS delivery works for these contacts.\n")
	sb.WriteString("➡️ If good, proceed to Phase 3 (full run) with:\n")
	sb.WriteString("   ghl_dnd_review phase=3 confirm=true\n")

	return &agent.ToolResult{Content: sb.String()}, nil
}

func (t *DNDReviewTool) phase3FullRun(ctx context.Context, loc string, input map[string]any) (*agent.ToolResult, error) {
	cfg := t.client.resolveLocation(loc)
	totalFixed, totalFailed := 0, 0
	batchNum := 0

	var sb strings.Builder
	sb.WriteString("📋 DND Review — Phase 3: Full Run\n\n")

	// Process in batches.
	for {
		body := map[string]any{
			"locationId": cfg.ID,
			"dnd":        true,
			"limit":      50,
		}

		data, err := t.client.PostFor(ctx, loc, "/contacts/search", body)
		if err != nil {
			sb.WriteString(fmt.Sprintf("⚠️ Error fetching batch: %v\n", err))
			break
		}

		var resp struct {
			Contacts []struct {
				ID string `json:"id"`
			} `json:"contacts"`
			Meta struct {
				Total int `json:"total"`
			} `json:"meta"`
		}
		json.Unmarshal(data, &resp)

		if len(resp.Contacts) == 0 {
			break
		}

		batchNum++
		batchSuccess, batchFail := 0, 0

		for _, c := range resp.Contacts {
			updateBody := map[string]any{"dnd": false}
			_, err := t.client.PutFor(ctx, loc, "/contacts/"+c.ID, updateBody)
			if err != nil {
				batchFail++
			} else {
				batchSuccess++
			}
		}

		totalFixed += batchSuccess
		totalFailed += batchFail
		sb.WriteString(fmt.Sprintf("  Batch %d: %d fixed, %d failed (remaining: ~%d)\n", batchNum, batchSuccess, batchFail, resp.Meta.Total-len(resp.Contacts)))

		// Safety: max 100 batches (5,000 contacts).
		if batchNum >= 100 {
			sb.WriteString("\n⚠️ Hit 100 batch limit (5,000 contacts). Run again to continue.\n")
			break
		}
	}

	sb.WriteString(fmt.Sprintf("\n✅ Phase 3 complete: %d contacts fixed, %d failed across %d batches.\n", totalFixed, totalFailed, batchNum))
	return &agent.ToolResult{Content: sb.String()}, nil
}

var _ agent.Tool = (*DNDReviewTool)(nil)
