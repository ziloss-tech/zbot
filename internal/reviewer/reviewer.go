package reviewer

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/ziloss-tech/zbot/internal/agent"
)

// CheckerI is the interface for review checking.
type CheckerI interface {
	Check(ctx context.Context, chunk ReviewChunk) ([]ReviewFinding, error)
}

type ReviewEngine struct {
	config    ReviewConfig
	checker   CheckerI
	eventBus  agent.EventBus
	logger    *slog.Logger
	stopCh   chan struct{}
	mu        sync.Mutex
	running   bool
	todayCost float64
	lastReset time.Time
}

func NewReviewEngine(cfg ReviewConfig, eventBus agent.EventBus, logger *slog.Logger) *ReviewEngine {
	var checker CheckerI
	if cfg.APIKey != "" {
		checker = NewChecker(cfg.ModelEndpoint, cfg.ModelName, cfg.APIKey)
	}
	return &ReviewEngine{
		config:    cfg,
		checker:   checker,
		eventBus:  eventBus,
		logger:    logger,
		stopCh:    make(chan struct{}),
		lastReset: time.Now(),
	}
}

func (r *ReviewEngine) Start() {
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return
	}
	r.running = true
	r.mu.Unlock()

	go r.loop()
}

func (r *ReviewEngine) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.running {
		return
	}
	r.running = false
	close(r.stopCh)
}

func (r *ReviewEngine) loop() {
	ticker := time.NewTicker(r.config.ReviewInterval)
	defer ticker.Stop()

	for {
		select {
		case <-r.stopCh:
			r.logger.Info("reviewer stopped")
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			findings, err := r.ReviewOnce(ctx)
			cancel()
			if err != nil {
				r.logger.Warn("review cycle failed", "err", err)
			} else if len(findings) > 0 {
				r.logger.Info("review cycle complete", "findings", len(findings))
			}
		}
	}
}

func (r *ReviewEngine) ReviewOnce(ctx context.Context) ([]ReviewFinding, error) {
	if r.checker == nil {
		return nil, fmt.Errorf("no checker configured (missing API key)")
	}

	// Reset daily cost at midnight
	now := time.Now()
	if now.Day() != r.lastReset.Day() {
		r.mu.Lock()
		r.todayCost = 0
		r.lastReset = now
		r.mu.Unlock()
	}

	// Check cost cap
	r.mu.Lock()
	if r.todayCost >= r.config.MaxCostPerDay {
		r.mu.Unlock()
		r.logger.Info("daily cost cap reached, skipping review", "cost", r.todayCost)
		return nil, nil
	}
	r.mu.Unlock()

	// Get chunks from the most recently active session
	// For now, use "web-chat" as the default session ID
	sessionID := "web-chat"
	chunks := ChunkRecentEvents(r.eventBus, sessionID, r.config.MaxChunksPerCycle)
	if len(chunks) == 0 {
		return nil, nil // nothing to review
	}

	r.eventBus.Emit(ctx, agent.AgentEvent{
		SessionID: sessionID,
		Type:      EventReviewCycle,
		Summary:   fmt.Sprintf("Review cycle: %d chunks", len(chunks)),
		Timestamp: now,
	})

	var allFindings []ReviewFinding
	for _, chunk := range chunks {
		findings, err := r.checker.Check(ctx, chunk)
		if err != nil {
			r.logger.Warn("chunk review failed", "chunk", chunk.ID, "err", err)
			r.eventBus.Emit(ctx, agent.AgentEvent{
				SessionID: sessionID,
				Type:      EventReviewError,
				Summary:   fmt.Sprintf("Review error: %v", err),
				Timestamp: time.Now(),
			})
			continue
		}

		// Estimate cost
		inputTokens := float64(chunk.TokenEst)
		outputTokens := float64(len(findings) * 50) // rough estimate
		cost := (inputTokens/1_000_000)*r.config.InputCostPer1M + (outputTokens/1_000_000)*r.config.OutputCostPer1M
		r.mu.Lock()
		r.todayCost += cost
		r.mu.Unlock()

		// Filter by severity and emit
		for _, f := range findings {
			if !meetsSeverity(f.Severity, r.config.MinSeverity) {
				continue
			}
			allFindings = append(allFindings, f)
			r.eventBus.Emit(ctx, agent.AgentEvent{
				SessionID: sessionID,
				Type:      EventReviewFinding,
				Summary:   fmt.Sprintf("[%s] %s: %s", f.Severity, f.Category, f.Description),
				Detail: map[string]any{
					"severity":    f.Severity,
					"category":    f.Category,
					"description": f.Description,
					"location":    f.Location,
					"suggestion":  f.Suggestion,
					"confidence":  f.Confidence,
					"model":       f.ReviewerModel,
				},
				Timestamp: f.Timestamp,
			})
		}
	}

	return allFindings, nil
}

func meetsSeverity(severity, minSeverity string) bool {
	levels := map[string]int{"info": 0, "warning": 1, "critical": 2}
	s, ok1 := levels[severity]
	m, ok2 := levels[minSeverity]
	if !ok1 || !ok2 {
		return true
	}
	return s >= m
}
