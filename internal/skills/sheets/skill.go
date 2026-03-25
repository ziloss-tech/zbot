// Package sheets implements the Google Sheets skill for ZBOT.
package sheets

import (
	"context"
	"encoding/json"
	"fmt"

	"google.golang.org/api/option"
	sheetsv4 "google.golang.org/api/sheets/v4"

	"github.com/ziloss-tech/zbot/internal/agent"
)

// Skill wraps the Google Sheets service.
type Skill struct {
	svc *sheetsv4.Service
}

// NewSkill creates a Sheets skill using service account credentials JSON.
func NewSkill(ctx context.Context, credentialsJSON string) (*Skill, error) {
	svc, err := sheetsv4.NewService(ctx, option.WithCredentialsJSON([]byte(credentialsJSON))) //lint:ignore SA1019 TODO: migrate to google.CredentialsFromJSON
	if err != nil {
		return nil, fmt.Errorf("sheets: init service: %w", err)
	}
	return &Skill{svc: svc}, nil
}

func (s *Skill) Name() string        { return "sheets" }
func (s *Skill) Description() string { return "Google Sheets — read, write, append data" }

func (s *Skill) Tools() []agent.Tool {
	return []agent.Tool{
		&ReadTool{svc: s.svc},
		&WriteTool{svc: s.svc},
		&AppendTool{svc: s.svc},
		&ListTool{svc: s.svc},
	}
}

func (s *Skill) SystemPromptAddendum() string {
	return `### Google Sheets
You can read and write Google Sheets:
- Read data (sheets_read) — specify spreadsheetId and range like "Sheet1!A1:Z100"
- Write data (sheets_write) — overwrite a range with 2D array values
- Append rows (sheets_append) — add rows to end of a sheet
- List sheet names (sheets_list) — see all tabs in a spreadsheet`
}

// ─── READ TOOL ───────────────────────────────────────────────────────────────

type ReadTool struct{ svc *sheetsv4.Service }

func (t *ReadTool) Name() string { return "sheets_read" }
func (t *ReadTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "sheets_read",
		Description: "Read data from a Google Sheets range. Returns a 2D array of values.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"spreadsheet_id", "range"},
			"properties": map[string]any{
				"spreadsheet_id": map[string]any{"type": "string", "description": "Google Sheets spreadsheet ID"},
				"range":          map[string]any{"type": "string", "description": "A1 range notation (e.g. Sheet1!A1:Z100)"},
			},
		},
	}
}

func (t *ReadTool) Execute(ctx context.Context, input map[string]any) (*agent.ToolResult, error) {
	sid, _ := input["spreadsheet_id"].(string)
	rng, _ := input["range"].(string)
	if sid == "" || rng == "" {
		return &agent.ToolResult{Content: "error: spreadsheet_id and range are required", IsError: true}, nil
	}

	resp, err := t.svc.Spreadsheets.Values.Get(sid, rng).Context(ctx).Do()
	if err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("Sheets error: %v", err), IsError: true}, nil
	}

	data, _ := json.Marshal(resp.Values)
	return &agent.ToolResult{Content: string(data)}, nil
}

var _ agent.Tool = (*ReadTool)(nil)

// ─── WRITE TOOL ──────────────────────────────────────────────────────────────

type WriteTool struct{ svc *sheetsv4.Service }

func (t *WriteTool) Name() string { return "sheets_write" }
func (t *WriteTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "sheets_write",
		Description: "Write data to a Google Sheets range (overwrites existing data).",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"spreadsheet_id", "range", "values"},
			"properties": map[string]any{
				"spreadsheet_id": map[string]any{"type": "string", "description": "Google Sheets spreadsheet ID"},
				"range":          map[string]any{"type": "string", "description": "A1 range notation"},
				"values":         map[string]any{"type": "array", "description": "2D array of values", "items": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}},
			},
		},
	}
}

func (t *WriteTool) Execute(ctx context.Context, input map[string]any) (*agent.ToolResult, error) {
	sid, _ := input["spreadsheet_id"].(string)
	rng, _ := input["range"].(string)
	if sid == "" || rng == "" {
		return &agent.ToolResult{Content: "error: spreadsheet_id and range are required", IsError: true}, nil
	}

	values, err := parseValues(input["values"])
	if err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("error parsing values: %v", err), IsError: true}, nil
	}

	vr := &sheetsv4.ValueRange{Values: values}
	_, err = t.svc.Spreadsheets.Values.Update(sid, rng, vr).
		ValueInputOption("USER_ENTERED").Context(ctx).Do()
	if err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("Sheets error: %v", err), IsError: true}, nil
	}

	return &agent.ToolResult{Content: fmt.Sprintf("✅ Written to %s", rng)}, nil
}

var _ agent.Tool = (*WriteTool)(nil)

// ─── APPEND TOOL ─────────────────────────────────────────────────────────────

type AppendTool struct{ svc *sheetsv4.Service }

func (t *AppendTool) Name() string { return "sheets_append" }
func (t *AppendTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "sheets_append",
		Description: "Append rows to the end of a Google Sheet.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"spreadsheet_id", "sheet_name", "values"},
			"properties": map[string]any{
				"spreadsheet_id": map[string]any{"type": "string", "description": "Google Sheets spreadsheet ID"},
				"sheet_name":     map[string]any{"type": "string", "description": "Sheet/tab name"},
				"values":         map[string]any{"type": "array", "description": "2D array of row values", "items": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}},
			},
		},
	}
}

func (t *AppendTool) Execute(ctx context.Context, input map[string]any) (*agent.ToolResult, error) {
	sid, _ := input["spreadsheet_id"].(string)
	sheet, _ := input["sheet_name"].(string)
	if sid == "" || sheet == "" {
		return &agent.ToolResult{Content: "error: spreadsheet_id and sheet_name are required", IsError: true}, nil
	}

	values, err := parseValues(input["values"])
	if err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("error parsing values: %v", err), IsError: true}, nil
	}

	vr := &sheetsv4.ValueRange{Values: values}
	_, err = t.svc.Spreadsheets.Values.Append(sid, sheet+"!A:ZZ", vr).
		ValueInputOption("USER_ENTERED").InsertDataOption("INSERT_ROWS").Context(ctx).Do()
	if err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("Sheets error: %v", err), IsError: true}, nil
	}

	return &agent.ToolResult{Content: fmt.Sprintf("✅ Appended %d rows to %s", len(values), sheet)}, nil
}

var _ agent.Tool = (*AppendTool)(nil)

// ─── LIST TOOL ───────────────────────────────────────────────────────────────

type ListTool struct{ svc *sheetsv4.Service }

func (t *ListTool) Name() string { return "sheets_list" }
func (t *ListTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "sheets_list",
		Description: "List all sheet/tab names in a Google Spreadsheet.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"spreadsheet_id"},
			"properties": map[string]any{
				"spreadsheet_id": map[string]any{"type": "string", "description": "Google Sheets spreadsheet ID"},
			},
		},
	}
}

func (t *ListTool) Execute(ctx context.Context, input map[string]any) (*agent.ToolResult, error) {
	sid, _ := input["spreadsheet_id"].(string)
	if sid == "" {
		return &agent.ToolResult{Content: "error: spreadsheet_id is required", IsError: true}, nil
	}

	ss, err := t.svc.Spreadsheets.Get(sid).Context(ctx).Do()
	if err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("Sheets error: %v", err), IsError: true}, nil
	}

	var names []string
	for _, s := range ss.Sheets {
		names = append(names, s.Properties.Title)
	}

	data, _ := json.Marshal(names)
	return &agent.ToolResult{Content: string(data)}, nil
}

var _ agent.Tool = (*ListTool)(nil)

// ─── HELPERS ─────────────────────────────────────────────────────────────────

// parseValues converts the LLM's JSON values into the [][]interface{} format
// that the Sheets API expects.
func parseValues(raw any) ([][]interface{}, error) {
	if raw == nil {
		return nil, fmt.Errorf("values is required")
	}

	rows, ok := raw.([]interface{})
	if !ok {
		return nil, fmt.Errorf("values must be a 2D array")
	}

	var result [][]interface{}
	for _, row := range rows {
		cols, ok := row.([]interface{})
		if !ok {
			return nil, fmt.Errorf("each row must be an array")
		}
		result = append(result, cols)
	}
	return result, nil
}
