// Package secrets provides the GCP Secret Manager adapter.
// ALL API keys and credentials are retrieved here — never from env vars or config files.
package secrets

import (
	"context"
	"fmt"
	"strings"

	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	"cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	"github.com/zbot-ai/zbot/internal/agent"
	"google.golang.org/api/iterator"
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

// Store creates or updates a secret in GCP Secret Manager.
// Sprint C: Full CRUD for credentialed site research.
func (s *GCPSecretManager) Store(ctx context.Context, key string, value []byte) error {
	parent := fmt.Sprintf("projects/%s", s.projectID)
	secretName := fmt.Sprintf("%s/secrets/%s", parent, key)

	// Try to add a new version to an existing secret.
	_, err := s.client.AddSecretVersion(ctx, &secretmanagerpb.AddSecretVersionRequest{
		Parent:  secretName,
		Payload: &secretmanagerpb.SecretPayload{Data: value},
	})
	if err == nil {
		return nil
	}

	// Secret doesn't exist — create it, then add the version.
	_, createErr := s.client.CreateSecret(ctx, &secretmanagerpb.CreateSecretRequest{
		Parent:   parent,
		SecretId: key,
		Secret: &secretmanagerpb.Secret{
			Replication: &secretmanagerpb.Replication{
				Replication: &secretmanagerpb.Replication_Automatic_{
					Automatic: &secretmanagerpb.Replication_Automatic{},
				},
			},
		},
	})
	if createErr != nil {
		return fmt.Errorf("secrets.Store create %q: %w", key, createErr)
	}

	_, addErr := s.client.AddSecretVersion(ctx, &secretmanagerpb.AddSecretVersionRequest{
		Parent:  secretName,
		Payload: &secretmanagerpb.SecretPayload{Data: value},
	})
	if addErr != nil {
		return fmt.Errorf("secrets.Store add version %q: %w", key, addErr)
	}
	return nil
}

// Delete removes a secret and all its versions from GCP Secret Manager.
func (s *GCPSecretManager) Delete(ctx context.Context, key string) error {
	name := fmt.Sprintf("projects/%s/secrets/%s", s.projectID, key)
	err := s.client.DeleteSecret(ctx, &secretmanagerpb.DeleteSecretRequest{Name: name})
	if err != nil {
		return fmt.Errorf("secrets.Delete %q: %w", key, err)
	}
	return nil
}

// List returns all secret names matching the given prefix.
func (s *GCPSecretManager) List(ctx context.Context, prefix string) ([]string, error) {
	parent := fmt.Sprintf("projects/%s", s.projectID)
	it := s.client.ListSecrets(ctx, &secretmanagerpb.ListSecretsRequest{
		Parent: parent,
	})

	var keys []string
	for {
		secret, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("secrets.List: %w", err)
		}
		// Extract short name from "projects/X/secrets/NAME".
		parts := strings.Split(secret.Name, "/")
		name := parts[len(parts)-1]
		if prefix == "" || strings.HasPrefix(name, prefix) {
			keys = append(keys, name)
		}
	}
	return keys, nil
}

// Close releases the underlying gRPC connection.
func (s *GCPSecretManager) Close() error {
	return s.client.Close()
}

// SecretNames centralizes all secret name constants.
// Add new secrets here — never scatter string literals through the codebase.
// SecretNames maps to actual GCP Secret Manager names in the your GCP project.
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
