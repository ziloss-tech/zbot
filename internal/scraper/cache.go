package scraper

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// ScrapeCache caches URL responses for 24 hours.
// Prevents redundant fetches within a session.
// Uses pure Go SQLite (modernc.org/sqlite) — no CGO required.
type ScrapeCache struct {
	db  *sql.DB
	ttl time.Duration
}

// NewScrapeCache opens or creates a SQLite cache at dbPath.
// Default TTL is 24 hours.
func NewScrapeCache(dbPath string) (*ScrapeCache, error) {
	// Ensure parent directory exists.
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o750); err != nil {
		return nil, fmt.Errorf("create cache dir: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open cache DB: %w", err)
	}

	// Create schema.
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS scrape_cache (
			url        TEXT PRIMARY KEY,
			content    TEXT NOT NULL,
			fetched_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("create cache table: %w", err)
	}

	return &ScrapeCache{db: db, ttl: 24 * time.Hour}, nil
}

// Get returns cached content for a URL if it's still fresh.
func (c *ScrapeCache) Get(url string) (content string, found bool) {
	row := c.db.QueryRow(
		`SELECT content FROM scrape_cache WHERE url = ? AND fetched_at > ?`,
		url, time.Now().Add(-c.ttl).Format(time.RFC3339),
	)
	if err := row.Scan(&content); err != nil {
		return "", false
	}
	return content, true
}

// Set stores content for a URL in the cache.
func (c *ScrapeCache) Set(url string, content string) error {
	_, err := c.db.Exec(
		`INSERT OR REPLACE INTO scrape_cache (url, content, fetched_at) VALUES (?, ?, ?)`,
		url, content, time.Now().Format(time.RFC3339),
	)
	return err
}

// Prune deletes entries older than the TTL.
func (c *ScrapeCache) Prune() error {
	_, err := c.db.Exec(
		`DELETE FROM scrape_cache WHERE fetched_at < ?`,
		time.Now().Add(-c.ttl).Format(time.RFC3339),
	)
	return err
}

// Close closes the underlying database.
func (c *ScrapeCache) Close() error {
	return c.db.Close()
}
