// Package vault implements an encrypted secrets vault for ZBOT.
//
// Architecture:
//   - Secrets are AES-256-GCM encrypted at rest in Postgres
//   - Each user gets their own encryption key derived from a master key
//   - Master key lives in GCP Secret Manager (or env var for self-hosted)
//   - ZBOT server never logs or exposes plaintext secrets
//   - Audit log tracks every access
//
// The flow for a paid user:
//   1. User stores a secret via API/UI: PUT /api/vault/secrets/MY_GHL_TOKEN
//   2. Server encrypts with user's derived key → stores ciphertext in Postgres
//   3. ZBOT agent calls vault.Get("MY_GHL_TOKEN") at runtime
//   4. Server decrypts in memory, passes to tool, never logs value
//   5. Audit log records: who, what key, when, from where
//
// This is the "Doppler/Infisical but ours" approach Jeremy wanted.
package vault

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/hkdf"
)

// Secret is an encrypted secret stored in the vault.
type Secret struct {
	Key        string    `json:"key"`
	Ciphertext string    `json:"ciphertext"` // base64(nonce + ciphertext)
	Version    int       `json:"version"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	CreatedBy  string    `json:"created_by"`
	// Metadata is unencrypted — use for labels, not sensitive data.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// AuditEntry logs every vault access.
type AuditEntry struct {
	Timestamp time.Time `json:"timestamp"`
	UserID    string    `json:"user_id"`
	Action    string    `json:"action"` // "read", "write", "delete", "list"
	Key       string    `json:"key"`
	IP        string    `json:"ip,omitempty"`
}

// Store is the vault backend interface.
type Store interface {
	Put(ctx context.Context, userID string, secret Secret) error
	Get(ctx context.Context, userID, key string) (*Secret, error)
	Delete(ctx context.Context, userID, key string) error
	List(ctx context.Context, userID string) ([]string, error)
	LogAudit(ctx context.Context, entry AuditEntry) error
}

// Vault is the main secrets vault.
type Vault struct {
	masterKey []byte // 32-byte master key from GCP Secret Manager or env
	store     Store
	mu        sync.RWMutex
}

// New creates a vault with the given master key and storage backend.
// masterKey must be exactly 32 bytes (256 bits).
func New(masterKey []byte, store Store) (*Vault, error) {
	if len(masterKey) != 32 {
		return nil, fmt.Errorf("vault: master key must be 32 bytes, got %d", len(masterKey))
	}
	return &Vault{masterKey: masterKey, store: store}, nil
}

// deriveKey creates a per-user encryption key using HKDF.
// This means each user's secrets are encrypted with a different key,
// derived from the master key + their user ID.
func (v *Vault) deriveKey(userID string) ([]byte, error) {
	hkdfReader := hkdf.New(sha256.New, v.masterKey, []byte("zbot-vault-v1"), []byte(userID))
	key := make([]byte, 32)
	if _, err := io.ReadFull(hkdfReader, key); err != nil {
		return nil, fmt.Errorf("vault: key derivation failed: %w", err)
	}
	return key, nil
}

// encrypt encrypts plaintext using AES-256-GCM with the user's derived key.
func (v *Vault) encrypt(userID string, plaintext []byte) (string, error) {
	key, err := v.deriveKey(userID)
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("vault: cipher init: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("vault: gcm init: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("vault: nonce generation: %w", err)
	}

	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// decrypt decrypts ciphertext using AES-256-GCM with the user's derived key.
func (v *Vault) decrypt(userID, encoded string) ([]byte, error) {
	key, err := v.deriveKey(userID)
	if err != nil {
		return nil, err
	}

	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("vault: base64 decode: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("vault: cipher init: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("vault: gcm init: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return nil, fmt.Errorf("vault: ciphertext too short")
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("vault: decryption failed (wrong key or tampered data)")
	}

	return plaintext, nil
}

// Put stores a secret in the vault.
func (v *Vault) Put(ctx context.Context, userID, key, value string, metadata map[string]string) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	ciphertext, err := v.encrypt(userID, []byte(value))
	if err != nil {
		return err
	}

	// Check if exists for versioning
	existing, _ := v.store.Get(ctx, userID, key)
	version := 1
	if existing != nil {
		version = existing.Version + 1
	}

	secret := Secret{
		Key:        key,
		Ciphertext: ciphertext,
		Version:    version,
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
		CreatedBy:  userID,
		Metadata:   metadata,
	}

	if err := v.store.Put(ctx, userID, secret); err != nil {
		return fmt.Errorf("vault: store put: %w", err)
	}

	_ = v.store.LogAudit(ctx, AuditEntry{
		Timestamp: time.Now().UTC(),
		UserID:    userID,
		Action:    "write",
		Key:       key,
	})

	return nil
}

// Get retrieves and decrypts a secret.
func (v *Vault) Get(ctx context.Context, userID, key string) (string, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()

	secret, err := v.store.Get(ctx, userID, key)
	if err != nil {
		return "", fmt.Errorf("vault: not found: %w", err)
	}

	plaintext, err := v.decrypt(userID, secret.Ciphertext)
	if err != nil {
		return "", err
	}

	_ = v.store.LogAudit(ctx, AuditEntry{
		Timestamp: time.Now().UTC(),
		UserID:    userID,
		Action:    "read",
		Key:       key,
	})

	return string(plaintext), nil
}

// Delete removes a secret from the vault.
func (v *Vault) Delete(ctx context.Context, userID, key string) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if err := v.store.Delete(ctx, userID, key); err != nil {
		return fmt.Errorf("vault: delete: %w", err)
	}

	_ = v.store.LogAudit(ctx, AuditEntry{
		Timestamp: time.Now().UTC(),
		UserID:    userID,
		Action:    "delete",
		Key:       key,
	})

	return nil
}

// List returns all secret keys for a user (no values).
func (v *Vault) List(ctx context.Context, userID string) ([]string, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()

	keys, err := v.store.List(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("vault: list: %w", err)
	}

	_ = v.store.LogAudit(ctx, AuditEntry{
		Timestamp: time.Now().UTC(),
		UserID:    userID,
		Action:    "list",
		Key:       "*",
	})

	return keys, nil
}

// Export returns all secrets as a JSON blob for backup (still encrypted).
func (v *Vault) Export(ctx context.Context, userID string) ([]byte, error) {
	keys, err := v.List(ctx, userID)
	if err != nil {
		return nil, err
	}

	secrets := make([]Secret, 0, len(keys))
	for _, key := range keys {
		s, err := v.store.Get(ctx, userID, key)
		if err != nil {
			continue
		}
		secrets = append(secrets, *s)
	}

	return json.MarshalIndent(secrets, "", "  ")
}

// MaskValue returns a masked version of a secret for display (e.g. "sk-****abcd").
func MaskValue(value string) string {
	if len(value) <= 8 {
		return strings.Repeat("*", len(value))
	}
	prefix := value[:3]
	suffix := value[len(value)-4:]
	return prefix + strings.Repeat("*", len(value)-7) + suffix
}
