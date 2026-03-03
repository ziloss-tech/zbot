package security

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// DestructiveTools lists tool names that require explicit user confirmation.
var DestructiveTools = map[string]bool{
	"ghl_send_message":    true,
	"send_email":          true,
	"ghl_update_contact":  true,
	"sheets_write":        true,
	"run_code":            true,
}

// IsDestructive returns true if the tool requires confirmation.
func IsDestructive(toolName string) bool {
	return DestructiveTools[toolName]
}

// PendingAction holds a destructive tool call awaiting user confirmation.
type PendingAction struct {
	ToolCallID string
	ToolName   string
	Input      map[string]any
	Preview    string // human-readable preview of what will happen
}

// ConfirmationStore tracks pending confirmations per session.
// In-memory — resets on restart, that's fine per sprint spec.
type ConfirmationStore struct {
	mu      sync.Mutex
	pending map[string]*PendingAction // sessionID → pending action
}

// NewConfirmationStore creates a new in-memory confirmation store.
func NewConfirmationStore() *ConfirmationStore {
	return &ConfirmationStore{
		pending: make(map[string]*PendingAction),
	}
}

// SetPending stores a pending action for a session.
func (cs *ConfirmationStore) SetPending(sessionID string, action *PendingAction) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.pending[sessionID] = action
}

// GetPending retrieves and clears the pending action for a session.
func (cs *ConfirmationStore) GetPending(sessionID string) *PendingAction {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	action := cs.pending[sessionID]
	delete(cs.pending, sessionID)
	return action
}

// HasPending checks if a session has a pending confirmation.
func (cs *ConfirmationStore) HasPending(sessionID string) bool {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	_, ok := cs.pending[sessionID]
	return ok
}

// IsConfirmation returns true if the user's message is a confirmation response.
func IsConfirmation(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	confirms := []string{"yes", "confirm", "do it", "go ahead", "y", "proceed", "ok", "approved"}
	for _, c := range confirms {
		if lower == c {
			return true
		}
	}
	return false
}

// IsCancellation returns true if the user's message is a cancellation response.
func IsCancellation(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	cancels := []string{"no", "cancel", "stop", "abort", "n", "nevermind", "nope"}
	for _, c := range cancels {
		if lower == c {
			return true
		}
	}
	return false
}

// FormatPreview creates a human-readable preview of a destructive tool call.
func FormatPreview(toolName string, input map[string]any) string {
	switch toolName {
	case "ghl_send_message":
		contactID, _ := input["contactId"].(string)
		message, _ := input["message"].(string)
		return fmt.Sprintf("📤 *Send GHL message*\nTo: `%s`\nMessage: %s", contactID, truncatePreview(message, 200))
	case "send_email":
		to, _ := input["to"].(string)
		subject, _ := input["subject"].(string)
		return fmt.Sprintf("📧 *Send email*\nTo: %s\nSubject: %s", to, subject)
	case "ghl_update_contact":
		contactID, _ := input["contactId"].(string)
		return fmt.Sprintf("✏️ *Update GHL contact*\nContact: `%s`", contactID)
	case "sheets_write":
		sheetID, _ := input["spreadsheetId"].(string)
		rangeStr, _ := input["range"].(string)
		return fmt.Sprintf("📊 *Write to Google Sheet*\nSheet: `%s`\nRange: %s", sheetID, rangeStr)
	case "run_code":
		lang, _ := input["language"].(string)
		code, _ := input["code"].(string)
		return fmt.Sprintf("⚡ *Execute code* (%s)\n```\n%s\n```", lang, truncatePreview(code, 300))
	default:
		b, _ := json.Marshal(input)
		return fmt.Sprintf("⚠️ *%s*\nInput: %s", toolName, truncatePreview(string(b), 200))
	}
}

func truncatePreview(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
