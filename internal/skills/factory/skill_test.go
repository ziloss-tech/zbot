package factory

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func testFactory(t *testing.T) (*Factory, string) {
	t.Helper()
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	f, err := NewFactory(dir, logger)
	if err != nil {
		t.Fatalf("NewFactory: %v", err)
	}
	return f, dir
}

func TestCreateSkill(t *testing.T) {
	f, dir := testFactory(t)
	tool := &CreateSkillTool{factory: f}

	result, err := tool.Execute(context.Background(), map[string]any{
		"name":        "hello_world",
		"description": "Says hello",
		"script":      "import sys, json\ninput = json.loads(sys.argv[1])\nprint(f\"Hello {input.get('name', 'World')}!\")\n",
		"input_schema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{"type": "string"},
			},
		},
	})

	if err != nil {
		t.Fatalf("CreateSkill: %v", err)
	}
	if result.IsError {
		t.Fatalf("CreateSkill error: %s", result.Content)
	}

	// Verify files exist
	if _, err := os.Stat(filepath.Join(dir, "hello_world.py")); err != nil {
		t.Error("script file not created")
	}
	if _, err := os.Stat(filepath.Join(dir, "hello_world.json")); err != nil {
		t.Error("manifest file not created")
	}

	// Verify tool is registered
	tools := f.GetDynamicTools()
	if len(tools) != 1 {
		t.Fatalf("expected 1 dynamic tool, got %d", len(tools))
	}
	if tools[0].Name() != "hello_world" {
		t.Errorf("tool name = %q, want hello_world", tools[0].Name())
	}
}

func TestExecuteDynamicSkill(t *testing.T) {
	f, _ := testFactory(t)
	createTool := &CreateSkillTool{factory: f}

	// Create a simple skill
	_, err := createTool.Execute(context.Background(), map[string]any{
		"name":        "add_numbers",
		"description": "Adds two numbers",
		"script":      "import sys, json\nd = json.loads(sys.argv[1])\nprint(d.get('a', 0) + d.get('b', 0))\n",
	})
	if err != nil {
		t.Fatalf("CreateSkill: %v", err)
	}

	// Execute it
	tools := f.GetDynamicTools()
	if len(tools) == 0 {
		t.Fatal("no dynamic tools")
	}
	result, err := tools[0].Execute(context.Background(), map[string]any{"a": 3.0, "b": 7.0})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("Execute error: %s", result.Content)
	}
	if result.Content != "10" && result.Content != "10.0" {
		t.Errorf("result = %q, want '10' or '10.0'", result.Content)
	}
}

func TestLoadExistingSkills(t *testing.T) {
	f, _ := testFactory(t)
	createTool := &CreateSkillTool{factory: f}

	// Create two skills
	createTool.Execute(context.Background(), map[string]any{
		"name": "skill_a", "description": "Skill A", "script": "print('A')\n",
	})
	createTool.Execute(context.Background(), map[string]any{
		"name": "skill_b", "description": "Skill B", "script": "print('B')\n",
	})

	// Create a fresh factory pointing to same dir — simulates restart
	f2, err := NewFactory(f.skillsDir, f.logger)
	if err != nil {
		t.Fatalf("NewFactory: %v", err)
	}
	if err := f2.LoadExistingSkills(); err != nil {
		t.Fatalf("LoadExistingSkills: %v", err)
	}
	tools := f2.GetDynamicTools()
	if len(tools) != 2 {
		t.Errorf("loaded %d tools, want 2", len(tools))
	}
}

func TestVersionIncrement(t *testing.T) {
	f, _ := testFactory(t)
	createTool := &CreateSkillTool{factory: f}

	// Create v1
	createTool.Execute(context.Background(), map[string]any{
		"name": "versioned", "description": "v1", "script": "print('v1')\n",
	})
	f.mu.RLock()
	v1 := f.dynamicTools["versioned"].manifest.Version
	f.mu.RUnlock()
	if v1 != 1 {
		t.Errorf("first version = %d, want 1", v1)
	}

	// Create v2 (overwrite)
	createTool.Execute(context.Background(), map[string]any{
		"name": "versioned", "description": "v2", "script": "print('v2')\n",
	})
	f.mu.RLock()
	v2 := f.dynamicTools["versioned"].manifest.Version
	f.mu.RUnlock()
	if v2 != 2 {
		t.Errorf("second version = %d, want 2", v2)
	}
}

func TestListSkills(t *testing.T) {
	f, _ := testFactory(t)
	listTool := &ListSkillsTool{factory: f}

	// Empty
	result, _ := listTool.Execute(context.Background(), nil)
	if result.Content != "No dynamic skills created yet. Use create_skill to build new tools." {
		t.Errorf("empty list unexpected: %s", result.Content)
	}

	// After creating one
	createTool := &CreateSkillTool{factory: f}
	createTool.Execute(context.Background(), map[string]any{
		"name": "test_tool", "description": "A test", "script": "print('test')\n",
	})
	result, _ = listTool.Execute(context.Background(), nil)
	if result.Content == "" || !contains(result.Content, "test_tool") {
		t.Errorf("list should contain test_tool: %s", result.Content)
	}
}

func contains(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
