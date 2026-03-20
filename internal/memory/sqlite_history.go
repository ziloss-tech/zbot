// Package memory — sqlite_history.go provides persistent conversation history
// using pure-Go SQLite (modernc.org/sqlite). No CGO required.
//
// Stores message history per session so conversations survive restarts.
// DB file lives at {workspaceRoot}/.cache/history.db.
package memory

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ziloss-tech/zbot/internal/agent"

	_ "modernc.org/sqlite"
)

// SQLiteHistory stores conversation messages in a local SQLite database.
type SQLiteHistory struct {
	db *sql.DB
}

// NewSQLiteHistory opens (or creates) the history database.
// dbPath should be something like {workspaceRoot}/.cache/history.db.
func NewSQLiteHistory(dbPath string) (*SQLiteHistory, error) {
	// Ensure parent directory exists.
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o750); err != nil {
		return nil, fmt.Errorf("create history db dir: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open history db: %w", err)
	}

	// Create table if not exists.
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS conversations (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id TEXT NOT NULL,
		role TEXT NOT NULL,
		content TEXT NOT NULL,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("create conversations table: %w", err)
	}

	// Index for session lookups.
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_conv_session ON conversations(session_id, created_at)`)

	return &SQLiteHistory{db: db}, nil
}

// SaveMessage persists a single message.
// Rejects empty content to prevent Anthropic API "text content blocks must be non-empty" errors.
func (h *SQLiteHistory) SaveMessage(sessionID, role, content string) error {
	if content == "" {
		return nil // silently skip empty messages
	}
	_, err := h.db.Exec(
		`INSERT INTO conversations (session_id, role, content, created_at) VALUES (?, ?, ?, ?)`,
		sessionID, role, content, time.Now().UTC(),
	)
	return err
}

// LoadHistory returns the last `limit` messages for a session, oldest first.
func (h *SQLiteHistory) LoadHistory(sessionID string, limit int) ([]agent.Message, error) {
	rows, err := h.db.Query(
		`SELECT role, content, created_at FROM conversations
		 WHERE session_id = ?
		 ORDER BY id DESC LIMIT ?`,
		sessionID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []agent.Message
	for rows.Next() {
		var roleStr, content string
		var createdAt time.Time
		if err := rows.Scan(&roleStr, &content, &createdAt); err != nil {
			continue
		}
		// Skip empty messages — they cause Anthropic API errors.
		if content == "" {
			continue
		}
		msgs = append(msgs, agent.Message{
			Role:      agent.Role(roleStr),
			Content:   content,
			CreatedAt: createdAt,
		})
	}

	// Reverse to get chronological order (we queried DESC for the LIMIT).
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}

	return msgs, nil
}

// ClearHistory deletes all messages for a session.
func (h *SQLiteHistory) ClearHistory(sessionID string) error {
	_, err := h.db.Exec(`DELETE FROM conversations WHERE session_id = ?`, sessionID)
	return err
}

// Close shuts down the database connection.
func (h *SQLiteHistory) Close() error {
	return h.db.Close()
}
