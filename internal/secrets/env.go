// Package secrets provides secret management adapters.
// EnvSecretManager provides env var fallback when GCP is unavailable.
package secrets

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/jeremylerwick-max/zbot/internal/agent"
)

// envMapping maps GCP secret names to environment variable names.
// This allows the same secret names used with GCP to resolve from env vars.
var envMapping = map[string]string{
	SecretAnthropicAPIKey: "ZBOT_ANTHROPIC_API_KEY",
	SecretOpenAIAPIKey:    "ZBOT_OPENAI_API_KEY",
	SecretBraveAPIKey:     "ZBOT_BRAVE_API_KEY",
	SecretDatabaseURL:     "ZBOT_DATABASE_URL",
	SecretTelegramToken:   "ZBOT_TELEGRAM_TOKEN",
	SecretProxyURL:        "ZBOT_PROXY_URL",
	// Slack tokens.
	"zbot-slack-token":     "ZBOT_SLACK_TOKEN",
	"zbot-slack-app-token": "ZBOT_SLACK_APP_TOKEN",
	// DB password.
	"zbot-db-password": "ZBOT_DB_PASSWORD",
	// OpenRouter.
	"OPENROUTER_API_KEY": "ZBOT_OPENROUTER_API_KEY",
	// Allowed user.
	"zbot-allowed-user-id": "ZBOT_ALLOWED_USER_ID",
	// Webhook.
	"zbot-webhook-secret": "ZBOT_WEBHOOK_SECRET",
	// Together.
	"together-api-key": "ZBOT_TOGETHER_API_KEY",
}

// EnvSecretManager implements agent.SecretsManager using environment variables.
type EnvSecretManager struct{}

// NewEnvSecretManager creates an env var based secret manager.
func NewEnvSecretManager() *EnvSecretManager {
	return &EnvSecretManager{}
}

// Get retrieves a secret by looking up the mapped env var.
// Returns empty string with no error if env var is not set (graceful degradation).
func (e *EnvSecretManager) Get(_ context.Context, name string) (string, error) {
	// Check explicit mapping first.
	if envName, ok := envMapping[name]; ok {
		val := os.Getenv(envName)
		if val != "" {
			return val, nil
		}
	}

	// Try ZBOT_ prefixed version of the name (normalized).
	normalized := "ZBOT_" + strings.ToUpper(strings.ReplaceAll(strings.ReplaceAll(name, "-", "_"), " ", "_"))
	val := os.Getenv(normalized)
	if val != "" {
		return val, nil
	}

	// Try the raw name as an env var.
	val = os.Getenv(name)
	if val != "" {
		return val, nil
	}

	return "", fmt.Errorf("secret %q not found in environment", name)
}

// Close is a no-op for env var secrets.
func (e *EnvSecretManager) Close() error {
	return nil
}

// Ensure EnvSecretManager implements the port.
var _ agent.SecretsManager = (*EnvSecretManager)(nil)
