package reviewer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Checker struct {
	client   *http.Client
	endpoint string
	model    string
	apiKey   string
}

func NewChecker(endpoint, model, apiKey string) *Checker {
	return &Checker{
		client:   &http.Client{Timeout: 30 * time.Second},
		endpoint: endpoint,
		model:    model,
		apiKey:   apiKey,
	}
}

const reviewerSystemPrompt = `You are a code and action reviewer for an AI agent called ZBOT. You receive logs of actions taken by the agent (tool calls, code output, navigation, etc.). Your job:
1. Find bugs, security issues, logic errors, missed edge cases, and better approaches
2. For each finding, respond with JSON: severity (critical/warning/info), category (bug/security/performance/style/logic), description, location, suggestion, confidence (0-1)
3. If everything looks good, return an empty array
4. Be concise. Only flag real issues, not style preferences.

Respond ONLY with a JSON array of findings. No preamble, no markdown.`

func (c *Checker) Check(ctx context.Context, chunk ReviewChunk) ([]ReviewFinding, error) {
	userContent := fmt.Sprintf("Session: %s\nSummary: %s\n\nActions:\n%s",
		chunk.SessionID, chunk.Summary, strings.Join(chunk.Actions, "\n"))

	reqBody := map[string]any{
		"model": c.model,
		"messages": []map[string]string{
			{"role": "system", "content": reviewerSystemPrompt},
			{"role": "user", "content": userContent},
		},
		"temperature": 0.1,
		"max_tokens":  1000,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.endpoint, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(body[:min(200, len(body))]))
	}

	// Parse OpenAI response
	var apiResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("parse API response: %w", err)
	}
	if len(apiResp.Choices) == 0 {
		return nil, fmt.Errorf("no choices in API response")
	}

	content := strings.TrimSpace(apiResp.Choices[0].Message.Content)
	// Strip markdown fences if present
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	var findings []ReviewFinding
	if err := json.Unmarshal([]byte(content), &findings); err != nil {
		return nil, fmt.Errorf("parse findings: %w (raw: %s)", err, content[:min(200, len(content))])
	}

	// Set timestamps and model
	now := time.Now()
	for i := range findings {
		findings[i].Timestamp = now
		findings[i].ReviewerModel = c.model
		findings[i].ID = fmt.Sprintf("rf-%d-%d", now.UnixMilli(), i)
	}

	return findings, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
