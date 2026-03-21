package llm

import (
	"log/slog"
	"os"
	"testing"

	"github.com/ziloss-tech/zbot/internal/agent"
)

var testLogger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

// ─── Compile-time interface checks ──────────────────────────────────────────
// Verify that the primary LLM clients satisfy agent.LLMClient.
// OpenAIClient is intentionally excluded — it has a different interface
// (Chat/ChatStream) used for research critic/planner roles, not the
// standard Complete/CompleteStream contract.

var _ agent.LLMClient = (*Client)(nil)
var _ agent.LLMClient = (*OpenAICompatClient)(nil)
var _ agent.LLMClient = (*OpenRouterClient)(nil)

// ─── Constructor + ModelName Tests ──────────────────────────────────────────

func TestAnthropicClientModelName(t *testing.T) {
	c := New("fake-key", testLogger)
	if c.ModelName() != ModelSonnet {
		t.Errorf("expected %q, got %q", ModelSonnet, c.ModelName())
	}
}

func TestHaikuClientModelName(t *testing.T) {
	c := NewHaikuClient("fake-key", testLogger)
	if c.ModelName() != ModelHaiku {
		t.Errorf("expected %q, got %q", ModelHaiku, c.ModelName())
	}
}

func TestOpenAICompatClientModelName(t *testing.T) {
	c := NewOpenAICompatClient("http://localhost:11434/v1", "ollama", "llama3.1:8b", testLogger)
	if c.ModelName() != "llama3.1:8b" {
		t.Errorf("expected 'llama3.1:8b', got %q", c.ModelName())
	}
}

func TestOpenRouterClientModelName(t *testing.T) {
	c := NewOpenRouterClient("fake-key", "mistralai/mistral-large", testLogger)
	if c.ModelName() != "mistralai/mistral-large" {
		t.Errorf("expected 'mistralai/mistral-large', got %q", c.ModelName())
	}
}

func TestOpenRouterDisplayName(t *testing.T) {
	c := NewOpenRouterClient("fake-key", "mistralai/mistral-large", testLogger)
	name := c.DisplayName()
	if name == "" {
		t.Error("DisplayName should not be empty")
	}
}

func TestDeepSeekCheapClientModelName(t *testing.T) {
	c := NewDeepSeekCheapClient("fake-key", testLogger)
	name := c.ModelName()
	if name == "" {
		t.Error("DeepSeek cheap client ModelName should not be empty")
	}
}

// ─── Model Constants ────────────────────────────────────────────────────────

func TestModelConstants(t *testing.T) {
	if ModelSonnet == "" {
		t.Error("ModelSonnet should not be empty")
	}
	if ModelOpus == "" {
		t.Error("ModelOpus should not be empty")
	}
	if ModelHaiku == "" {
		t.Error("ModelHaiku should not be empty")
	}
	if DefaultMaxTokens <= 0 {
		t.Error("DefaultMaxTokens should be positive")
	}
}

// ─── OpenAIClient (research-specific, different interface) ──────────────────

func TestOpenAIClientModelName(t *testing.T) {
	c := NewOpenAIClient("fake-key", "gpt-4o", testLogger)
	if c.ModelName() != "gpt-4o" {
		t.Errorf("expected 'gpt-4o', got %q", c.ModelName())
	}
}
