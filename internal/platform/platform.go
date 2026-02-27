// Package platform provides shared utilities, types, and helpers.
// No business logic lives here — only cross-cutting concerns.
package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/jeremylerwick-max/zbot/internal/agent"
)

// ─── LOGGING ─────────────────────────────────────────────────────────────────

// NewLogger creates a structured slog logger.
// JSON in production, human-readable text in dev.
// If ZBOT_LOG_DIR is set, also writes JSON logs to a file for Promtail/Loki ingestion.
func NewLogger(env string) *slog.Logger {
	opts := &slog.HandlerOptions{Level: slog.LevelDebug}
	var h slog.Handler
	if env == "production" {
		h = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		h = slog.NewTextHandler(os.Stdout, opts)
	}

	// If ZBOT_LOG_DIR is set, tee JSON logs to a file for Promtail to ship to Loki.
	if logDir := os.Getenv("ZBOT_LOG_DIR"); logDir != "" {
		logPath := logDir + "/zbot.log"
		f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err == nil {
			fileHandler := slog.NewJSONHandler(f, opts)
			h = &multiHandler{primary: h, secondary: fileHandler}
		}
	}

	return slog.New(h)
}

// multiHandler fans out log records to two handlers.
type multiHandler struct {
	primary   slog.Handler
	secondary slog.Handler
}

func (m *multiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return m.primary.Enabled(ctx, level) || m.secondary.Enabled(ctx, level)
}

func (m *multiHandler) Handle(ctx context.Context, r slog.Record) error {
	_ = m.primary.Handle(ctx, r)
	_ = m.secondary.Handle(ctx, r)
	return nil
}

func (m *multiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &multiHandler{
		primary:   m.primary.WithAttrs(attrs),
		secondary: m.secondary.WithAttrs(attrs),
	}
}

func (m *multiHandler) WithGroup(name string) slog.Handler {
	return &multiHandler{
		primary:   m.primary.WithGroup(name),
		secondary: m.secondary.WithGroup(name),
	}
}

// ─── CONFIG ──────────────────────────────────────────────────────────────────

// AppConfig holds non-secret configuration.
// Secrets (API keys) come from GCP Secret Manager at runtime — never here.
type AppConfig struct {
	Env               string   `json:"env"`
	GCPProject        string   `json:"gcp_project"`
	DBHost            string   `json:"db_host"`
	DBName            string   `json:"db_name"`
	DBUser            string   `json:"db_user"`
	TelegramAllowFrom []string `json:"telegram_allow_from"`
	WorkerCount       int      `json:"worker_count"`
	GatewayPort       int      `json:"gateway_port"`
	WorkspaceRoot     string   `json:"workspace_root"`
}

func DefaultAppConfig() AppConfig {
	return AppConfig{
		Env:           "development",
		GCPProject:    "ziloss",
		WorkerCount:   3,
		GatewayPort:   18790,
		WorkspaceRoot: os.ExpandEnv("$HOME/zbot-workspace"),
	}
}

// ─── CONTEXT KEYS ────────────────────────────────────────────────────────────

type contextKey string

const (
	ContextKeySessionID  contextKey = "session_id"
	ContextKeyWorkflowID contextKey = "workflow_id"
)

func WithSessionID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ContextKeySessionID, id)
}

func SessionIDFrom(ctx context.Context) string {
	v, _ := ctx.Value(ContextKeySessionID).(string)
	return v
}

// ─── TASK GRAPH PARSING ──────────────────────────────────────────────────────

type taskGraphJSON struct {
	Tasks []struct {
		Name           string   `json:"name"`
		Instruction    string   `json:"instruction"`
		DependsOnNames []string `json:"depends_on_names"`
	} `json:"tasks"`
}

var jsonFenceRe = regexp.MustCompile("(?s)```(?:json)?\\s*(.*?)\\s*```")

// ParseTaskGraph parses the LLM's JSON task decomposition.
// Strips markdown fences, unmarshals, resolves dependency names to IDs.
func ParseTaskGraph(raw string) ([]agent.Task, error) {
	clean := strings.TrimSpace(raw)
	if m := jsonFenceRe.FindStringSubmatch(clean); len(m) == 2 {
		clean = strings.TrimSpace(m[1])
	}

	var graph taskGraphJSON
	if err := json.Unmarshal([]byte(clean), &graph); err != nil {
		return nil, fmt.Errorf("ParseTaskGraph: %w\nraw: %s", err, raw)
	}

	nameToID := make(map[string]string, len(graph.Tasks))
	tasks := make([]agent.Task, len(graph.Tasks))
	now := time.Now()

	for i, t := range graph.Tasks {
		id := fmt.Sprintf("task-%d", i+1)
		nameToID[t.Name] = id
		tasks[i] = agent.Task{
			ID: id, Step: i + 1, Name: t.Name,
			Instruction: t.Instruction,
			Status:      agent.TaskPending,
			CreatedAt:   now, UpdatedAt: now,
		}
	}

	for i, t := range graph.Tasks {
		deps := make([]string, 0, len(t.DependsOnNames))
		for _, name := range t.DependsOnNames {
			if id, ok := nameToID[name]; ok {
				deps = append(deps, id)
			}
		}
		tasks[i].DependsOn = deps
	}

	return tasks, nil
}

// ─── TOKEN BUCKET RATE LIMITER ───────────────────────────────────────────────

// TokenBucket is a per-domain rate limiter for the scraper.
// Prevents hammering any single domain.
type TokenBucket struct {
	mu         sync.Mutex
	tokens     float64
	maxTokens  float64
	refillRate float64 // tokens per second
	lastRefill time.Time
}

func NewTokenBucket(maxTokens, refillRate float64) *TokenBucket {
	return &TokenBucket{
		tokens: maxTokens, maxTokens: maxTokens,
		refillRate: refillRate, lastRefill: time.Now(),
	}
}

// Allow returns true if a request token is available, consuming it.
func (b *TokenBucket) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(b.lastRefill).Seconds()
	b.tokens = min(b.maxTokens, b.tokens+elapsed*b.refillRate)
	b.lastRefill = now

	if b.tokens >= 1 {
		b.tokens--
		return true
	}
	return false
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
