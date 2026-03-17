// Package secrets — Sprint C: Credential scrubber.
//
// Ensures credentials never appear in logs, memory, error messages, or
// LLM conversation history. Implements slog.LogValuer for structured
// log redaction, plus string-level scrubbing for free-form text.
package secrets

import (
	"log/slog"
	"regexp"
	"strings"
)

// Redacted is the replacement text for scrubbed credentials.
const Redacted = "[REDACTED]"

// SensitiveValue wraps a credential value and implements slog.LogValuer
// so it's automatically redacted in structured logs.
type SensitiveValue struct {
	value string
}

// NewSensitiveValue wraps a string so it's redacted in logs.
func NewSensitiveValue(v string) SensitiveValue {
	return SensitiveValue{value: v}
}

// String returns the actual value. Use sparingly — only in code paths
// that actually need the credential (e.g., HTTP headers, form fields).
func (s SensitiveValue) String() string {
	return s.value
}

// LogValue implements slog.LogValuer — always returns [REDACTED].
func (s SensitiveValue) LogValue() slog.Value {
	return slog.StringValue(Redacted)
}

// Scrubber holds known credential values and scrubs them from text.
// Thread-safe once constructed (no mutations after creation).
type Scrubber struct {
	secrets  []string // raw credential values to scrub
	patterns []*regexp.Regexp
}

// NewScrubber creates a scrubber that removes the given secret values from any text.
// Also includes regex patterns for common credential formats.
func NewScrubber(secretValues ...string) *Scrubber {
	patterns := []*regexp.Regexp{
		// Bearer tokens.
		regexp.MustCompile(`Bearer\s+[A-Za-z0-9\-._~+/]+=*`),
		// Basic auth.
		regexp.MustCompile(`Basic\s+[A-Za-z0-9+/]+=*`),
		// API keys (common formats).
		regexp.MustCompile(`(?i)(api[_-]?key|token|secret|password|credential)\s*[=:]\s*["']?[A-Za-z0-9\-._~+/]{16,}["']?`),
		// GitHub PATs.
		regexp.MustCompile(`ghp_[A-Za-z0-9]{36}`),
		// Anthropic keys.
		regexp.MustCompile(`sk-ant-[A-Za-z0-9\-]{20,}`),
		// OpenAI keys.
		regexp.MustCompile(`sk-[A-Za-z0-9]{20,}`),
	}

	// Deduplicate and filter empty values.
	var cleaned []string
	seen := make(map[string]bool)
	for _, s := range secretValues {
		s = strings.TrimSpace(s)
		if s != "" && len(s) >= 4 && !seen[s] {
			cleaned = append(cleaned, s)
			seen[s] = true
		}
	}

	return &Scrubber{
		secrets:  cleaned,
		patterns: patterns,
	}
}

// Scrub removes all known secret values and credential patterns from text.
func (s *Scrubber) Scrub(text string) string {
	if text == "" {
		return text
	}

	// Replace known secret values first (exact match).
	for _, secret := range s.secrets {
		text = strings.ReplaceAll(text, secret, Redacted)
	}

	// Apply regex patterns for common credential formats.
	for _, p := range s.patterns {
		text = p.ReplaceAllString(text, Redacted)
	}

	return text
}

// ScrubMessages scrubs credentials from a slice of message-like strings.
// Returns a new slice — does not mutate the input.
func (s *Scrubber) ScrubMessages(messages []string) []string {
	result := make([]string, len(messages))
	for i, m := range messages {
		result[i] = s.Scrub(m)
	}
	return result
}
