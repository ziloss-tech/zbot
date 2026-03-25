package reviewer

import "time"

type ReviewConfig struct {
	Enabled           bool
	ReviewInterval    time.Duration
	ModelEndpoint     string
	ModelName         string
	APIKey            string
	MaxCostPerDay     float64
	MaxChunksPerCycle int
	MinSeverity       string
	InputCostPer1M    float64
	OutputCostPer1M   float64
}

func DefaultReviewConfig() ReviewConfig {
	return ReviewConfig{
		Enabled:           false,
		ReviewInterval:    60 * time.Second,
		ModelEndpoint:     "https://api.openai.com/v1/chat/completions",
		ModelName:         "gpt-4o-mini",
		MaxCostPerDay:     2.00,
		MaxChunksPerCycle: 10,
		MinSeverity:       "warning",
		InputCostPer1M:    0.15,
		OutputCostPer1M:   0.60,
	}
}
