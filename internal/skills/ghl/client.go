// Package ghl provides the GoHighLevel skill for ZBOT.
// Gives ZBOT access to CRM contacts, conversations, pipelines, and messaging.
package ghl

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const baseURL = "https://services.leadconnectorhq.com"

// Client is a GoHighLevel API client.
type Client struct {
	apiKey     string
	locationID string
	httpClient *http.Client
}

// NewClient creates a GHL API client.
func NewClient(apiKey, locationID string) *Client {
	return &Client{
		apiKey:     apiKey,
		locationID: locationID,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// LocationID returns the configured location.
func (c *Client) LocationID() string { return c.locationID }

// Get performs an authenticated GET request.
func (c *Client) Get(ctx context.Context, path string, params url.Values) ([]byte, error) {
	u := baseURL + path
	if len(params) > 0 {
		u += "?" + params.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, fmt.Errorf("ghl.Get build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
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

// Post performs an authenticated POST request with JSON body.
func (c *Client) Post(ctx context.Context, path string, body any) ([]byte, error) {
	return c.doJSON(ctx, "POST", path, body)
}

// Put performs an authenticated PUT request with JSON body.
func (c *Client) Put(ctx context.Context, path string, body any) ([]byte, error) {
	return c.doJSON(ctx, "PUT", path, body)
}

func (c *Client) doJSON(ctx context.Context, method, path string, payload any) ([]byte, error) {
	jsonBody, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("ghl marshal body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, method, baseURL+path, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("ghl.%s build request: %w", method, err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
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
