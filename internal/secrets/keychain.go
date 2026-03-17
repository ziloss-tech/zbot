// Package secrets provides the macOS Keychain adapter for SecretsManager.
// Sprint C: Credentialed site research — local credential storage via `security` CLI.
package secrets

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/jeremylerwick-max/zbot/internal/agent"
)

// KeychainService is the macOS Keychain service name used for all zbot credentials.
const KeychainService = "com.zbot.credentials"

// KeychainManager implements agent.SecretsManager using macOS Keychain
// via the `security` CLI tool. This is the preferred adapter for local dev —
// credentials never leave the machine and are protected by the login keychain.
type KeychainManager struct {
	service string // Keychain service name (default: KeychainService)
}

// NewKeychainManager creates a Keychain-backed secret manager.
// Returns ErrNotSupported if not running on macOS or `security` CLI is missing.
func NewKeychainManager() (*KeychainManager, error) {
	// Verify `security` CLI is available (macOS only).
	if _, err := exec.LookPath("security"); err != nil {
		return nil, fmt.Errorf("keychain: `security` CLI not found (macOS only): %w", err)
	}
	return &KeychainManager{service: KeychainService}, nil
}

// Get retrieves a secret from the macOS login keychain.
// Maps to: security find-generic-password -s <service> -a <name> -w
func (k *KeychainManager) Get(ctx context.Context, name string) (string, error) {
	cmd := exec.CommandContext(ctx, "security", "find-generic-password",
		"-s", k.service,
		"-a", name,
		"-w", // output password only
	)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("keychain.Get %q: %w", name, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// Store saves a credential to the macOS login keychain.
// Uses add-generic-password with -U (update if exists).
func (k *KeychainManager) Store(ctx context.Context, key string, value []byte) error {
	cmd := exec.CommandContext(ctx, "security", "add-generic-password",
		"-s", k.service,
		"-a", key,
		"-w", string(value),
		"-U", // update existing
	)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("keychain.Store %q: %w", key, err)
	}
	return nil
}

// Delete removes a credential from the macOS login keychain.
func (k *KeychainManager) Delete(ctx context.Context, key string) error {
	cmd := exec.CommandContext(ctx, "security", "delete-generic-password",
		"-s", k.service,
		"-a", key,
	)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("keychain.Delete %q: %w", key, err)
	}
	return nil
}

// List returns all account names stored under the zbot Keychain service.
// Parses the output of: security dump-keychain | grep "acct"
func (k *KeychainManager) List(ctx context.Context, prefix string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "security", "dump-keychain")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("keychain.List: %w", err)
	}

	var keys []string
	inService := false
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Track when we're inside a keychain item for our service.
		if strings.Contains(trimmed, fmt.Sprintf(`"svce"<blob>="%s"`, k.service)) {
			inService = true
			continue
		}

		// Extract account name if we're in our service's block.
		if inService && strings.Contains(trimmed, `"acct"<blob>="`) {
			// Parse: "acct"<blob>="the-key-name"
			start := strings.Index(trimmed, `="`) + 2
			end := strings.LastIndex(trimmed, `"`)
			if start > 1 && end > start {
				acct := trimmed[start:end]
				if prefix == "" || strings.HasPrefix(acct, prefix) {
					keys = append(keys, acct)
				}
			}
			inService = false // reset for next item
			continue
		}

		// Reset service tracking on new keychain item boundary.
		if strings.HasPrefix(trimmed, "keychain:") || strings.HasPrefix(trimmed, "class:") {
			inService = false
		}
	}

	return keys, nil
}

// Close is a no-op for Keychain.
func (k *KeychainManager) Close() error { return nil }

// Ensure KeychainManager implements the port.
var _ agent.SecretsManager = (*KeychainManager)(nil)
