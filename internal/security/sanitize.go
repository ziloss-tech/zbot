// Package security provides input sanitization, tool validation, and
// destructive-operation confirmation gates for ZBOT.
package security

import (
	"log/slog"
	"strings"
)

// injectionPatterns are known prompt injection phrases.
// Defense-in-depth — Claude is generally resistant, but we add a layer.
var injectionPatterns = []string{
	"ignore previous instructions",
	"ignore all previous",
	"disregard your instructions",
	"you are now",
	"pretend you are",
	"act as if you are",
	"your new instructions",
	"system prompt:",
	"[system]",
	"<system>",
	"jailbreak",
}

// SanitizeInput strips known prompt injection patterns from user input.
func SanitizeInput(input string) string {
	lower := strings.ToLower(input)
	result := input
	for _, pattern := range injectionPatterns {
		if idx := strings.Index(lower, pattern); idx >= 0 {
			// Remove the injection pattern from both the result and the lowercase tracker.
			result = result[:idx] + result[idx+len(pattern):]
			lower = lower[:idx] + lower[idx+len(pattern):]
		}
	}
	return strings.TrimSpace(result)
}

// IsLikelyInjection returns true if the input looks like a prompt injection attempt.
// Logs a warning — caller decides whether to reject or sanitize.
func IsLikelyInjection(input string, logger *slog.Logger, sessionID, userID string) bool {
	lower := strings.ToLower(input)
	for _, pattern := range injectionPatterns {
		if strings.Contains(lower, pattern) {
			logger.Warn("possible prompt injection detected",
				"pattern", pattern,
				"session", sessionID,
				"user", userID,
				"input_prefix", truncate(input, 100),
			)
			return true
		}
	}
	return false
}

// truncate shortens a string for safe logging.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
