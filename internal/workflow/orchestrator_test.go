package workflow

import (
	"context"
	"log/slog"
	"testing"

	"github.com/ziloss-tech/zbot/internal/agent"
)

// MockWorkflowStore is an in-memory implementation for testing
type MockWorkflowStore struct {
	tasks map[string]*agent.Task
}

func NewMockWorkflowStore() *MockWorkflowStore {
	return &MockWorkflowStore{
		tasks: make(map[string]*agent.Task),
	}
}

func (m *MockWorkflowStore) CreateWorkflow(ctx context.Context, tasks []agent.Task) (string, error) {
	wfID := "wf-test-001"
	for i := range tasks {
		taskID := tasks[i].ID
		if taskID == "" {
			taskID = "task-" + wfID + "-" + string(rune(i))
		}
		m.tasks[taskID] = &tasks[i]
	}
	return wfID, nil
}

func (m *MockWorkflowStore) ClaimNextTask(ctx context.Context, workerID string) (*agent.Task, error) {
	for _, task := range m.tasks {
		if task.Status == agent.TaskPending {
			task.Status = agent.TaskRunning
			return task, nil
		}
	}
	return nil, nil
}

func (m *MockWorkflowStore) CompleteTask(ctx context.Context, taskID string, outputRef string) error {
	if task, ok := m.tasks[taskID]; ok {
		task.Status = agent.TaskDone
		task.OutputRef = outputRef
	}
	return nil
}

func (m *MockWorkflowStore) FailTask(ctx context.Context, taskID string, reason string) error {
	if task, ok := m.tasks[taskID]; ok {
		task.Status = agent.TaskFailed
	}
	return nil
}

func (m *MockWorkflowStore) GetWorkflowStatus(ctx context.Context, workflowID string) ([]agent.Task, error) {
	var result []agent.Task
	for _, task := range m.tasks {
		if task.WorkflowID == workflowID {
			result = append(result, *task)
		}
	}
	return result, nil
}

func (m *MockWorkflowStore) CancelWorkflow(ctx context.Context, workflowID string) error {
	for _, task := range m.tasks {
		if task.WorkflowID == workflowID && task.Status == agent.TaskPending {
			task.Status = agent.TaskCanceled
		}
	}
	return nil
}

func (m *MockWorkflowStore) SetTaskOutputFiles(ctx context.Context, taskID string, files []string) error {
	if task, ok := m.tasks[taskID]; ok {
		task.OutputFiles = files
	}
	return nil
}

// MockDataStore is an in-memory data store for testing
type MockDataStore struct {
	data map[string]string
}

func NewMockDataStore() *MockDataStore {
	return &MockDataStore{
		data: make(map[string]string),
	}
}

func (m *MockDataStore) Put(ctx context.Context, value any) (string, error) {
	ref := "ref-" + string(rune(len(m.data)))
	m.data[ref] = value.(string)
	return ref, nil
}

func (m *MockDataStore) Get(ctx context.Context, ref string, dest any) error {
	if s, ok := dest.(*string); ok {
		if val, exists := m.data[ref]; exists {
			*s = val
			return nil
		}
	}
	return nil
}

func (m *MockDataStore) Delete(ctx context.Context, ref string) error {
	delete(m.data, ref)
	return nil
}

func TestNewOrchestrator(t *testing.T) {
	store := NewMockWorkflowStore()
	dataStore := NewMockDataStore()
	logger := slog.Default()

	orch := NewOrchestrator(store, dataStore, nil, 0, logger)

	if orch == nil {
		t.Fatal("expected non-nil orchestrator")
	}
	if orch.workerN != 3 {
		t.Errorf("expected default workerN=3, got %d", orch.workerN)
	}
	if len(orch.retried) != 0 {
		t.Errorf("expected empty retried map, got %d entries", len(orch.retried))
	}
}

func TestNewOrchestratorCustomWorkerCount(t *testing.T) {
	store := NewMockWorkflowStore()
	dataStore := NewMockDataStore()
	logger := slog.Default()

	orch := NewOrchestrator(store, dataStore, nil, 5, logger)

	if orch.workerN != 5 {
		t.Errorf("expected workerN=5, got %d", orch.workerN)
	}
}

func TestSetTaskEventHook(t *testing.T) {
	store := NewMockWorkflowStore()
	dataStore := NewMockDataStore()
	logger := slog.Default()

	orch := NewOrchestrator(store, dataStore, nil, 3, logger)

	hookCalled := false
	orch.SetTaskEventHook(func(wfID, taskID, eventType, payload string) {
		hookCalled = true
	})

	orch.publishTaskEvent("wf1", "task1", "test", "payload")

	if !hookCalled {
		t.Error("expected task event hook to be called")
	}
}

func TestPublishTaskEvent(t *testing.T) {
	store := NewMockWorkflowStore()
	dataStore := NewMockDataStore()
	logger := slog.Default()

	orch := NewOrchestrator(store, dataStore, nil, 3, logger)

	eventsCaptured := []struct {
		wfID      string
		taskID    string
		eventType string
		payload   string
	}{}

	orch.SetTaskEventHook(func(wfID, taskID, eventType, payload string) {
		eventsCaptured = append(eventsCaptured, struct {
			wfID      string
			taskID    string
			eventType string
			payload   string
		}{wfID, taskID, eventType, payload})
	})

	orch.publishTaskEvent("wf1", "task1", "started", "data")

	if len(eventsCaptured) != 1 {
		t.Fatalf("expected 1 event, got %d", len(eventsCaptured))
	}
	if eventsCaptured[0].wfID != "wf1" {
		t.Errorf("expected wfID=wf1, got %s", eventsCaptured[0].wfID)
	}
	if eventsCaptured[0].taskID != "task1" {
		t.Errorf("expected taskID=task1, got %s", eventsCaptured[0].taskID)
	}
	if eventsCaptured[0].eventType != "started" {
		t.Errorf("expected eventType=started, got %s", eventsCaptured[0].eventType)
	}
}

func TestSetMemoryAutoSave(t *testing.T) {
	store := NewMockWorkflowStore()
	dataStore := NewMockDataStore()
	logger := slog.Default()

	orch := NewOrchestrator(store, dataStore, nil, 3, logger)

	if orch.memoryStore != nil {
		t.Error("expected initial memoryStore=nil")
	}

	memStore := &MockMemoryStore{}
	extractor := &MockInsightExtractor{}

	orch.SetMemoryAutoSave(memStore, extractor)

	if orch.memoryStore == nil {
		t.Error("expected memoryStore to be set")
	}
	if orch.insightExtractor == nil {
		t.Error("expected insightExtractor to be set")
	}
}

func TestSetCriticFunc(t *testing.T) {
	store := NewMockWorkflowStore()
	dataStore := NewMockDataStore()
	logger := slog.Default()

	orch := NewOrchestrator(store, dataStore, nil, 3, logger)

	if orch.criticFunc != nil {
		t.Error("expected initial criticFunc=nil")
	}

	criticFunc := func(ctx context.Context, wfID, taskID, instruction, output string) (string, string, bool, error) {
		return "{}", "", false, nil
	}

	orch.SetCriticFunc(criticFunc)

	if orch.criticFunc == nil {
		t.Error("expected criticFunc to be set")
	}
}

func TestStatus(t *testing.T) {
	store := NewMockWorkflowStore()
	dataStore := NewMockDataStore()
	logger := slog.Default()

	orch := NewOrchestrator(store, dataStore, nil, 3, logger)

	ctx := context.Background()

	// Create a workflow with tasks
	tasks := []agent.Task{
		{
			ID:         "task1",
			WorkflowID: "wf1",
			Name:       "Step 1",
			Status:     agent.TaskDone,
		},
		{
			ID:         "task2",
			WorkflowID: "wf1",
			Name:       "Step 2",
			Status:     agent.TaskRunning,
		},
	}

	store.CreateWorkflow(ctx, tasks)

	status, err := orch.Status(ctx, "wf1")

	if err != nil {
		t.Errorf("Status failed: %v", err)
	}
	if len(status) != 2 {
		t.Errorf("expected 2 tasks, got %d", len(status))
	}
}

func TestCancel(t *testing.T) {
	store := NewMockWorkflowStore()
	dataStore := NewMockDataStore()
	logger := slog.Default()

	orch := NewOrchestrator(store, dataStore, nil, 3, logger)

	ctx := context.Background()

	// Create workflow with pending tasks
	tasks := []agent.Task{
		{
			ID:         "task1",
			WorkflowID: "wf1",
			Name:       "Step 1",
			Status:     agent.TaskPending,
		},
	}

	store.CreateWorkflow(ctx, tasks)

	err := orch.Cancel(ctx, "wf1")

	if err != nil {
		t.Errorf("Cancel failed: %v", err)
	}

	status, _ := orch.Status(ctx, "wf1")
	if len(status) > 0 && status[0].Status != agent.TaskCanceled {
		t.Errorf("expected task to be canceled, got %s", status[0].Status)
	}
}

func TestStore(t *testing.T) {
	store := NewMockWorkflowStore()
	dataStore := NewMockDataStore()
	logger := slog.Default()

	orch := NewOrchestrator(store, dataStore, nil, 3, logger)

	returnedStore := orch.Store()

	if returnedStore != store {
		t.Error("expected Store() to return the workflow store")
	}
}

func TestIsFinalTask(t *testing.T) {
	store := NewMockWorkflowStore()
	dataStore := NewMockDataStore()
	logger := slog.Default()

	orch := NewOrchestrator(store, dataStore, nil, 3, logger)

	ctx := context.Background()

	// Create workflow with task dependencies
	tasks := []agent.Task{
		{
			ID:         "task1",
			WorkflowID: "wf1",
			Name:       "Step 1",
			Status:     agent.TaskDone,
			DependsOn:  []string{},
		},
		{
			ID:         "task2",
			WorkflowID: "wf1",
			Name:       "Step 2",
			Status:     agent.TaskDone,
			DependsOn:  []string{"task1"},
		},
	}

	store.CreateWorkflow(ctx, tasks)

	// task2 is final (nothing depends on it)
	if !orch.isFinalTask(ctx, &tasks[1]) {
		t.Error("expected task2 to be final")
	}

	// task1 is not final (task2 depends on it)
	if orch.isFinalTask(ctx, &tasks[0]) {
		t.Error("expected task1 to not be final")
	}
}

// MockMemoryStore is a stub for testing
type MockMemoryStore struct {
	saved []agent.Fact
}

func (m *MockMemoryStore) Save(ctx context.Context, fact agent.Fact) error {
	m.saved = append(m.saved, fact)
	return nil
}

func (m *MockMemoryStore) Search(ctx context.Context, query string, limit int) ([]agent.Fact, error) {
	return []agent.Fact{}, nil
}

func (m *MockMemoryStore) Delete(ctx context.Context, id string) error {
	return nil
}

// MockInsightExtractor is a stub for testing
type MockInsightExtractor struct{}

func (m *MockInsightExtractor) Complete(ctx context.Context, messages []agent.Message, tools []agent.ToolDefinition) (*agent.CompletionResult, error) {
	return &agent.CompletionResult{Content: "[]"}, nil
}
