// Package memory — Sprint D: Diversity re-ranking filter.
//
// After retrieving top-k candidates from pgvector, this filter removes
// near-duplicates so the model sees N distinct pieces of context rather
// than N variations of the same memory.
//
// Algorithm: greedy selection with cosine similarity threshold.
// 1. Retrieve top-k=20 candidates.
// 2. For each candidate (in score order), check cosine similarity
//    against all already-selected results.
// 3. If similarity < threshold (default 0.92), keep it.
// 4. Return top-N diverse results.
package memory

import (
	"math"
)

// DefaultDiversityThreshold is the cosine similarity threshold for dedup.
// Facts with similarity >= this threshold are considered near-duplicates.
const DefaultDiversityThreshold = 0.92

// DiversityReranker removes near-duplicate results from a memory search.
type DiversityReranker struct {
	threshold float64
}

// NewDiversityReranker creates a reranker with the given cosine similarity threshold.
func NewDiversityReranker(threshold float64) *DiversityReranker {
	if threshold <= 0 || threshold > 1 {
		threshold = DefaultDiversityThreshold
	}
	return &DiversityReranker{threshold: threshold}
}

// RankedFact pairs a fact's text with its embedding for diversity checking.
type RankedFact struct {
	ID        string
	Content   string
	Score     float32
	Embedding []float32
}

// Rerank filters candidates to keep only diverse results.
// Expects candidates in descending score order.
// Returns at most `limit` diverse results.
func (r *DiversityReranker) Rerank(candidates []RankedFact, limit int) []RankedFact {
	if len(candidates) == 0 || limit <= 0 {
		return nil
	}

	var selected []RankedFact
	for _, c := range candidates {
		if len(selected) >= limit {
			break
		}

		// Check if this candidate is too similar to any already-selected result.
		if r.isDuplicate(c, selected) {
			continue
		}

		selected = append(selected, c)
	}

	return selected
}

// isDuplicate checks if candidate is a near-duplicate of any selected result.
func (r *DiversityReranker) isDuplicate(candidate RankedFact, selected []RankedFact) bool {
	// If no embedding available, use text-based dedup.
	if len(candidate.Embedding) == 0 {
		for _, s := range selected {
			if candidate.Content == s.Content {
				return true
			}
		}
		return false
	}

	for _, s := range selected {
		if len(s.Embedding) == 0 {
			continue
		}
		sim := cosineSimilarity(candidate.Embedding, s.Embedding)
		if sim >= r.threshold {
			return true
		}
	}
	return false
}

// cosineSimilarity computes cosine similarity between two vectors.
func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}

	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}

	denom := math.Sqrt(normA) * math.Sqrt(normB)
	if denom == 0 {
		return 0
	}
	return dot / denom
}
