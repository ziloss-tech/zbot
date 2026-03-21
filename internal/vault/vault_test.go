package vault

import (
	"context"
	"crypto/rand"
	"fmt"
	"testing"
)

// memStore is an in-memory Store for tests.
type memStore struct {
	secrets map[string]map[string]Secret // userID -> key -> secret
	audit   []AuditEntry
}

func newMemStore() *memStore {
	return &memStore{secrets: make(map[string]map[string]Secret)}
}

func (m *memStore) Put(_ context.Context, userID string, secret Secret) error {
	if m.secrets[userID] == nil {
		m.secrets[userID] = make(map[string]Secret)
	}
	m.secrets[userID][secret.Key] = secret
	return nil
}

func (m *memStore) Get(_ context.Context, userID, key string) (*Secret, error) {
	if u, ok := m.secrets[userID]; ok {
		if s, ok := u[key]; ok {
			return &s, nil
		}
	}
	return nil, fmt.Errorf("not found")
}

func (m *memStore) Delete(_ context.Context, userID, key string) error {
	if u, ok := m.secrets[userID]; ok {
		delete(u, key)
	}
	return nil
}

func (m *memStore) List(_ context.Context, userID string) ([]string, error) {
	var keys []string
	for k := range m.secrets[userID] {
		keys = append(keys, k)
	}
	return keys, nil
}

func (m *memStore) LogAudit(_ context.Context, entry AuditEntry) error {
	m.audit = append(m.audit, entry)
	return nil
}

func TestVaultRoundTrip(t *testing.T) {
	masterKey := make([]byte, 32)
	rand.Read(masterKey)

	v, err := New(masterKey, newMemStore())
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	err = v.Put(ctx, "user1", "GHL_TOKEN", "pat-abc123xyz", nil)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	val, err := v.Get(ctx, "user1", "GHL_TOKEN")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if val != "pat-abc123xyz" {
		t.Errorf("expected pat-abc123xyz, got %q", val)
	}

	_, err = v.Get(ctx, "user2", "GHL_TOKEN")
	if err == nil {
		t.Error("expected error for wrong user, got nil")
	}

	keys, err := v.List(ctx, "user1")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(keys) != 1 || keys[0] != "GHL_TOKEN" {
		t.Errorf("unexpected keys: %v", keys)
	}

	err = v.Delete(ctx, "user1", "GHL_TOKEN")
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err = v.Get(ctx, "user1", "GHL_TOKEN")
	if err == nil {
		t.Error("expected error after delete, got nil")
	}
}

func TestVaultVersioning(t *testing.T) {
	masterKey := make([]byte, 32)
	rand.Read(masterKey)
	store := newMemStore()

	v, _ := New(masterKey, store)
	ctx := context.Background()

	v.Put(ctx, "u1", "KEY", "v1", nil)
	v.Put(ctx, "u1", "KEY", "v2", nil)

	val, _ := v.Get(ctx, "u1", "KEY")
	if val != "v2" {
		t.Errorf("expected v2, got %q", val)
	}

	s := store.secrets["u1"]["KEY"]
	if s.Version != 2 {
		t.Errorf("expected version 2, got %d", s.Version)
	}
}

func TestVaultIsolation(t *testing.T) {
	masterKey := make([]byte, 32)
	rand.Read(masterKey)

	v, _ := New(masterKey, newMemStore())
	ctx := context.Background()

	v.Put(ctx, "alice", "TOKEN", "alice-secret", nil)
	v.Put(ctx, "bob", "TOKEN", "bob-secret", nil)

	aliceVal, _ := v.Get(ctx, "alice", "TOKEN")
	bobVal, _ := v.Get(ctx, "bob", "TOKEN")

	if aliceVal != "alice-secret" {
		t.Errorf("alice got %q", aliceVal)
	}
	if bobVal != "bob-secret" {
		t.Errorf("bob got %q", bobVal)
	}
}

func TestMaskValue(t *testing.T) {
	// MaskValue: len <= 8 → all stars; len > 8 → first 3 + stars + last 4
	tests := []struct {
		in   string
		want string
	}{
		{"abc", "***"},                    // len 3 <= 8 → all stars
		{"abcdefgh", "********"},          // len 8 <= 8 → all stars
		{"123456789", "123**6789"},         // len 9 > 8 → "123" + 2 stars + "6789"
		{"sk-1234567890abcdef", "sk-************cdef"}, // len 19 > 8 → "sk-" + 12 stars + "cdef"
	}
	for _, tt := range tests {
		got := MaskValue(tt.in)
		if got != tt.want {
			t.Errorf("MaskValue(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestEncryptDecryptDifferentUsers(t *testing.T) {
	masterKey := make([]byte, 32)
	rand.Read(masterKey)

	v, _ := New(masterKey, newMemStore())

	// Same plaintext encrypted by different users should produce different ciphertext
	ct1, _ := v.encrypt("alice", []byte("secret"))
	ct2, _ := v.encrypt("bob", []byte("secret"))
	if ct1 == ct2 {
		t.Error("different users produced same ciphertext — key derivation broken")
	}

	// Each user can only decrypt their own
	pt1, err := v.decrypt("alice", ct1)
	if err != nil || string(pt1) != "secret" {
		t.Errorf("alice decrypt failed: %v", err)
	}

	_, err = v.decrypt("bob", ct1)
	if err == nil {
		t.Error("bob should not be able to decrypt alice's ciphertext")
	}
}

func TestAuditLogging(t *testing.T) {
	masterKey := make([]byte, 32)
	rand.Read(masterKey)
	store := newMemStore()
	v, _ := New(masterKey, store)
	ctx := context.Background()

	v.Put(ctx, "u1", "KEY", "val", nil)
	v.Get(ctx, "u1", "KEY")
	v.List(ctx, "u1")
	v.Delete(ctx, "u1", "KEY")

	if len(store.audit) != 4 {
		t.Errorf("expected 4 audit entries, got %d", len(store.audit))
	}
	actions := []string{"write", "read", "list", "delete"}
	for i, a := range actions {
		if store.audit[i].Action != a {
			t.Errorf("audit[%d]: expected %q, got %q", i, a, store.audit[i].Action)
		}
	}
}
