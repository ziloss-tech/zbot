package reviewer

import (
	"time"

	"github.com/ziloss-tech/zbot/internal/agent"
)

type ReviewFinding struct {
	ID            string  `json:"id"`
	Severity      string  `json:"severity"`
	Category      string  `json:"category"`
	Description   string  `json:"description"`
	Location      string  `json:"location"`
	Suggestion    string  `json:"suggestion"`
	Confidence    float64 `json:"confidence"`
	ReviewerModel string  `json:"reviewer_model"`
	Timestamp     time.Time `json:"timestamp"`
}

const (
	EventReviewFinding agent.EventType = "review_finding"
	EventReviewCycle   agent.EventType = "review_cycle"
	EventReviewError   agent.EventType = "review_error"
)
