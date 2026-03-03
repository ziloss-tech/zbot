package research

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DailyBudgetUSD is the hard cap: $200/month / 30 days.
const DailyBudgetUSD = 6.67

// BudgetTracker enforces the daily spend limit for research sessions.
type BudgetTracker struct {
	db *pgxpool.Pool
}

// NewBudgetTracker creates a tracker backed by Postgres.
func NewBudgetTracker(db *pgxpool.Pool) *BudgetTracker {
	return &BudgetTracker{db: db}
}

// CheckBudget returns an error if the daily budget would be exceeded.
func (bt *BudgetTracker) CheckBudget(ctx context.Context, estimatedCost float64) error {
	spent := bt.GetTodaySpend(ctx)
	if spent+estimatedCost > DailyBudgetUSD {
		return fmt.Errorf("daily budget exceeded: spent $%.2f of $%.2f today — try again tomorrow", spent, DailyBudgetUSD)
	}
	return nil
}

// GetTodaySpend returns the total USD spent today.
func (bt *BudgetTracker) GetTodaySpend(ctx context.Context) float64 {
	today := time.Now().Truncate(24 * time.Hour)
	var total float64
	err := bt.db.QueryRow(ctx,
		`SELECT COALESCE(SUM(cost_usd), 0) FROM zbot_model_spend WHERE recorded_at >= $1`,
		today,
	).Scan(&total)
	if err != nil {
		return 0
	}
	return total
}

// RecordSpend adds actual cost after a model call.
func (bt *BudgetTracker) RecordSpend(ctx context.Context, sessionID, modelID string, promptTokens, completionTokens int, costUSD float64) {
	_, _ = bt.db.Exec(ctx,
		`INSERT INTO zbot_model_spend (session_id, model_id, prompt_tokens, completion_tokens, cost_usd, recorded_at)
		 VALUES ($1, $2, $3, $4, $5, NOW())`,
		sessionID, modelID, promptTokens, completionTokens, costUSD,
	)
}
