package ghl

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
)

const baseURL = "https://services.leadconnectorhq.com"

// LocationConfig holds per-location credentials and metadata.
type LocationConfig struct {
	ID    string `json:"id" yaml:"id"`
	Token string `json:"token" yaml:"token"`
	Name  string `json:"name" yaml:"name"`
}

// Client is a GoHighLevel API client with multi-location support.
type Client struct {
	defaultLocationID string
	locations         map[string]LocationConfig // alias → config
	httpClient        *http.Client
	mu                sync.RWMutex
}

// NewClient creates a GHL API client with a single default location.
// Use AddLocation to register additional locations.
func NewClient(apiKey, locationID string) *Client {
	c := &Client{
		defaultLocationID: locationID,
		locations: map[string]LocationConfig{
			"default": {ID: locationID, Token: apiKey, Name: "Default"},
		},
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
	return c
}

// AddLocation registers an additional GHL location by alias.
func (c *Client) AddLocation(alias string, cfg LocationConfig) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.locations[alias] = cfg
}

// LocationID returns the default location ID.
func (c *Client) LocationID() string { return c.defaultLocationID }

// ListLocations returns all registered location aliases and names.
func (c *Client) ListLocations() map[string]string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make(map[string]string, len(c.locations))
	for alias, cfg := range c.locations {
		result[alias] = cfg.Name + " (" + cfg.ID + ")"
	}
	return result
}

// resolveLocation returns the LocationConfig for the given alias or location ID.
// Falls back to default if empty or not found.
func (c *Client) resolveLocation(location string) LocationConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if location == "" {
		return c.locations["default"]
	}

	// Check by alias first.
	if cfg, ok := c.locations[location]; ok {
		return cfg
	}

	// Check by location ID.
	for _, cfg := range c.locations {
		if cfg.ID == location {
			return cfg
		}
	}

	// Fall back to default.
	return c.locations["default"]
}

// GetFor performs an authenticated GET for a specific location.
func (c *Client) GetFor(ctx context.Context, location, path string, params url.Values) ([]byte, error) {
	cfg := c.resolveLocation(location)
	if params == nil {
		params = url.Values{}
	}
	params.Set("locationId", cfg.ID)

	u := baseURL + path + "?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, fmt.Errorf("ghl.Get build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Token)
	req.Header.Set("Version", "2021-07-28")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ghl.Get: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("ghl.Get read body: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("ghl.Get %s: HTTP %d — %s", path, resp.StatusCode, string(body))
	}
	return body, nil
}

// PostFor performs an authenticated POST for a specific location.
func (c *Client) PostFor(ctx context.Context, location, path string, payload any) ([]byte, error) {
	return c.doJSONFor(ctx, location, "POST", path, payload)
}

// PutFor performs an authenticated PUT for a specific location.
func (c *Client) PutFor(ctx context.Context, location, path string, payload any) ([]byte, error) {
	return c.doJSONFor(ctx, location, "PUT", path, payload)
}

// DeleteFor performs an authenticated DELETE for a specific location.
func (c *Client) DeleteFor(ctx context.Context, location, path string) ([]byte, error) {
	cfg := c.resolveLocation(location)
	req, err := http.NewRequestWithContext(ctx, "DELETE", baseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("ghl.Delete build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Token)
	req.Header.Set("Version", "2021-07-28")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ghl.Delete: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("ghl.Delete read body: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("ghl.Delete %s: HTTP %d — %s", path, resp.StatusCode, string(body))
	}
	return body, nil
}

func (c *Client) doJSONFor(ctx context.Context, location, method, path string, payload any) ([]byte, error) {
	cfg := c.resolveLocation(location)
	jsonBody, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("ghl marshal body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, method, baseURL+path, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("ghl.%s build request: %w", method, err)
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Version", "2021-07-28")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ghl.%s: %w", method, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("ghl.%s read body: %w", method, err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("ghl.%s %s: HTTP %d — %s", method, path, resp.StatusCode, string(respBody))
	}
	return respBody, nil
}

// Backward-compatible methods that use the default location.

func (c *Client) Get(ctx context.Context, path string, params url.Values) ([]byte, error) {
	return c.GetFor(ctx, "", path, params)
}

func (c *Client) Post(ctx context.Context, path string, body any) ([]byte, error) {
	return c.PostFor(ctx, "", path, body)
}

func (c *Client) Put(ctx context.Context, path string, body any) ([]byte, error) {
	return c.PutFor(ctx, "", path, body)
}
