package secrets

import (
	"log/slog"
	"strings"
	"testing"
)

func TestSensitiveValue_LogValue(t *testing.T) {
	sv := NewSensitiveValue("super-secret-api-key-12345")

	// String() should return the real value.
	if sv.String() != "super-secret-api-key-12345" {
		t.Errorf("String() = %q, want the real value", sv.String())
	}

	// LogValue() should return [REDACTED].
	logVal := sv.LogValue()
	if logVal.String() != Redacted {
		t.Errorf("LogValue() = %q, want %q", logVal.String(), Redacted)
	}
}

func TestScrubber_ExactMatch(t *testing.T) {
	scrubber := NewScrubber("my-api-key-abc123", "db-password-xyz")

	input := "Connecting with api key my-api-key-abc123 and password db-password-xyz to the server"
	got := scrubber.Scrub(input)

	if strings.Contains(got, "my-api-key-abc123") {
		t.Error("API key was not scrubbed")
	}
	if strings.Contains(got, "db-password-xyz") {
		t.Error("password was not scrubbed")
	}
	if !strings.Contains(got, Redacted) {
		t.Error("expected [REDACTED] in output")
	}
}

func TestScrubber_Patterns(t *testing.T) {
	scrubber := NewScrubber() // no exact values, just patterns

	tests := []struct {
		name  string
		input string
	}{
		{"bearer token", "Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0"},
		{"basic auth", "Authorization: Basic dXNlcjpwYXNzd29yZA=="},
		{"github pat", "Using token ghp_ABC123DEF456GHI789JKL012MNO345PQR678"},
		{"anthropic key", "API key: sk-ant-abcdef1234567890-test"},
		{"openai key", "Using sk-abcdefghijklmnopqrstuvwxyz"},
		{"generic api_key", "api_key=ABCDEFGHIJ1234567890"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := scrubber.Scrub(tt.input)
			if !strings.Contains(got, Redacted) {
				t.Errorf("pattern not scrubbed: %q → %q", tt.input, got)
			}
		})
	}
}

func TestScrubber_PreservesNormalText(t *testing.T) {
	scrubber := NewScrubber("secret123")

	input := "The weather today is sunny and warm."
	got := scrubber.Scrub(input)

	if got != input {
		t.Errorf("normal text modified: %q → %q", input, got)
	}
}

func TestScrubber_EmptyInput(t *testing.T) {
	scrubber := NewScrubber("test")
	if got := scrubber.Scrub(""); got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestScrubber_ScrubMessages(t *testing.T) {
	scrubber := NewScrubber("secret-value")
	messages := []string{
		"Hello, using secret-value here",
		"No secrets in this one",
		"Another secret-value occurrence",
	}

	got := scrubber.ScrubMessages(messages)
	if len(got) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(got))
	}
	if strings.Contains(got[0], "secret-value") {
		t.Error("message 0 not scrubbed")
	}
	if got[1] != "No secrets in this one" {
		t.Error("message 1 should be unchanged")
	}
	if strings.Contains(got[2], "secret-value") {
		t.Error("message 2 not scrubbed")
	}
}

func TestScrubber_ShortValues(t *testing.T) {
	// Values less than 4 chars should be ignored to avoid over-scrubbing.
	scrubber := NewScrubber("ab", "the", "")
	input := "the quick brown fox"
	got := scrubber.Scrub(input)
	if got != input {
		t.Errorf("short values should be ignored: %q → %q", input, got)
	}
}

func TestSensitiveValue_WithSlog(t *testing.T) {
	// Verify the interface is implemented.
	var _ slog.LogValuer = SensitiveValue{}
}
