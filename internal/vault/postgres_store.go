package vault

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // pgx driver for database/sql
)

// PostgresStore implements Store using Postgres.
type PostgresStore struct {
	db *sql.DB
}

// NewPostgresStore creates a vault store backed by Postgres.
// connStr is a PostgreSQL connection string.
func NewPostgresStore(connStr string) (*PostgresStore, error) {
	db, err := sql.Open("pgx", connStr)
	if err != nil {
		return nil, fmt.Errorf("vault: open postgres: %w", err)
	}
	db.SetMaxOpenConns(3)
	db.SetMaxIdleConns(1)

	s := &PostgresStore{db: db}
	if err := s.migrate(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *PostgresStore) migrate(ctx context.Context) error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS vault_secrets (
			user_id    TEXT NOT NULL,
			key        TEXT NOT NULL,
			ciphertext TEXT NOT NULL,
			version    INTEGER NOT NULL DEFAULT 1,
			metadata   JSONB DEFAULT '{}',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			created_by TEXT NOT NULL,
			PRIMARY KEY (user_id, key)
		)`,
		`CREATE TABLE IF NOT EXISTS vault_audit_log (
			id         BIGSERIAL PRIMARY KEY,
			timestamp  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			user_id    TEXT NOT NULL,
			action     TEXT NOT NULL,
			key        TEXT NOT NULL,
			ip         TEXT DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_vault_audit_user ON vault_audit_log (user_id, timestamp DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_vault_secrets_user ON vault_secrets (user_id)`,
	}
	for _, q := range queries {
		if _, err := s.db.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("vault migration: %w", err)
		}
	}
	return nil
}

func (s *PostgresStore) Put(ctx context.Context, userID string, secret Secret) error {
	metaJSON, _ := json.Marshal(secret.Metadata)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO vault_secrets (user_id, key, ciphertext, version, metadata, created_at, updated_at, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (user_id, key) DO UPDATE SET
			ciphertext = EXCLUDED.ciphertext,
			version    = EXCLUDED.version,
			metadata   = EXCLUDED.metadata,
			updated_at = EXCLUDED.updated_at
	`, userID, secret.Key, secret.Ciphertext, secret.Version,
		metaJSON, secret.CreatedAt, secret.UpdatedAt, secret.CreatedBy)
	return err
}

func (s *PostgresStore) Get(ctx context.Context, userID, key string) (*Secret, error) {
	var sec Secret
	var metaJSON []byte
	err := s.db.QueryRowContext(ctx, `
		SELECT key, ciphertext, version, metadata, created_at, updated_at, created_by
		FROM vault_secrets WHERE user_id = $1 AND key = $2
	`, userID, key).Scan(&sec.Key, &sec.Ciphertext, &sec.Version, &metaJSON,
		&sec.CreatedAt, &sec.UpdatedAt, &sec.CreatedBy)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("secret %q not found", key)
	}
	if err != nil {
		return nil, err
	}
	if len(metaJSON) > 0 {
		_ = json.Unmarshal(metaJSON, &sec.Metadata)
	}
	return &sec, nil
}

func (s *PostgresStore) Delete(ctx context.Context, userID, key string) error {
	res, err := s.db.ExecContext(ctx, `
		DELETE FROM vault_secrets WHERE user_id = $1 AND key = $2
	`, userID, key)
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("secret %q not found", key)
	}
	return nil
}

func (s *PostgresStore) List(ctx context.Context, userID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT key FROM vault_secrets WHERE user_id = $1 ORDER BY key
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

func (s *PostgresStore) LogAudit(ctx context.Context, entry AuditEntry) error {
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO vault_audit_log (timestamp, user_id, action, key, ip)
		VALUES ($1, $2, $3, $4, $5)
	`, entry.Timestamp, entry.UserID, entry.Action, entry.Key, entry.IP)
	return err
}

// Close closes the database connection.
func (s *PostgresStore) Close() error {
	return s.db.Close()
}
