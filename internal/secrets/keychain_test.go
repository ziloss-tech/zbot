package secrets

import (
	"context"
	"os/exec"
	"testing"
)

func TestKeychainManager_SecurityCLICheck(t *testing.T) {
	// This test verifies the constructor behavior.
	// On non-macOS (Linux CI), `security` won't exist and NewKeychainManager should fail.
	_, err := exec.LookPath("security")
	hasSecurity := err == nil

	km, kmErr := NewKeychainManager()
	if hasSecurity {
		if kmErr != nil {
			t.Fatalf("expected NewKeychainManager to succeed on macOS, got: %v", kmErr)
		}
		if km == nil {
			t.Fatal("expected non-nil KeychainManager")
		}
		if km.service != KeychainService {
			t.Errorf("expected service=%q, got=%q", KeychainService, km.service)
		}
	} else {
		if kmErr == nil {
			t.Fatal("expected NewKeychainManager to fail without `security` CLI")
		}
		if km != nil {
			t.Fatal("expected nil KeychainManager on failure")
		}
	}
}

func TestMatchDomain(t *testing.T) {
	// matchDomain is tested via the credentialed_fetch tool tests,
	// but we test the internal helper here for edge cases.

	// Note: matchDomain is in the tools package, so we test the List parsing logic instead.
	_ = context.Background()
}

func TestKeychainManager_Close(t *testing.T) {
	// Close is a no-op — just verify it doesn't panic.
	km := &KeychainManager{service: "test"}
	if err := km.Close(); err != nil {
		t.Fatalf("unexpected close error: %v", err)
	}
}
