// Package research implements the deep research pipeline.
// Core principle: deep research is created by iteration, not context window size.
// A model never researches AND verifies itself.
package research

import "time"

// ResearchPlan — output of the Planner agent.
type ResearchPlan struct {
	Goal               string   `json:"goal"`
	SubQuestions       []string `json:"sub_questions"`
	SearchTerms        []string `json:"search_terms"`
	Depth              string   `json:"depth"` // "shallow" | "deep" | "exhaustive"
	AcceptanceCriteria string   `json:"acceptance_criteria"`
}

// Source — one retrieved web source, ID-tagged for citation tracking.
type Source struct {
	ID      string `json:"id"`      // "SRC_001", "SRC_002" etc
	URL     string `json:"url"`
	Title   string `json:"title"`
	Snippet string `json:"snippet"`
}

// SourcesBlock — output of the Searcher agent.
type SourcesBlock struct {
	Query   string   `json:"query"`
	Sources []Source `json:"sources"`
}

// Claim — one atomic, verifiable fact extracted from sources.
type Claim struct {
	ID          string   `json:"id"`           // "CLM_001" etc
	Statement   string   `json:"statement"`
	EvidenceIDs []string `json:"evidence_ids"` // source IDs that support this
	Confidence  float64  `json:"confidence"`   // 0.0 - 1.0
}

// ClaimSet — output of the Extractor agent.
type ClaimSet struct {
	Claims    []Claim  `json:"claims"`
	Gaps      []string `json:"gaps"`       // things not found in sources
	SourceIDs []string `json:"source_ids"` // which sources were used
}

// CritiqueReport — output of the Critic agent (different provider than extractor by design).
type CritiqueReport struct {
	Passed            bool     `json:"passed"`
	UnsupportedClaims []string `json:"unsupported_claims"` // claim IDs with no evidence
	Contradictions    []string `json:"contradictions"`
	Gaps              []string `json:"gaps"`              // topics still not covered
	NewSubQuestions   []string `json:"new_sub_questions"` // follow-up research needed
	ConfidenceScore   float64  `json:"confidence_score"`  // 0.0 - 1.0
}

// ResearchState — full state tracked across loop iterations.
type ResearchState struct {
	WorkflowID  string         `json:"workflow_id"`
	Goal        string         `json:"goal"`
	Iteration   int            `json:"iteration"`
	MaxIter     int            `json:"max_iterations"`
	Plan        ResearchPlan   `json:"plan"`
	Sources     []Source       `json:"sources"`
	Claims      []Claim        `json:"claims"`
	Critique    CritiqueReport `json:"critique"`
	FinalReport string         `json:"final_report"` // markdown with citations
	Complete    bool           `json:"complete"`
	CostUSD     float64        `json:"cost_usd"`
	StartedAt   time.Time      `json:"started_at"`
	FinishedAt  time.Time      `json:"finished_at,omitempty"`
}

// ResearchSession is the DB row for a research session.
type ResearchSession struct {
	ID              string    `json:"id"`
	WorkflowID      string    `json:"workflow_id"`
	Goal            string    `json:"goal"`
	Status          string    `json:"status"` // running, complete, failed
	Iterations      int       `json:"iterations"`
	ConfidenceScore float64   `json:"confidence_score"`
	FinalReport     string    `json:"final_report"`
	StateJSON       string    `json:"state_json"` // serialized ResearchState
	CostUSD         float64   `json:"cost_usd"`
	Error           string    `json:"error,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}
