package factory

import (
	"context"
	"testing"
	"time"
)

func TestMemSessionStoreSaveAndLoad(t *testing.T) {
	store := NewMemSessionStore()
	ctx := context.Background()

	state := &PlanStateV2{
		ID:        "test-plan-1",
		Idea:      "build a todo app",
		Phase:     PhaseInterview,
		StartedAt: time.Now(),
		Decisions: NewDecisionLog(),
	}
	state.Decisions.Record(Decision{
		Agent:     "interviewer",
		Phase:     "interview",
		Choice:    "ask about auth",
		Rationale: "need to understand security requirements",
	})

	// Save.
	if err := store.Save(ctx, state); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Load.
	loaded, err := store.Load(ctx, "test-plan-1")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loaded.ID != "test-plan-1" {
		t.Errorf("expected ID 'test-plan-1', got %q", loaded.ID)
	}
	if loaded.Idea != "build a todo app" {
		t.Errorf("expected idea 'build a todo app', got %q", loaded.Idea)
	}
	if loaded.Phase != PhaseInterview {
		t.Errorf("expected phase interview, got %q", loaded.Phase)
	}

	// Decisions should be restored.
	decisions := loaded.Decisions.All()
	if len(decisions) != 1 {
		t.Fatalf("expected 1 decision, got %d", len(decisions))
	}
	if decisions[0].Agent != "interviewer" {
		t.Errorf("expected decision agent 'interviewer', got %q", decisions[0].Agent)
	}
}

func TestMemSessionStoreLoadNotFound(t *testing.T) {
	store := NewMemSessionStore()
	_, err := store.Load(context.Background(), "nonexistent")
	if err == nil {
		t.Error("expected error loading nonexistent session")
	}
}

func TestMemSessionStoreLoadIncomplete(t *testing.T) {
	store := NewMemSessionStore()
	ctx := context.Background()

	// Save one incomplete and one complete session.
	incomplete := &PlanStateV2{
		ID: "plan-incomplete", Idea: "idea1", Phase: PhaseArchitect,
		StartedAt: time.Now(), Decisions: NewDecisionLog(),
	}
	complete := &PlanStateV2{
		ID: "plan-complete", Idea: "idea2", Phase: PhaseComplete,
		StartedAt: time.Now(), Decisions: NewDecisionLog(),
	}
	_ = store.Save(ctx, incomplete)
	_ = store.Save(ctx, complete)

	sessions, err := store.LoadIncomplete(ctx)
	if err != nil {
		t.Fatalf("LoadIncomplete failed: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 incomplete session, got %d", len(sessions))
	}
	if sessions[0].ID != "plan-incomplete" {
		t.Errorf("expected 'plan-incomplete', got %q", sessions[0].ID)
	}
}

func TestMemSessionStoreDelete(t *testing.T) {
	store := NewMemSessionStore()
	ctx := context.Background()

	state := &PlanStateV2{
		ID: "plan-delete", Idea: "idea", Phase: PhaseInterview,
		StartedAt: time.Now(), Decisions: NewDecisionLog(),
	}
	_ = store.Save(ctx, state)

	// Should load fine.
	_, err := store.Load(ctx, "plan-delete")
	if err != nil {
		t.Fatalf("Load before delete failed: %v", err)
	}

	// Delete.
	if err := store.Delete(ctx, "plan-delete"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Should fail now.
	_, err = store.Load(ctx, "plan-delete")
	if err == nil {
		t.Error("expected error after delete")
	}
}

// Verify both stores implement SessionStore.
var _ SessionStore = (*MemSessionStore)(nil)
var _ SessionStore = (*PGSessionStore)(nil)
