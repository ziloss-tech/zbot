// Package webui — Sprint 20: Persistent Claude Chat
// Bidirectional conversation between Jeremy (Slack/UI) and Claude.
// One thread, stored in Postgres, streamed live to UI via SSE.
package webui

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ChatMessage is a single message in the persistent conversation.
type ChatMessage struct {
	ID        string    `json:"id"`
	Role      string    `json:"role"`    // "user" | "assistant"
	Content   string    `json:"content"`
	Source    string    `json:"source"`  // "slack" | "ui"
	CreatedAt time.Time `json:"created_at"`
}

// ChatStore persists and retrieves chat messages from Postgres.
type ChatStore struct {
	db *pgxpool.Pool
}

// NewChatStore creates a ChatStore and ensures the table exists.
func NewChatStore(ctx context.Context, db *pgxpool.Pool) (*ChatStore, error) {
	_, err := db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS claude_chat_messages (
			id         TEXT PRIMARY KEY,
			role       TEXT        NOT NULL,
			content    TEXT        NOT NULL,
			source     TEXT        NOT NULL DEFAULT 'ui',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
		CREATE INDEX IF NOT EXISTS claude_chat_messages_created_at
			ON claude_chat_messages (created_at DESC);
	`)
	if err != nil {
		return nil, err
	}
	return &ChatStore{db: db}, nil
}

// Save persists a message.
func (cs *ChatStore) Save(ctx context.Context, msg ChatMessage) error {
	_, err := cs.db.Exec(ctx,
		`INSERT INTO claude_chat_messages (id, role, content, source, created_at)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (id) DO NOTHING`,
		msg.ID, msg.Role, msg.Content, msg.Source, msg.CreatedAt,
	)
	return err
}

// History returns the most recent N messages (oldest first).
func (cs *ChatStore) History(ctx context.Context, limit int) ([]ChatMessage, error) {
	rows, err := cs.db.Query(ctx,
		`SELECT id, role, content, source, created_at
		 FROM claude_chat_messages
		 ORDER BY created_at DESC
		 LIMIT $1`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []ChatMessage
	for rows.Next() {
		var m ChatMessage
		if err := rows.Scan(&m.ID, &m.Role, &m.Content, &m.Source, &m.CreatedAt); err != nil {
			continue
		}
		msgs = append(msgs, m)
	}

	// Reverse: DB returned newest-first, we want oldest-first for display.
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
	return msgs, nil
}

// newChatID generates a unique message ID.
func newChatID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return "msg_" + hex.EncodeToString(b)
}
