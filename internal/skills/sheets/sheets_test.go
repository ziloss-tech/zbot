package sheets

import (
	"testing"

	"github.com/ziloss-tech/zbot/internal/agent"
)

func TestParseValuesFromSlice(t *testing.T) {
	input := []interface{}{
		[]interface{}{"Name", "Age"},
		[]interface{}{"Alice", "30"},
	}
	result, err := parseValues(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(result))
	}
}

func TestParseValuesFromNil(t *testing.T) {
	_, err := parseValues(nil)
	if err == nil {
		t.Fatal("expected error for nil input")
	}
}

func TestParseValuesFromInvalidType(t *testing.T) {
	_, err := parseValues("not an array")
	if err == nil {
		t.Fatal("expected error for string input")
	}
}

func TestToolNames(t *testing.T) {
	tools := []agent.Tool{
		&ReadTool{},
		&WriteTool{},
		&AppendTool{},
		&ListTool{},
	}
	expected := map[string]bool{"sheets_read": true, "sheets_write": true, "sheets_append": true, "sheets_list": true}
	for _, tool := range tools {
		if !expected[tool.Name()] {
			t.Fatalf("unexpected tool name: %q", tool.Name())
		}
		def := tool.Definition()
		if def.Name == "" {
			t.Fatal("definition name should not be empty")
		}
	}
}
