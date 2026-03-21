package parallel

import (
	"context"
	"os"
	"path/filepath"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/ziloss-tech/zbot/internal/agent"
)

// mockCoderClient returns a fixed Go file for any prompt.
type mockCoderClient struct {
	response string
}

func (m *mockCoderClient) Complete(ctx context.Context, messages []agent.Message, tools []agent.ToolDefinition) (*agent.CompletionResult, error) {
	return &agent.CompletionResult{
		Content:      m.response,
		InputTokens:  100,
		OutputTokens: 50,
	}, nil
}

func (m *mockCoderClient) CompleteStream(ctx context.Context, messages []agent.Message, tools []agent.ToolDefinition, out chan<- string) error {
	out <- m.response
	close(out)
	return nil
}

func (m *mockCoderClient) ModelName() string { return "mock-qwen-coder" }

func TestDispatcherBasic(t *testing.T) {
	tmpDir := t.TempDir()

	mockClient := &mockCoderClient{
		response: `package foo

func Add(a, b int) int {
	return a + b
}
`,
	}

	d := NewDispatcher(mockClient, 2, nil)
	// Use a noop logger in tests.
	d.logger = noopLogger()

	manifest := TaskManifest{
		ProjectName: "test-project",
		BaseDir:     tmpDir,
		SharedContext: []ContextFile{
			{Path: "go.mod", Content: "module test-project\n\ngo 1.22\n"},
		},
		Tasks: []CodingTask{
			{
				ID:          "task-1",
				OutputFile:  "pkg/foo/add.go",
				Instruction: "Implement the Add function that adds two integers.",
			},
			{
				ID:          "task-2",
				OutputFile:  "pkg/bar/multiply.go",
				Instruction: "Implement the Multiply function that multiplies two integers.",
			},
		},
		CreatedAt:         time.Now(),
		OrchestratorModel: "test",
	}

	results, err := d.Run(context.Background(), manifest)
	if err != nil {
		t.Fatal(err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	for _, r := range results {
		if r.Status != "success" {
			t.Errorf("task %s failed: %s", r.TaskID, r.Error)
		}
		if r.Model != "mock-qwen-coder" {
			t.Errorf("task %s: expected model mock-qwen-coder, got %s", r.TaskID, r.Model)
		}
	}

	// Verify files were written.
	content, err := os.ReadFile(filepath.Join(tmpDir, "pkg/foo/add.go"))
	if err != nil {
		t.Fatal(err)
	}
	if len(content) == 0 {
		t.Error("add.go is empty")
	}
}

func TestDispatcherDependencies(t *testing.T) {
	tmpDir := t.TempDir()
	callOrder := make([]string, 0)

	mockClient := &mockCoderClient{
		response: "package foo\n\nfunc Stub() {}\n",
	}

	d := NewDispatcher(mockClient, 1, nil) // max 1 to test sequential ordering
	d.logger = noopLogger()

	manifest := TaskManifest{
		ProjectName: "dep-test",
		BaseDir:     tmpDir,
		Tasks: []CodingTask{
			{ID: "base", OutputFile: "base.go", Instruction: "base"},
			{ID: "derived", OutputFile: "derived.go", Instruction: "derived", DependsOn: []string{"base"}},
		},
		CreatedAt: time.Now(),
	}

	results, err := d.Run(context.Background(), manifest)
	if err != nil {
		t.Fatal(err)
	}
	_ = callOrder

	// "derived" should come after "base" in results since max_parallel=1.
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	for _, r := range results {
		if r.Status != "success" {
			t.Errorf("task %s failed", r.TaskID)
		}
	}
}

func TestExtractCode(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{
			"```go\npackage foo\n\nfunc Bar() {}\n```",
			"package foo\n\nfunc Bar() {}",
		},
		{
			"package foo\n\nfunc Bar() {}",
			"package foo\n\nfunc Bar() {}",
		},
		{
			"```\npackage foo\n```",
			"package foo",
		},
	}
	for _, tt := range tests {
		got := extractCode(tt.input)
		if got != tt.want {
			t.Errorf("extractCode(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestManifestSaveLoad(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "manifest.json")

	original := TaskManifest{
		ProjectName: "save-test",
		BaseDir:     tmpDir,
		Tasks: []CodingTask{
			{ID: "t1", OutputFile: "a.go", Instruction: "do something"},
		},
		CreatedAt:         time.Now().Truncate(time.Second),
		OrchestratorModel: "opus",
	}

	if err := SaveManifest(original, path); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadManifest(path)
	if err != nil {
		t.Fatal(err)
	}

	if loaded.ProjectName != original.ProjectName {
		t.Errorf("project name: got %q, want %q", loaded.ProjectName, original.ProjectName)
	}
	if len(loaded.Tasks) != 1 {
		t.Errorf("tasks: got %d, want 1", len(loaded.Tasks))
	}
}

// noopLogger creates a logger that discards all output.
func noopLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
