package factory

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PGSessionStore persists factory planning sessions to Postgres.
// Sessions survive ZBOT restarts so users don't lose in-progress plans.
type PGSessionStore struct {
	db *pgxpool.Pool
}

// NewPGSessionStore creates a Postgres-backed session store.
// Calls CreateTable to ensure the schema exists.
func NewPGSessionStore(ctx context.Context, db *pgxpool.Pool) (*PGSessionStore, error) {
	store := &PGSessionStore{db: db}
	if err := store.CreateTable(ctx); err != nil {
		return nil, fmt.Errorf("factory session store: %w", err)
	}
	return store, nil
}

// CreateTable ensures the factory_sessions table exists.
func (s *PGSessionStore) CreateTable(ctx context.Context) error {
	_, err := s.db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS factory_sessions (
			id TEXT PRIMARY KEY,
			idea TEXT NOT NULL,
			phase TEXT NOT NULL DEFAULT 'interview',
			state JSONB NOT NULL,
			decisions JSONB DEFAULT '[]'::jsonb,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW()
		)`)
	return err
}

// Save persists a plan state. Upserts — creates on first call, updates after.
func (s *PGSessionStore) Save(ctx context.Context, state *PlanStateV2) error {
	stateJSON, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}

	var decisionsJSON []byte
	if state.Decisions != nil {
		decisionsJSON, err = json.Marshal(state.Decisions.All())
		if err != nil {
			return fmt.Errorf("marshal decisions: %w", err)
		}
	} else {
		decisionsJSON = []byte("[]")
	}

	_, err = s.db.Exec(ctx, `
		INSERT INTO factory_sessions (id, idea, phase, state, decisions, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, NOW())
		ON CONFLICT (id) DO UPDATE SET
			phase = EXCLUDED.phase,
			state = EXCLUDED.state,
			decisions = EXCLUDED.decisions,
			updated_at = NOW()`,
		state.ID, state.Idea, string(state.Phase), stateJSON, decisionsJSON, state.StartedAt)
	return err
}

// Load retrieves a plan state by ID.
func (s *PGSessionStore) Load(ctx context.Context, id string) (*PlanStateV2, error) {
	var stateJSON, decisionsJSON []byte
	err := s.db.QueryRow(ctx,
		`SELECT state, decisions FROM factory_sessions WHERE id = $1`, id).
		Scan(&stateJSON, &decisionsJSON)
	if err != nil {
		return nil, fmt.Errorf("load session %s: %w", id, err)
	}

	var state PlanStateV2
	if err := json.Unmarshal(stateJSON, &state); err != nil {
		return nil, fmt.Errorf("unmarshal state: %w", err)
	}

	// Restore the decision log from the separate JSON column.
	state.Decisions = NewDecisionLog()
	if len(decisionsJSON) > 0 {
		var decisions []Decision
		if err := json.Unmarshal(decisionsJSON, &decisions); err == nil {
			for _, d := range decisions {
				state.Decisions.Record(d)
			}
		}
	}

	return &state, nil
}

// LoadIncomplete returns all sessions that haven't reached the "complete" phase.
// Used on startup to restore in-progress plans.
func (s *PGSessionStore) LoadIncomplete(ctx context.Context) ([]*PlanStateV2, error) {
	rows, err := s.db.Query(ctx,
		`SELECT state, decisions FROM factory_sessions WHERE phase != 'complete' ORDER BY updated_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("load incomplete sessions: %w", err)
	}
	defer rows.Close()

	var sessions []*PlanStateV2
	for rows.Next() {
		var stateJSON, decisionsJSON []byte
		if err := rows.Scan(&stateJSON, &decisionsJSON); err != nil {
			continue
		}
		var state PlanStateV2
		if err := json.Unmarshal(stateJSON, &state); err != nil {
			continue
		}
		state.Decisions = NewDecisionLog()
		if len(decisionsJSON) > 0 {
			var decisions []Decision
			if err := json.Unmarshal(decisionsJSON, &decisions); err == nil {
				for _, d := range decisions {
					state.Decisions.Record(d)
				}
			}
		}
		sessions = append(sessions, &state)
	}
	return sessions, nil
}

// Delete removes a session by ID. Used for cleanup of completed plans.
func (s *PGSessionStore) Delete(ctx context.Context, id string) error {
	_, err := s.db.Exec(ctx, `DELETE FROM factory_sessions WHERE id = $1`, id)
	return err
}

// ─── In-Memory Store (for testing / no-Postgres fallback) ───────────────────

// MemSessionStore is an in-memory session store for testing.
type MemSessionStore struct {
	sessions map[string]*storeEntry
}

type storeEntry struct {
	state     *PlanStateV2
	decisions []Decision
	savedAt   time.Time
}

// NewMemSessionStore creates an in-memory session store.
func NewMemSessionStore() *MemSessionStore {
	return &MemSessionStore{sessions: make(map[string]*storeEntry)}
}

func (m *MemSessionStore) Save(_ context.Context, state *PlanStateV2) error {
	var decisions []Decision
	if state.Decisions != nil {
		decisions = state.Decisions.All()
	}
	m.sessions[state.ID] = &storeEntry{
		state:     state,
		decisions: decisions,
		savedAt:   time.Now(),
	}
	return nil
}

func (m *MemSessionStore) Load(_ context.Context, id string) (*PlanStateV2, error) {
	entry, ok := m.sessions[id]
	if !ok {
		return nil, fmt.Errorf("session %s not found", id)
	}
	// Restore decisions.
	entry.state.Decisions = NewDecisionLog()
	for _, d := range entry.decisions {
		entry.state.Decisions.Record(d)
	}
	return entry.state, nil
}

func (m *MemSessionStore) LoadIncomplete(_ context.Context) ([]*PlanStateV2, error) {
	var result []*PlanStateV2
	for _, entry := range m.sessions {
		if entry.state.Phase != PhaseComplete {
			result = append(result, entry.state)
		}
	}
	return result, nil
}

func (m *MemSessionStore) Delete(_ context.Context, id string) error {
	delete(m.sessions, id)
	return nil
}
