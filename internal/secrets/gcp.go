// Package secrets provides the GCP Secret Manager adapter.
// ALL API keys and credentials are retrieved here — never from env vars or config files.
package secrets

import (
	"context"
	"fmt"

	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	"cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	"github.com/jeremylerwick-max/zbot/internal/agent"
)

// GCPSecretManager implements agent.SecretsManager using GCP Secret Manager.
type GCPSecretManager struct {
	client    *secretmanager.Client
	projectID string
}

// NewGCPSecretManager constructs the adapter. Uses Application Default Credentials.
func NewGCPSecretManager(ctx context.Context, projectID string) (*GCPSecretManager, error) {
	client, err := secretmanager.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("secrets.NewGCPSecretManager: %w", err)
	}
	return &GCPSecretManager{client: client, projectID: projectID}, nil
}

// Get retrieves the latest version of a secret by name.
// name is just the secret name (e.g. "anthropic-api-key") — not the full resource path.
func (s *GCPSecretManager) Get(ctx context.Context, name string) (string, error) {
	resource := fmt.Sprintf("projects/%s/secrets/%s/versions/latest", s.projectID, name)

	req := &secretmanagerpb.AccessSecretVersionRequest{Name: resource}
	resp, err := s.client.AccessSecretVersion(ctx, req)
	if err != nil {
		return "", fmt.Errorf("secrets.Get name=%s: %w", name, err)
	}

	return string(resp.Payload.Data), nil
}

// Close releases the underlying gRPC connection.
func (s *GCPSecretManager) Close() error {
	return s.client.Close()
}

// SecretNames centralizes all secret name constants.
// Add new secrets here — never scatter string literals through the codebase.
// SecretNames maps to actual GCP Secret Manager names in the ziloss project.
// Using existing secrets where possible — only zbot-telegram-token is new.
const (
	SecretAnthropicAPIKey = "ANTHROPIC_API_KEY"    // existing
	SecretOpenAIAPIKey    = "openai-api-key"        // existing
	SecretBraveAPIKey     = "brave-search-api-key"  // existing
	SecretDatabaseURL     = "database-url"          // existing (full postgres URL)
	SecretTelegramToken   = "zbot-telegram-token"   // new — add after BotFather setup
	SecretProxyURL        = "zbot-proxy-url"        // new — optional, Sprint 4 only
)

// Ensure GCPSecretManager implements the port.
var _ agent.SecretsManager = (*GCPSecretManager)(nil)
