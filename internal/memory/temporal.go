// Package memory — Temporal Awareness (freshness + staleness detection).
// Phase 6 of the Memory Overhaul.
//
// Facts and packages decay over time. Stale information gets flagged so
// ZBOT says "I remember X but that was 6 weeks ago — want me to verify?"
// instead of confidently citing outdated information.
//
// Freshness scoring:
//   - Score starts at 1.0 when created
//   - Decays 0.03/day (reaches 0.3 "stale" threshold at ~23 days)
//   - Referenced facts get freshness reset to 1.0
//   - Nightly batch detects stale packages and flags them
package memory

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ziloss-tech/zbot/internal/agent"
)

// FreshnessConfig controls temporal awareness behavior.
type FreshnessConfig struct {
	DecayRate      float64 // freshness decay per day (default 0.03)
	StaleThreshold float64 // below this = stale (default 0.3, ~23 days)
}

// DefaultFreshnessConfig returns sensible defaults.
func DefaultFreshnessConfig() FreshnessConfig {
	return FreshnessConfig{DecayRate: 0.03, StaleThreshold: 0.3}
}

// CalculateFreshness returns a 0.0-1.0 freshness score based on age.
func CalculateFreshness(createdAt time.Time, cfg FreshnessConfig) float64 {
	days := time.Since(createdAt).Hours() / 24
	score := math.Exp(-cfg.DecayRate * days)
	if score < 0 {
		return 0
	}
	if score > 1 {
		return 1
	}
	return score
}

// IsStale returns true if a fact's freshness is below the threshold.
func IsStale(createdAt time.Time, cfg FreshnessConfig) bool {
	return CalculateFreshness(createdAt, cfg) < cfg.StaleThreshold
}

// DaysSince returns how many days ago a timestamp was.
func DaysSince(t time.Time) int {
	return int(time.Since(t).Hours() / 24)
}

// StalenessChecker scans packages and facts for stale entries.
type StalenessChecker struct {
	db     *pgxpool.Pool
	cfg    FreshnessConfig
	logger *slog.Logger
	ns     string
}

// NewStalenessChecker creates a staleness checker.
func NewStalenessChecker(db *pgxpool.Pool, cfg FreshnessConfig, logger *slog.Logger, ns string) *StalenessChecker {
	return &StalenessChecker{db: db, cfg: cfg, logger: logger, ns: ns}
}

// StalenessReport is the output of a staleness scan.
type StalenessReport struct {
	ScannedAt     time.Time       `json:"scanned_at"`
	TotalPackages int             `json:"total_packages"`
	StalePackages int             `json:"stale_packages"`
	FreshPackages int             `json:"fresh_packages"`
	StaleItems    []StaleItem     `json:"stale_items,omitempty"`
}

// StaleItem is a package or fact flagged as stale.
type StaleItem struct {
	ID        string  `json:"id"`
	Label     string  `json:"label"`
	DaysOld   int     `json:"days_old"`
	Freshness float64 `json:"freshness"`
}

// ScanPackages checks all thought packages for staleness.
func (sc *StalenessChecker) ScanPackages(ctx context.Context) (*StalenessReport, error) {
	tbl := sc.ns + "_thought_packages"
	sql := fmt.Sprintf(`
		SELECT id, label, freshness, created_at
		FROM %s ORDER BY freshness DESC
	`, tbl)

	rows, err := sc.db.Query(ctx, sql)
	if err != nil {
		return nil, fmt.Errorf("staleness scan: %w", err)
	}
	defer rows.Close()

	report := &StalenessReport{ScannedAt: time.Now()}
	for rows.Next() {
		var id, label string
		var freshness, createdAt time.Time
		if err := rows.Scan(&id, &label, &freshness, &createdAt); err != nil {
			continue
		}
		report.TotalPackages++
		score := CalculateFreshness(freshness, sc.cfg)
		if score < sc.cfg.StaleThreshold {
			report.StalePackages++
			report.StaleItems = append(report.StaleItems, StaleItem{
				ID: id, Label: label,
				DaysOld: DaysSince(freshness), Freshness: score,
			})
		} else {
			report.FreshPackages++
		}
	}

	sc.logger.Info("staleness scan complete",
		"total", report.TotalPackages,
		"stale", report.StalePackages,
		"fresh", report.FreshPackages,
	)
	return report, rows.Err()
}

// RefreshPackage resets a package's freshness timestamp (called when it's referenced).
func (sc *StalenessChecker) RefreshPackage(ctx context.Context, pkgID string) error {
	tbl := sc.ns + "_thought_packages"
	sql := fmt.Sprintf(`UPDATE %s SET freshness = NOW() WHERE id = $1`, tbl)
	_, err := sc.db.Exec(ctx, sql, pkgID)
	return err
}

// AnnotateStale adds staleness warnings to package content for prompt injection.
// If a package is stale, wraps content with a temporal caveat.
func AnnotateStale(pkg agent.ThoughtPackage, cfg FreshnessConfig) string {
	score := CalculateFreshness(pkg.Freshness, cfg)
	if score >= cfg.StaleThreshold {
		return pkg.Content
	}
	days := DaysSince(pkg.Freshness)
	return fmt.Sprintf("[NOTE: This information is %d days old and may be outdated — verify before citing.]\n%s", days, pkg.Content)
}
