// Package github implements the GitHub skill for ZBOT.
package github

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

const apiBase = "https://api.github.com"

// Client is a GitHub REST API client.
type Client struct {
	token      string
	httpClient *http.Client
}

// NewClient creates a GitHub API client.
func NewClient(token string) *Client {
	return &Client{
		token:      token,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// Get performs an authenticated GET request to the GitHub API.
func (c *Client) Get(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", apiBase+path, nil)
	if err != nil {
		return nil, fmt.Errorf("github.Get build request: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github.Get: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, fmt.Errorf("github.Get read body: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("github.Get %s: HTTP %d — %s", path, resp.StatusCode, string(body))
	}
	return body, nil
}

// Post performs an authenticated POST request with JSON body.
func (c *Client) Post(ctx context.Context, path string, jsonBody []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", apiBase+path, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("github.Post build request: %w", err)
	}
	c.setHeaders(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github.Post: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, fmt.Errorf("github.Post read body: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("github.Post %s: HTTP %d — %s", path, resp.StatusCode, string(respBody))
	}
	return respBody, nil
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
}
