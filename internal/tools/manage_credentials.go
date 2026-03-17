// Package tools — Sprint C: manage_credentials tool.
//
// Allows the agent to add, remove, and list stored site credentials
// on behalf of the user. Passwords are stored in the SecretsManager
// (Keychain or GCloud) — they NEVER appear in conversation history,
// logs, or memory.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/zbot-ai/zbot/internal/agent"
)

// ManageCredentialsTool lets the agent manage site credentials.
type ManageCredentialsTool struct {
	secrets agent.SecretsManager
	logger  *slog.Logger
}

// NewManageCredentialsTool creates the manage_credentials tool.
func NewManageCredentialsTool(secrets agent.SecretsManager, logger *slog.Logger) *ManageCredentialsTool {
	return &ManageCredentialsTool{secrets: secrets, logger: logger}
}

func (t *ManageCredentialsTool) Name() string { return "manage_credentials" }

func (t *ManageCredentialsTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "manage_credentials",
		Description: "Add, remove, or list stored site credentials. Passwords are securely stored in the system keychain — they never appear in conversation history or logs.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"action": map[string]any{
					"type":        "string",
					"enum":        []string{"add", "remove", "list"},
					"description": "The action to perform",
				},
				"domain": map[string]any{
					"type":        "string",
					"description": "Site domain (required for add/remove, e.g., 'wsj.com')",
				},
				"email": map[string]any{
					"type":        "string",
					"description": "Login email (required for add)",
				},
				"password": map[string]any{
					"type":        "string",
					"description": "Login password (required for add). Will be securely stored, never logged.",
				},
				"auth_type": map[string]any{
					"type":        "string",
					"enum":        []string{"bearer", "basic", "cookie", "header", "apikey", "login_form"},
					"description": "Authentication type (default: bearer)",
					"default":     "bearer",
				},
			},
			"required": []string{"action"},
		},
	}
}

func (t *ManageCredentialsTool) Execute(ctx context.Context, input map[string]any) (*agent.ToolResult, error) {
	action, _ := input["action"].(string)
	domain, _ := input["domain"].(string)

	switch action {
	case "add":
		return t.add(ctx, input, domain)
	case "remove":
		return t.remove(ctx, domain)
	case "list":
		return t.list(ctx)
	default:
		return &agent.ToolResult{
			Content: fmt.Sprintf("Unknown action %q — use 'add', 'remove', or 'list'.", action),
			IsError: true,
		}, nil
	}
}

func (t *ManageCredentialsTool) add(ctx context.Context, input map[string]any, domain string) (*agent.ToolResult, error) {
	if domain == "" {
		return &agent.ToolResult{Content: "error: domain is required for add", IsError: true}, nil
	}

	email, _ := input["email"].(string)
	password, _ := input["password"].(string)
	authType, _ := input["auth_type"].(string)
	if authType == "" {
		authType = "bearer"
	}

	if password == "" {
		return &agent.ToolResult{Content: "error: password/token is required for add", IsError: true}, nil
	}

	// Store the credential under a domain-namespaced key.
	credKey := "zbot-cred-" + sanitizeDomain(domain)
	if err := t.secrets.Store(ctx, credKey, []byte(password)); err != nil {
		return &agent.ToolResult{
			Content: fmt.Sprintf("Failed to store credential for %s: %v", domain, err),
			IsError: true,
		}, nil
	}

	// Store metadata (email, auth_type) as a separate JSON key — no secrets in here.
	meta := credentialMeta{Domain: domain, Email: email, AuthType: authType}
	metaJSON, _ := json.Marshal(meta)
	metaKey := "zbot-cred-meta-" + sanitizeDomain(domain)
	if err := t.secrets.Store(ctx, metaKey, metaJSON); err != nil {
		t.logger.Warn("credential metadata store failed", "domain", domain, "err", err)
		// Non-fatal — the credential itself is stored.
	}

	// IMPORTANT: Log only the domain, NEVER the password.
	t.logger.Info("credential stored", "domain", domain, "auth_type", authType)

	return &agent.ToolResult{
		Content: fmt.Sprintf("Credential for %s stored securely. Auth type: %s.", domain, authType),
	}, nil
}

func (t *ManageCredentialsTool) remove(ctx context.Context, domain string) (*agent.ToolResult, error) {
	if domain == "" {
		return &agent.ToolResult{Content: "error: domain is required for remove", IsError: true}, nil
	}

	credKey := "zbot-cred-" + sanitizeDomain(domain)
	metaKey := "zbot-cred-meta-" + sanitizeDomain(domain)

	credErr := t.secrets.Delete(ctx, credKey)
	metaErr := t.secrets.Delete(ctx, metaKey)

	if credErr != nil && metaErr != nil {
		return &agent.ToolResult{
			Content: fmt.Sprintf("No credential found for %s.", domain),
			IsError: true,
		}, nil
	}

	t.logger.Info("credential removed", "domain", domain)
	return &agent.ToolResult{
		Content: fmt.Sprintf("Credential for %s removed.", domain),
	}, nil
}

func (t *ManageCredentialsTool) list(ctx context.Context) (*agent.ToolResult, error) {
	keys, err := t.secrets.List(ctx, "zbot-cred-meta-")
	if err != nil {
		// Fallback: list credential keys directly.
		keys, err = t.secrets.List(ctx, "zbot-cred-")
		if err != nil {
			return &agent.ToolResult{
				Content: "Could not list credentials — this secrets backend may not support listing.",
				IsError: true,
			}, nil
		}
	}

	if len(keys) == 0 {
		return &agent.ToolResult{Content: "No stored credentials."}, nil
	}

	var entries []string
	for _, key := range keys {
		// Try to read metadata for richer display.
		metaJSON, getErr := t.secrets.Get(ctx, key)
		if getErr == nil && metaJSON != "" {
			var meta credentialMeta
			if json.Unmarshal([]byte(metaJSON), &meta) == nil && meta.Domain != "" {
				entry := meta.Domain
				if meta.Email != "" {
					entry += " (" + meta.Email + ")"
				}
				entry += " [" + meta.AuthType + "]"
				entries = append(entries, entry)
				continue
			}
		}
		// Fallback: just show the domain from the key name.
		domain := strings.TrimPrefix(key, "zbot-cred-meta-")
		domain = strings.TrimPrefix(domain, "zbot-cred-")
		entries = append(entries, domain)
	}

	return &agent.ToolResult{
		Content: fmt.Sprintf("Stored credentials (%d):\n%s", len(entries), strings.Join(entries, "\n")),
	}, nil
}

type credentialMeta struct {
	Domain   string `json:"domain"`
	Email    string `json:"email"`
	AuthType string `json:"auth_type"`
}

// sanitizeDomain normalizes a domain for use as a secret key.
func sanitizeDomain(domain string) string {
	domain = strings.ToLower(strings.TrimSpace(domain))
	domain = strings.ReplaceAll(domain, ".", "-")
	domain = strings.ReplaceAll(domain, "/", "")
	return domain
}

var _ agent.Tool = (*ManageCredentialsTool)(nil)
