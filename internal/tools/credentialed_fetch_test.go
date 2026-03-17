package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zbot-ai/zbot/internal/agent"
)

// mockSecrets implements agent.SecretsManager for testing.
type mockSecrets struct {
	store map[string]string
}

func (m *mockSecrets) Get(_ context.Context, name string) (string, error) {
	if v, ok := m.store[name]; ok {
		return v, nil
	}
	return "", fmt.Errorf("secret %q not found", name)
}
func (m *mockSecrets) Store(_ context.Context, _ string, _ []byte) error { return agent.ErrNotSupported }
func (m *mockSecrets) Delete(_ context.Context, _ string) error          { return agent.ErrNotSupported }
func (m *mockSecrets) List(_ context.Context, _ string) ([]string, error) {
	return nil, agent.ErrNotSupported
}

// newTestCredTool creates a CredentialedFetchTool with blocklist disabled for httptest.
func newTestCredTool(secrets agent.SecretsManager, creds []SiteCredential) *CredentialedFetchTool {
	t := NewCredentialedFetchTool(secrets, creds, nil, nil, nil, slog.Default())
	t.skipBlocklist = true
	return t
}

func TestMatchDomain(t *testing.T) {
	tests := []struct {
		pattern string
		domain  string
		want    bool
	}{
		{"github.com", "github.com", true},
		{"github.com", "api.github.com", false},
		{"*.github.com", "api.github.com", true},
		{"*.github.com", "github.com", true},
		{"*.github.com", "evil-github.com", false},
		{"*.notion.so", "api.notion.so", true},
		{"*.notion.so", "notion.so", true},
		{"example.com", "other.com", false},
		{"", "anything.com", false},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s_%s", tt.pattern, tt.domain), func(t *testing.T) {
			got := matchDomain(tt.pattern, tt.domain)
			if got != tt.want {
				t.Errorf("matchDomain(%q, %q) = %v, want %v", tt.pattern, tt.domain, got, tt.want)
			}
		})
	}
}

func TestCredentialedFetch_BearerAuth(t *testing.T) {
	var gotAuth string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer ts.Close()

	tool := newTestCredTool(
		&mockSecrets{store: map[string]string{"github-token": "ghp_test123"}},
		[]SiteCredential{{DomainPattern: "127.0.0.1", Type: CredBearer, SecretKey: "github-token"}},
	)

	result, err := tool.Execute(context.Background(), map[string]any{"url": ts.URL + "/api/test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", result.Content)
	}
	if gotAuth != "Bearer ghp_test123" {
		t.Errorf("expected Authorization: Bearer ghp_test123, got: %q", gotAuth)
	}
	if !strings.Contains(result.Content, "_[authenticated]_") {
		t.Error("expected authenticated tag in response")
	}
}

func TestCredentialedFetch_CookieAuth(t *testing.T) {
	var gotCookie string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie("session_id")
		if err == nil {
			gotCookie = c.Value
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, "<html><body><p>Protected content here</p></body></html>")
	}))
	defer ts.Close()

	tool := newTestCredTool(
		&mockSecrets{store: map[string]string{"notion-cookie": "abc123session"}},
		[]SiteCredential{{DomainPattern: "127.0.0.1", Type: CredCookie, SecretKey: "notion-cookie", CookieName: "session_id"}},
	)

	result, err := tool.Execute(context.Background(), map[string]any{"url": ts.URL + "/page"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", result.Content)
	}
	if gotCookie != "abc123session" {
		t.Errorf("expected cookie value abc123session, got: %q", gotCookie)
	}
}

func TestCredentialedFetch_APIKeyAuth(t *testing.T) {
	var gotKey string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.URL.Query().Get("api_key")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data": "secret stuff"}`)
	}))
	defer ts.Close()

	tool := newTestCredTool(
		&mockSecrets{store: map[string]string{"service-api-key": "sk-test-key-456"}},
		[]SiteCredential{{DomainPattern: "127.0.0.1", Type: CredAPIKey, SecretKey: "service-api-key", ParamName: "api_key"}},
	)

	result, err := tool.Execute(context.Background(), map[string]any{"url": ts.URL + "/api/data"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", result.Content)
	}
	if gotKey != "sk-test-key-456" {
		t.Errorf("expected api_key=sk-test-key-456, got: %q", gotKey)
	}
}

func TestCredentialedFetch_NoMatchingCred(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "" {
			t.Errorf("expected no auth header, got: %q", auth)
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, "<html><body><p>Public content</p></body></html>")
	}))
	defer ts.Close()

	tool := newTestCredTool(
		&mockSecrets{store: map[string]string{}},
		[]SiteCredential{{DomainPattern: "api.github.com", Type: CredBearer, SecretKey: "github-token"}},
	)

	result, err := tool.Execute(context.Background(), map[string]any{"url": ts.URL + "/public"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", result.Content)
	}
	if strings.Contains(result.Content, "_[authenticated]_") {
		t.Error("expected anonymous fetch, got authenticated tag")
	}
}

func TestCredentialedFetch_CustomHeader(t *testing.T) {
	var gotHeader string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Custom-Auth")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"ok": true}`)
	}))
	defer ts.Close()

	tool := newTestCredTool(
		&mockSecrets{store: map[string]string{"custom-token": "mytoken123"}},
		[]SiteCredential{{DomainPattern: "127.0.0.1", Type: CredHeader, SecretKey: "custom-token", HeaderName: "X-Custom-Auth"}},
	)

	result, err := tool.Execute(context.Background(), map[string]any{"url": ts.URL + "/api"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", result.Content)
	}
	if gotHeader != "mytoken123" {
		t.Errorf("expected X-Custom-Auth=mytoken123, got: %q", gotHeader)
	}
}

func TestCredentialedFetch_Definition(t *testing.T) {
	tool := NewCredentialedFetchTool(nil, nil, nil, nil, nil, slog.Default())
	if tool.Name() != "credentialed_fetch" {
		t.Errorf("expected name=credentialed_fetch, got=%s", tool.Name())
	}
	def := tool.Definition()
	if def.Name != "credentialed_fetch" {
		t.Errorf("expected definition name=credentialed_fetch, got=%s", def.Name)
	}
}

func TestCredentialedFetch_HTTPError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, "Access denied")
	}))
	defer ts.Close()

	tool := newTestCredTool(
		&mockSecrets{store: map[string]string{}},
		nil,
	)

	result, err := tool.Execute(context.Background(), map[string]any{"url": ts.URL + "/forbidden"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error result for 403")
	}
	if !strings.Contains(result.Content, "403") {
		t.Errorf("expected 403 in error content, got: %s", result.Content)
	}
}

func TestCredentialedFetch_EmptyURL(t *testing.T) {
	tool := newTestCredTool(&mockSecrets{store: map[string]string{}}, nil)
	result, err := tool.Execute(context.Background(), map[string]any{"url": ""})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for empty URL")
	}
}

func TestCredentialedFetch_JSONPrettyPrint(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"key":"value","nested":{"a":1}}`)
	}))
	defer ts.Close()

	tool := newTestCredTool(&mockSecrets{store: map[string]string{}}, nil)
	result, err := tool.Execute(context.Background(), map[string]any{"url": ts.URL + "/json"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", result.Content)
	}
	// Should be pretty-printed with indentation.
	if !strings.Contains(result.Content, "  ") {
		t.Error("expected pretty-printed JSON with indentation")
	}
}
