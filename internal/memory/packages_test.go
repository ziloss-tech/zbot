package memory

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/ziloss-tech/zbot/internal/agent"
)

// ─── IN-MEMORY PACKAGE STORE (for tests) ─────────────────────────────────────

// InMemoryPackageStore implements agent.PackageStore without a database.
type InMemoryPackageStore struct {
	mu   sync.RWMutex
	pkgs map[string]agent.ThoughtPackage
}

func NewInMemoryPackageStore() *InMemoryPackageStore {
	return &InMemoryPackageStore{pkgs: make(map[string]agent.ThoughtPackage)}
}

func (s *InMemoryPackageStore) SavePackage(_ context.Context, pkg agent.ThoughtPackage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if pkg.CreatedAt.IsZero() {
		pkg.CreatedAt = now
	}
	pkg.UpdatedAt = now
	if pkg.Freshness.IsZero() {
		pkg.Freshness = now
	}
	s.pkgs[pkg.ID] = pkg
	return nil
}

func (s *InMemoryPackageStore) GetPackage(_ context.Context, id string) (*agent.ThoughtPackage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	pkg, ok := s.pkgs[id]
	if !ok {
		return nil, fmt.Errorf("package not found: %s", id)
	}
	return &pkg, nil
}

func (s *InMemoryPackageStore) ListPackages(_ context.Context) ([]agent.ThoughtPackage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	pkgs := make([]agent.ThoughtPackage, 0, len(s.pkgs))
	for _, p := range s.pkgs {
		pkgs = append(pkgs, p)
	}
	sort.Slice(pkgs, func(i, j int) bool {
		if pkgs[i].Priority != pkgs[j].Priority {
			return pkgs[i].Priority < pkgs[j].Priority
		}
		return pkgs[j].Freshness.Before(pkgs[i].Freshness)
	})
	return pkgs, nil
}

func (s *InMemoryPackageStore) DeletePackage(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.pkgs, id)
	return nil
}

func (s *InMemoryPackageStore) MatchPackages(_ context.Context, query string, tokenBudget int) ([]agent.PackageMatch, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	queryWords := tokenize(query)
	var matches []agent.PackageMatch

	for _, pkg := range s.pkgs {
		score := keywordScore(queryWords, pkg.Keywords)
		if score > 0 {
			matches = append(matches, agent.PackageMatch{
				Package: pkg,
				Score:   score,
				Method:  "keyword",
			})
		}
	}

	sort.Slice(matches, func(i, j int) bool {
		return matches[i].Score > matches[j].Score
	})

	if tokenBudget > 0 {
		var budgeted []agent.PackageMatch
		used := 0
		for _, m := range matches {
			if used+m.Package.TokenCount > tokenBudget {
				continue
			}
			budgeted = append(budgeted, m)
			used += m.Package.TokenCount
		}
		matches = budgeted
	}
	return matches, nil
}

func (s *InMemoryPackageStore) AlwaysPackages(_ context.Context) ([]agent.ThoughtPackage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var always []agent.ThoughtPackage
	for _, p := range s.pkgs {
		if p.Priority == agent.PackageAlways {
			always = append(always, p)
		}
	}
	return always, nil
}

var _ agent.PackageStore = (*InMemoryPackageStore)(nil)


// ─── TESTS ───────────────────────────────────────────────────────────────────

func TestPackageStore_CRUD(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryPackageStore()

	pkg := agent.ThoughtPackage{
		ID:         "pkg-ghl",
		Label:      "ghl/workflow-migration",
		Keywords:   []string{"ghl", "workflow", "migration", "automation"},
		Content:    "GHL workflow migration tool clones workflows between locations...",
		TokenCount: 150,
		MemoryIDs:  []string{"mem-1", "mem-2"},
		Priority:   agent.PackageAuto,
		Version:    1,
	}

	// Save
	if err := store.SavePackage(ctx, pkg); err != nil {
		t.Fatalf("SavePackage: %v", err)
	}

	// Get
	got, err := store.GetPackage(ctx, "pkg-ghl")
	if err != nil {
		t.Fatalf("GetPackage: %v", err)
	}
	if got.Label != "ghl/workflow-migration" {
		t.Errorf("Label = %q, want %q", got.Label, "ghl/workflow-migration")
	}
	if got.TokenCount != 150 {
		t.Errorf("TokenCount = %d, want 150", got.TokenCount)
	}
	if got.Priority != agent.PackageAuto {
		t.Errorf("Priority = %d, want %d", got.Priority, agent.PackageAuto)
	}

	// List
	pkgs, err := store.ListPackages(ctx)
	if err != nil {
		t.Fatalf("ListPackages: %v", err)
	}
	if len(pkgs) != 1 {
		t.Fatalf("ListPackages len = %d, want 1", len(pkgs))
	}

	// Delete
	if err := store.DeletePackage(ctx, "pkg-ghl"); err != nil {
		t.Fatalf("DeletePackage: %v", err)
	}
	pkgs, _ = store.ListPackages(ctx)
	if len(pkgs) != 0 {
		t.Errorf("after delete, len = %d, want 0", len(pkgs))
	}
}

func TestKeywordScore(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		keywords []string
		wantMin  float32
		wantMax  float32
	}{
		{"exact match all", "ghl workflow", []string{"ghl", "workflow"}, 0.9, 1.1},
		{"partial match", "ghl migration tool", []string{"ghl", "migration"}, 0.5, 0.8},
		{"no match", "cooking recipes", []string{"ghl", "workflow"}, 0, 0},
		{"substring match", "workflows", []string{"workflow"}, 0.4, 0.6},
		{"empty query", "", []string{"ghl"}, 0, 0},
		{"empty keywords", "ghl", []string{}, 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			qw := tokenize(tt.query)
			score := keywordScore(qw, tt.keywords)
			if score < tt.wantMin || score > tt.wantMax {
				t.Errorf("keywordScore(%q, %v) = %f, want [%f, %f]",
					tt.query, tt.keywords, score, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestTokenize(t *testing.T) {
	words := tokenize("  GHL  Workflow  ghl  migration  A  ")
	// Should be lowercase, deduplicated, no single chars
	if len(words) != 3 {
		t.Errorf("tokenize got %d words %v, want 3", len(words), words)
	}
}

func TestMatchPackages(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryPackageStore()

	// Seed packages
	_ = store.SavePackage(ctx, agent.ThoughtPackage{
		ID: "pkg-ghl", Label: "ghl/workflows",
		Keywords: []string{"ghl", "workflow", "migration", "automation"},
		Content: "GHL workflow stuff", TokenCount: 200, Priority: agent.PackageAuto, Version: 1,
	})
	_ = store.SavePackage(ctx, agent.ThoughtPackage{
		ID: "pkg-zbot", Label: "projects/zbot",
		Keywords: []string{"zbot", "go", "agent", "memory", "pantheon"},
		Content: "ZBOT is an AI agent", TokenCount: 300, Priority: agent.PackageAuto, Version: 1,
	})
	_ = store.SavePackage(ctx, agent.ThoughtPackage{
		ID: "pkg-identity", Label: "identity",
		Keywords: []string{"jeremy", "ziloss", "lead", "certain"},
		Content: "Jeremy is the founder of Ziloss", TokenCount: 100, Priority: agent.PackageAlways, Version: 1,
	})

	// Match "ghl workflow migration"
	matches, err := store.MatchPackages(ctx, "ghl workflow migration", 0)
	if err != nil {
		t.Fatalf("MatchPackages: %v", err)
	}
	if len(matches) == 0 {
		t.Fatal("expected matches for 'ghl workflow migration'")
	}
	if matches[0].Package.ID != "pkg-ghl" {
		t.Errorf("top match = %q, want pkg-ghl", matches[0].Package.ID)
	}
	if matches[0].Method != "keyword" {
		t.Errorf("method = %q, want keyword", matches[0].Method)
	}

	// Token budget — only 250 tokens, should exclude the 300-token zbot package
	matches, _ = store.MatchPackages(ctx, "zbot memory agent ghl workflow", 250)
	totalTokens := 0
	for _, m := range matches {
		totalTokens += m.Package.TokenCount
	}
	if totalTokens > 250 {
		t.Errorf("token budget exceeded: got %d, limit 250", totalTokens)
	}
}

func TestAlwaysPackages(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryPackageStore()

	_ = store.SavePackage(ctx, agent.ThoughtPackage{
		ID: "pkg-always", Label: "identity",
		Keywords: []string{"identity"}, Content: "always on",
		TokenCount: 50, Priority: agent.PackageAlways, Version: 1,
	})
	_ = store.SavePackage(ctx, agent.ThoughtPackage{
		ID: "pkg-auto", Label: "projects/zbot",
		Keywords: []string{"zbot"}, Content: "auto matched",
		TokenCount: 100, Priority: agent.PackageAuto, Version: 1,
	})

	always, err := store.AlwaysPackages(ctx)
	if err != nil {
		t.Fatalf("AlwaysPackages: %v", err)
	}
	if len(always) != 1 {
		t.Errorf("AlwaysPackages len = %d, want 1", len(always))
	}
	if len(always) > 0 && always[0].ID != "pkg-always" {
		t.Errorf("AlwaysPackages[0].ID = %q, want pkg-always", always[0].ID)
	}
}

func TestListPackages_PriorityOrder(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryPackageStore()

	_ = store.SavePackage(ctx, agent.ThoughtPackage{
		ID: "pkg-auto", Label: "auto", Priority: agent.PackageAuto,
		Keywords: []string{"test"}, Content: "auto", TokenCount: 10, Version: 1,
	})
	_ = store.SavePackage(ctx, agent.ThoughtPackage{
		ID: "pkg-always", Label: "always", Priority: agent.PackageAlways,
		Keywords: []string{"test"}, Content: "always", TokenCount: 10, Version: 1,
	})
	_ = store.SavePackage(ctx, agent.ThoughtPackage{
		ID: "pkg-demand", Label: "demand", Priority: agent.PackageOnDemand,
		Keywords: []string{"test"}, Content: "demand", TokenCount: 10, Version: 1,
	})

	pkgs, _ := store.ListPackages(ctx)
	if len(pkgs) != 3 {
		t.Fatalf("len = %d, want 3", len(pkgs))
	}
	if pkgs[0].Priority != agent.PackageAlways {
		t.Errorf("first package priority = %d, want Always(0)", pkgs[0].Priority)
	}
	if pkgs[2].Priority != agent.PackageOnDemand {
		t.Errorf("last package priority = %d, want OnDemand(2)", pkgs[2].Priority)
	}
}
