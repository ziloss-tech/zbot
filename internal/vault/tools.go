package vault

import (
	"context"
	"fmt"
	"strings"

	"github.com/ziloss-tech/zbot/internal/agent"
)

// ─── VAULT SKILL ─────────────────────────────────────────────────────────────

// Skill wraps the vault into a ZBOT skill.
type Skill struct {
	vault  *Vault
	userID string // resolved at startup from config
}

// NewSkill creates a vault skill.
func NewSkill(v *Vault, userID string) *Skill {
	return &Skill{vault: v, userID: userID}
}

func (s *Skill) Name() string        { return "vault" }
func (s *Skill) Description() string { return "Encrypted secrets vault — store and retrieve API keys securely" }

func (s *Skill) Tools() []agent.Tool {
	return []agent.Tool{
		&VaultPutTool{vault: s.vault, userID: s.userID},
		&VaultGetTool{vault: s.vault, userID: s.userID},
		&VaultListTool{vault: s.vault, userID: s.userID},
		&VaultDeleteTool{vault: s.vault, userID: s.userID},
	}
}

func (s *Skill) SystemPromptAddendum() string {
	return `### Vault (4 tools)
You have an encrypted secrets vault. Secrets are AES-256-GCM encrypted at rest.
- vault_put: Store a secret (key + value). Value is encrypted immediately.
- vault_get: Retrieve a decrypted secret by key. Use this to get API tokens at runtime.
- vault_list: List all stored secret keys (no values shown).
- vault_delete: Permanently delete a secret.
IMPORTANT: Never log or display secret values to the user unless they explicitly ask.
Use vault_get internally when you need a token for a tool (e.g. GHL, GitHub, OpenAI).`
}

// ─── PUT ─────────────────────────────────────────────────────────────────────

type VaultPutTool struct {
	vault  *Vault
	userID string
}

func (t *VaultPutTool) Name() string { return "vault_put" }
func (t *VaultPutTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "vault_put",
		Description: "Store an encrypted secret in the vault. The value is AES-256-GCM encrypted at rest.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"key", "value"},
			"properties": map[string]any{
				"key":   map[string]any{"type": "string", "description": "Secret name (e.g. GHL_API_KEY, OPENAI_TOKEN)"},
				"value": map[string]any{"type": "string", "description": "Secret value to encrypt and store"},
				"label": map[string]any{"type": "string", "description": "Optional human-readable label"},
			},
		},
	}
}

func (t *VaultPutTool) Execute(ctx context.Context, input map[string]any) (*agent.ToolResult, error) {
	key, _ := input["key"].(string)
	value, _ := input["value"].(string)
	if key == "" || value == "" {
		return &agent.ToolResult{Content: "error: key and value are required", IsError: true}, nil
	}

	metadata := map[string]string{}
	if label, ok := input["label"].(string); ok && label != "" {
		metadata["label"] = label
	}

	if err := t.vault.Put(ctx, t.userID, key, value, metadata); err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("vault error: %v", err), IsError: true}, nil
	}

	masked := MaskValue(value)
	return &agent.ToolResult{Content: fmt.Sprintf("✅ Secret `%s` stored (value: %s)", key, masked)}, nil
}

var _ agent.Tool = (*VaultPutTool)(nil)

// ─── GET ─────────────────────────────────────────────────────────────────────

type VaultGetTool struct {
	vault  *Vault
	userID string
}

func (t *VaultGetTool) Name() string { return "vault_get" }
func (t *VaultGetTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "vault_get",
		Description: "Retrieve a decrypted secret from the vault. Use internally to get API tokens for tools.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"key"},
			"properties": map[string]any{
				"key":    map[string]any{"type": "string", "description": "Secret name to retrieve"},
				"masked": map[string]any{"type": "boolean", "description": "Return masked value instead of plaintext (default: false)"},
			},
		},
	}
}

func (t *VaultGetTool) Execute(ctx context.Context, input map[string]any) (*agent.ToolResult, error) {
	key, _ := input["key"].(string)
	if key == "" {
		return &agent.ToolResult{Content: "error: key is required", IsError: true}, nil
	}

	value, err := t.vault.Get(ctx, t.userID, key)
	if err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("vault error: %v", err), IsError: true}, nil
	}

	masked, _ := input["masked"].(bool)
	if masked {
		return &agent.ToolResult{Content: MaskValue(value)}, nil
	}

	return &agent.ToolResult{Content: value}, nil
}

var _ agent.Tool = (*VaultGetTool)(nil)

// ─── LIST ────────────────────────────────────────────────────────────────────

type VaultListTool struct {
	vault  *Vault
	userID string
}

func (t *VaultListTool) Name() string { return "vault_list" }
func (t *VaultListTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "vault_list",
		Description: "List all secret keys stored in the vault (no values shown).",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}
}

func (t *VaultListTool) Execute(ctx context.Context, input map[string]any) (*agent.ToolResult, error) {
	keys, err := t.vault.List(ctx, t.userID)
	if err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("vault error: %v", err), IsError: true}, nil
	}
	if len(keys) == 0 {
		return &agent.ToolResult{Content: "Vault is empty. Use vault_put to store secrets."}, nil
	}
	return &agent.ToolResult{Content: fmt.Sprintf("Vault secrets (%d):\n• %s", len(keys), strings.Join(keys, "\n• "))}, nil
}

var _ agent.Tool = (*VaultListTool)(nil)

// ─── DELETE ──────────────────────────────────────────────────────────────────

type VaultDeleteTool struct {
	vault  *Vault
	userID string
}

func (t *VaultDeleteTool) Name() string { return "vault_delete" }
func (t *VaultDeleteTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "vault_delete",
		Description: "Permanently delete a secret from the vault. This cannot be undone.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"key", "confirm"},
			"properties": map[string]any{
				"key":     map[string]any{"type": "string", "description": "Secret name to delete"},
				"confirm": map[string]any{"type": "boolean", "description": "Must be true to confirm deletion"},
			},
		},
	}
}

func (t *VaultDeleteTool) Execute(ctx context.Context, input map[string]any) (*agent.ToolResult, error) {
	key, _ := input["key"].(string)
	confirm, _ := input["confirm"].(bool)
	if key == "" {
		return &agent.ToolResult{Content: "error: key is required", IsError: true}, nil
	}
	if !confirm {
		return &agent.ToolResult{Content: "error: set confirm=true to delete the secret", IsError: true}, nil
	}

	if err := t.vault.Delete(ctx, t.userID, key); err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("vault error: %v", err), IsError: true}, nil
	}

	return &agent.ToolResult{Content: fmt.Sprintf("✅ Secret `%s` permanently deleted.", key)}, nil
}

var _ agent.Tool = (*VaultDeleteTool)(nil)
