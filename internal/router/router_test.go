package router

import "testing"

func TestClassifyTask(t *testing.T) {
	tests := []struct {
		input    string
		expected TaskCategory
	}{
		{"write a Go function to parse JSON", TaskCoding},
		{"review this code for bugs", TaskCodeReview},
		{"calculate the derivative of x^3", TaskMath},
		{"research quantum computing trends", TaskResearch},
		{"extract emails from this PDF", TaskExtract},
		{"classify this support ticket", TaskClassify},
		{"draft an email to the team", TaskWriting},
		{"search the web for AI news", TaskToolUse},
		{"analyze this screenshot", TaskMultimodal},
		{"compare microservices vs monolith architecture", TaskReasoning},
		{"hey what's up", TaskConversation},
	}
	for _, tt := range tests {
		got := ClassifyTask(tt.input)
		if got != tt.expected {
			t.Errorf("ClassifyTask(%q) = %s, want %s", tt.input, got, tt.expected)
		}
	}
}

func TestRouteReturnsResults(t *testing.T) {
	r := NewRouter(Preferences{})
	for _, task := range []TaskCategory{TaskCoding, TaskMath, TaskReasoning, TaskWriting, TaskConversation} {
		recs := r.Route(task)
		if len(recs) == 0 {
			t.Errorf("Route(%s) returned 0 recommendations", task)
		}
		for _, rec := range recs {
			if rec.TaskScore <= 0 {
				t.Errorf("Route(%s): %s has score %.1f", task, rec.Model.Name, rec.TaskScore)
			}
			if rec.CostEfficiency <= 0 {
				t.Errorf("Route(%s): %s has efficiency %.1f", task, rec.Model.Name, rec.CostEfficiency)
			}
		}
	}
}

func TestBestModelQualityVsCost(t *testing.T) {
	r := NewRouter(Preferences{})

	quality := r.BestModel(TaskCoding, true)
	budget := r.BestModel(TaskCoding, false)

	if quality == nil || budget == nil {
		t.Fatal("BestModel returned nil")
	}

	// Quality pick should have higher or equal score.
	if quality.TaskScore < budget.TaskScore {
		t.Errorf("Quality pick (%s, %.1f) scored lower than budget pick (%s, %.1f)",
			quality.Model.Name, quality.TaskScore, budget.Model.Name, budget.TaskScore)
	}

	// Budget pick should have higher or equal efficiency.
	if budget.CostEfficiency < quality.CostEfficiency {
		t.Errorf("Budget pick (%s, %.0f) less efficient than quality pick (%s, %.0f)",
			budget.Model.Name, budget.CostEfficiency, quality.Model.Name, quality.CostEfficiency)
	}

	t.Logf("Coding — Quality: %s (score=%.1f), Budget: %s (eff=%.0f)",
		quality.Model.Name, quality.TaskScore, budget.Model.Name, budget.CostEfficiency)
}

func TestPreferAmerican(t *testing.T) {
	r := NewRouter(Preferences{PreferAmerican: true})
	recs := r.Route(TaskCoding)
	for _, rec := range recs {
		switch rec.Model.Provider {
		case Anthropic, OpenAI, Google, XAI:
			// OK
		default:
			t.Errorf("PreferAmerican: got provider %s (%s)", rec.Model.Provider, rec.Model.Name)
		}
	}
}

func TestBlockProvider(t *testing.T) {
	r := NewRouter(Preferences{BlockedProviders: []Provider{DeepSeek}})
	recs := r.Route(TaskCoding)
	for _, rec := range recs {
		if rec.Model.Provider == DeepSeek {
			t.Errorf("Blocked provider DeepSeek still returned: %s", rec.Model.Name)
		}
	}
}

func TestMaxCostFilter(t *testing.T) {
	r := NewRouter(Preferences{MaxCostPerQuery: 0.001}) // Very cheap only
	recs := r.Route(TaskCoding)
	for _, rec := range recs {
		estCost := (500.0 / 1_000_000.0) * rec.Model.OutputPer1M
		if estCost > 0.001 {
			t.Errorf("MaxCost filter: %s costs $%.4f per query (limit $0.001)",
				rec.Model.Name, estCost)
		}
	}
}

func TestAllTaskCategories(t *testing.T) {
	r := NewRouter(Preferences{})
	categories := []TaskCategory{
		TaskCoding, TaskCodeReview, TaskMath, TaskReasoning,
		TaskResearch, TaskExtract, TaskClassify, TaskWriting,
		TaskToolUse, TaskMultimodal, TaskConversation,
	}
	for _, cat := range categories {
		rec := r.BestModel(cat, false)
		if rec == nil {
			t.Errorf("BestModel(%s) returned nil", cat)
			continue
		}
		t.Logf("%-15s → %s (score=%.1f, eff=%.0f, $%.2f/1M out)",
			cat, rec.Model.Name, rec.TaskScore, rec.CostEfficiency, rec.Model.OutputPer1M)
	}
}
