// Package tools implements all agent tools.
// credentialed_fetch.go — Sprint C: Credential-aware URL fetcher.
//
// Wraps the existing FetchURLTool but can inject authentication headers
// or cookies from stored credentials before fetching. This allows the
// research pipeline to access gated content (paywalled APIs, private docs,
// authenticated dashboards) without exposing raw credentials to the LLM.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ziloss-tech/zbot/internal/agent"
	"github.com/ziloss-tech/zbot/internal/scraper"
)

// CredentialType describes how a stored credential should be applied to requests.
type CredentialType string

const (
	CredBearer CredentialType = "bearer" // Authorization: Bearer <token>
	CredBasic  CredentialType = "basic"  // Authorization: Basic <base64>
	CredCookie CredentialType = "cookie" // Cookie: <name>=<value>
	CredHeader CredentialType = "header" // Custom header: <key>: <value>
	CredAPIKey CredentialType = "apikey" // Query param: ?<key>=<value>
)

// SiteCredential maps a domain pattern to a stored credential.
type SiteCredential struct {
	DomainPattern string         `json:"domain_pattern"` // e.g. "*.notion.so", "api.github.com"
	Type          CredentialType `json:"type"`
	SecretKey     string         `json:"secret_key"`  // key in SecretsManager
	HeaderName    string         `json:"header_name"` // for CredHeader type
	ParamName     string         `json:"param_name"`  // for CredAPIKey type
	CookieName    string         `json:"cookie_name"` // for CredCookie type
}

// CredentialedFetchTool wraps URL fetching with automatic credential injection.
type CredentialedFetchTool struct {
	secrets       agent.SecretsManager
	credentials   []SiteCredential
	proxyPool     *scraper.ProxyPool
	rateLimiter   *scraper.DomainRateLimiter
	cache         *scraper.ScrapeCache
	logger        *slog.Logger
	skipBlocklist bool // testing only — bypasses SSRF blocklist
}

// NewCredentialedFetchTool creates a fetch tool that injects auth for known domains.
func NewCredentialedFetchTool(
	secrets agent.SecretsManager,
	credentials []SiteCredential,
	proxyPool *scraper.ProxyPool,
	rateLimiter *scraper.DomainRateLimiter,
	cache *scraper.ScrapeCache,
	logger *slog.Logger,
) *CredentialedFetchTool {
	return &CredentialedFetchTool{
		secrets:     secrets,
		credentials: credentials,
		proxyPool:   proxyPool,
		rateLimiter: rateLimiter,
		cache:       cache,
		logger:      logger,
	}
}

func (t *CredentialedFetchTool) Name() string { return "credentialed_fetch" }

func (t *CredentialedFetchTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "credentialed_fetch",
		Description: "Fetch a URL with automatic credential injection for authenticated sites. Use when accessing paywalled APIs, private docs, or gated content. Falls back to anonymous fetch if no credentials match.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"url": map[string]any{
					"type":        "string",
					"description": "The URL to fetch (credentials auto-matched by domain)",
				},
				"method": map[string]any{
					"type":        "string",
					"description": "HTTP method (default GET)",
					"enum":        []string{"GET", "POST", "PUT", "DELETE"},
					"default":     "GET",
				},
				"body": map[string]any{
					"type":        "string",
					"description": "Request body (for POST/PUT)",
				},
			},
			"required": []string{"url"},
		},
	}
}

func (t *CredentialedFetchTool) Execute(ctx context.Context, input map[string]any) (*agent.ToolResult, error) {
	rawURL, _ := input["url"].(string)
	if rawURL == "" {
		return &agent.ToolResult{Content: "error: url is required", IsError: true}, nil
	}

	method, _ := input["method"].(string)
	if method == "" {
		method = "GET"
	}
	body, _ := input["body"].(string)

	// Blocklist check (skippable for testing with httptest on 127.0.0.1).
	if !t.skipBlocklist && scraper.IsBlocked(rawURL) {
		return &agent.ToolResult{
			Content: fmt.Sprintf("error: URL %q is blocked", rawURL),
			IsError: true,
		}, nil
	}

	// Cache check.
	if t.cache != nil && method == "GET" {
		if content, found := t.cache.Get(rawURL); found {
			return &agent.ToolResult{Content: content + "\n\n_[cached]_"}, nil
		}
	}

	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("error: invalid URL: %v", err), IsError: true}, nil
	}
	domain := parsedURL.Hostname()

	// Rate limit.
	if t.rateLimiter != nil {
		if err := t.rateLimiter.Wait(ctx, domain); err != nil {
			return &agent.ToolResult{Content: fmt.Sprintf("error: rate limit: %v", err), IsError: true}, nil
		}
	}

	// Find matching credential.
	cred, credValue := t.resolveCredential(ctx, domain)
	if cred != nil {
		t.logger.Info("credentialed_fetch: matched credential",
			"domain", domain, "type", cred.Type, "secret_key", cred.SecretKey)
	}

	// Build request.
	var bodyReader io.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("credentialed_fetch: build request: %w", err)
	}
	req.Header.Set("User-Agent", scraper.RandomUserAgent())
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,application/json,*/*;q=0.8")

	// Apply credential.
	if cred != nil && credValue != "" {
		switch cred.Type {
		case CredBearer:
			req.Header.Set("Authorization", "Bearer "+credValue)
		case CredBasic:
			req.Header.Set("Authorization", "Basic "+credValue)
		case CredCookie:
			name := cred.CookieName
			if name == "" {
				name = "session"
			}
			req.AddCookie(&http.Cookie{Name: name, Value: credValue})
		case CredHeader:
			if cred.HeaderName != "" {
				req.Header.Set(cred.HeaderName, credValue)
			}
		case CredAPIKey:
			if cred.ParamName != "" {
				q := req.URL.Query()
				q.Set(cred.ParamName, credValue)
				req.URL.RawQuery = q.Encode()
			}
		}
	}

	// Execute request.
	var client *http.Client
	if t.proxyPool != nil {
		client = t.proxyPool.NewHTTPClient(20 * time.Second)
	} else {
		client = &http.Client{Timeout: 20 * time.Second}
	}

	resp, err := client.Do(req)
	if err != nil {
		return &agent.ToolResult{
			Content: fmt.Sprintf("fetch error for %s: %v", rawURL, err),
			IsError: true,
		}, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return &agent.ToolResult{
			Content: fmt.Sprintf("HTTP %d for %s: %s", resp.StatusCode, rawURL, string(respBody)),
			IsError: true,
		}, nil
	}

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 512<<10))
	if err != nil {
		return nil, fmt.Errorf("credentialed_fetch: read body: %w", err)
	}

	// Determine content type — extract text from HTML, pass JSON through.
	contentType := resp.Header.Get("Content-Type")
	var content string

	switch {
	case strings.Contains(contentType, "application/json"):
		// Pretty-print JSON for readability.
		var js json.RawMessage
		if json.Unmarshal(respBody, &js) == nil {
			pretty, prettyErr := json.MarshalIndent(js, "", "  ")
			if prettyErr == nil {
				content = string(pretty)
			} else {
				content = string(respBody)
			}
		} else {
			content = string(respBody)
		}
	case strings.Contains(contentType, "text/html"):
		content = scraper.ExtractText(string(respBody))
	default:
		content = string(respBody)
	}

	// Truncate.
	if len(content) > 100*1024 {
		content = content[:100*1024] + "\n[TRUNCATED — exceeds 100KB]"
	}

	if content == "" {
		return &agent.ToolResult{Content: "No readable content extracted."}, nil
	}

	// Cache.
	if t.cache != nil && method == "GET" {
		_ = t.cache.Set(rawURL, content)
	}

	authTag := ""
	if cred != nil {
		authTag = " _[authenticated]_"
	}
	return &agent.ToolResult{Content: content + authTag}, nil
}

// resolveCredential finds the first matching credential for a domain.
func (t *CredentialedFetchTool) resolveCredential(ctx context.Context, domain string) (*SiteCredential, string) {
	for i := range t.credentials {
		cred := &t.credentials[i]
		if matchDomain(cred.DomainPattern, domain) {
			val, err := t.secrets.Get(ctx, cred.SecretKey)
			if err != nil {
				t.logger.Warn("credentialed_fetch: secret lookup failed",
					"key", cred.SecretKey, "domain", domain, "err", err)
				continue
			}
			if val != "" {
				return cred, val
			}
		}
	}
	return nil, ""
}

// matchDomain checks if a domain matches a pattern (supports leading wildcard).
// Examples: "*.github.com" matches "api.github.com", "github.com" matches exactly.
func matchDomain(pattern, domain string) bool {
	if pattern == domain {
		return true
	}
	if strings.HasPrefix(pattern, "*.") {
		suffix := pattern[1:] // ".github.com"
		return strings.HasSuffix(domain, suffix) || domain == pattern[2:]
	}
	return false
}

var _ agent.Tool = (*CredentialedFetchTool)(nil)
