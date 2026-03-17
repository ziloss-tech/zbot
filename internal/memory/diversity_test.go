package memory

import (
	"math"
	"testing"
)

func TestCosineSimilarity(t *testing.T) {
	tests := []struct {
		name string
		a, b []float32
		want float64
	}{
		{"identical", []float32{1, 0, 0}, []float32{1, 0, 0}, 1.0},
		{"orthogonal", []float32{1, 0, 0}, []float32{0, 1, 0}, 0.0},
		{"opposite", []float32{1, 0}, []float32{-1, 0}, -1.0},
		{"similar", []float32{1, 1, 0}, []float32{1, 1, 0.1}, 0.99},
		{"empty", []float32{}, []float32{}, 0.0},
		{"mismatch", []float32{1}, []float32{1, 2}, 0.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cosineSimilarity(tt.a, tt.b)
			if math.Abs(got-tt.want) > 0.05 {
				t.Errorf("cosineSimilarity = %f, want ~%f", got, tt.want)
			}
		})
	}
}

func TestDiversityReranker_RemovesDuplicates(t *testing.T) {
	r := NewDiversityReranker(0.95)

	// Create candidates where #1 and #2 are near-identical, #3 is different.
	candidates := []RankedFact{
		{ID: "1", Content: "User likes dark mode", Score: 0.9, Embedding: []float32{1, 0, 0}},
		{ID: "2", Content: "User prefers dark theme", Score: 0.85, Embedding: []float32{0.99, 0.01, 0}}, // near-duplicate of #1
		{ID: "3", Content: "Project deadline March 20", Score: 0.8, Embedding: []float32{0, 1, 0}},        // different
	}

	result := r.Rerank(candidates, 5)
	if len(result) != 2 {
		t.Fatalf("expected 2 diverse results, got %d", len(result))
	}
	if result[0].ID != "1" {
		t.Errorf("expected first result to be #1, got %s", result[0].ID)
	}
	if result[1].ID != "3" {
		t.Errorf("expected second result to be #3, got %s", result[1].ID)
	}
}

func TestDiversityReranker_RespectsLimit(t *testing.T) {
	r := NewDiversityReranker(0.92)

	candidates := []RankedFact{
		{ID: "1", Content: "A", Score: 0.9, Embedding: []float32{1, 0, 0}},
		{ID: "2", Content: "B", Score: 0.8, Embedding: []float32{0, 1, 0}},
		{ID: "3", Content: "C", Score: 0.7, Embedding: []float32{0, 0, 1}},
	}

	result := r.Rerank(candidates, 2)
	if len(result) != 2 {
		t.Fatalf("expected 2 results with limit, got %d", len(result))
	}
}

func TestDiversityReranker_EmptyInput(t *testing.T) {
	r := NewDiversityReranker(0.92)
	result := r.Rerank(nil, 5)
	if len(result) != 0 {
		t.Errorf("expected 0 results for nil input, got %d", len(result))
	}
}

func TestDiversityReranker_NoEmbeddings(t *testing.T) {
	r := NewDiversityReranker(0.92)

	// Without embeddings, should fall back to exact text match.
	candidates := []RankedFact{
		{ID: "1", Content: "same text", Score: 0.9},
		{ID: "2", Content: "same text", Score: 0.85},
		{ID: "3", Content: "different text", Score: 0.8},
	}

	result := r.Rerank(candidates, 5)
	if len(result) != 2 {
		t.Fatalf("expected 2 results (text dedup), got %d", len(result))
	}
}

func TestDiversityReranker_DefaultThreshold(t *testing.T) {
	r := NewDiversityReranker(0) // invalid → should use default
	if r.threshold != DefaultDiversityThreshold {
		t.Errorf("expected default threshold %f, got %f", DefaultDiversityThreshold, r.threshold)
	}
}
