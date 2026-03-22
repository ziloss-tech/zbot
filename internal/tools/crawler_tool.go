package tools

import (
	"context"
	"fmt"

	"github.com/ziloss-tech/zbot/internal/agent"
	"github.com/ziloss-tech/zbot/internal/crawler"
)

// CrawlerTool lets Cortex browse the web visually via grid coordinates.
// Every action is logged with screenshots and element metadata.
type CrawlerTool struct {
	sessions *crawler.SessionManager
	emitter  crawler.EventEmitter
}

func NewCrawlerTool(sessions *crawler.SessionManager, emitter crawler.EventEmitter) *CrawlerTool {
	return &CrawlerTool{sessions: sessions, emitter: emitter}
}

func (t *CrawlerTool) Name() string { return "web_crawl" }

func (t *CrawlerTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name: "web_crawl",
		Description: `Browse the web visually with grid-based navigation. Actions:
- navigate: Go to a URL
- screenshot: Get current page state with interactive element positions
- click: Click a grid cell (e.g. "C7")
- type: Type text into the focused element
- scroll: Scroll up/down/left/right
- read: Extract all visible text from the current page
- elements: List interactive elements with their grid positions
- stop: Close the browser session`,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"action": map[string]any{
					"type":        "string",
					"description": "Action to perform",
					"enum":        []string{"navigate", "screenshot", "click", "type", "scroll", "read", "elements", "stop"},
				},
				"url": map[string]any{
					"type":        "string",
					"description": "URL to navigate to (for navigate action)",
				},
				"grid": map[string]any{
					"type":        "string",
					"description": "Grid cell to click, e.g. C7 (for click action)",
				},
				"text": map[string]any{
					"type":        "string",
					"description": "Text to type (for type action)",
				},
				"direction": map[string]any{
					"type":        "string",
					"description": "Scroll direction: up, down, left, right",
				},
				"amount": map[string]any{
					"type":        "integer",
					"description": "Scroll amount (default 3)",
					"default":     3,
				},
			},
			"required": []string{"action"},
		},
	}
}

func (t *CrawlerTool) Execute(ctx context.Context, input map[string]any) (*agent.ToolResult, error) {
	action, _ := input["action"].(string)
	if action == "" {
		return nil, fmt.Errorf("action is required")
	}

	// Get or create the session (one active session per tool instance).
	c := t.getOrCreateSession()
	if c == nil && action != "navigate" {
		return &agent.ToolResult{Content: "No browser session active. Use action 'navigate' with a URL first."}, nil
	}

	switch action {
	case "navigate":
		url, _ := input["url"].(string)
		if url == "" {
			return nil, fmt.Errorf("url is required for navigate action")
		}
		if c == nil {
			var err error
			c, err = t.sessions.Start(1280, 960, 64, t.emitter)
			if err != nil {
				return nil, fmt.Errorf("start browser: %w", err)
			}
		}
		if err := c.Navigate(url); err != nil {
			return &agent.ToolResult{Content: fmt.Sprintf("Navigate failed: %s", err)}, nil
		}
		elements, _ := c.InteractiveElements()
		return &agent.ToolResult{
			Content: fmt.Sprintf("Navigated to %s\nPage: %s\n\nInteractive elements:\n%s", c.CurrentURL(), url, elements),
		}, nil

	case "screenshot", "elements":
		elements, _ := c.InteractiveElements()
		return &agent.ToolResult{
			Content: fmt.Sprintf("Current page: %s\nURL: %s\n\nInteractive elements:\n%s", c.CurrentURL(), c.CurrentURL(), elements),
		}, nil

	case "click":
		grid, _ := input["grid"].(string)
		if grid == "" {
			return nil, fmt.Errorf("grid cell is required for click action")
		}
		info, err := c.Click(grid)
		var desc string
		if info != nil {
			desc = fmt.Sprintf("Clicked %s → <%s>", grid, info.Tag)
			if info.Text != "" {
				desc += fmt.Sprintf(" %q", info.Text)
			}
		} else {
			desc = fmt.Sprintf("Clicked %s", grid)
		}
		if err != nil {
			desc += fmt.Sprintf(" (error: %s)", err)
		}
		elements, _ := c.InteractiveElements()
		return &agent.ToolResult{
			Content: fmt.Sprintf("%s\nNow at: %s\n\nInteractive elements:\n%s", desc, c.CurrentURL(), elements),
		}, nil

	case "type":
		text, _ := input["text"].(string)
		if text == "" {
			return nil, fmt.Errorf("text is required for type action")
		}
		if err := c.Type(text); err != nil {
			return &agent.ToolResult{Content: fmt.Sprintf("Type failed: %s", err)}, nil
		}
		return &agent.ToolResult{Content: fmt.Sprintf("Typed %q", text)}, nil

	case "scroll":
		dir, _ := input["direction"].(string)
		if dir == "" {
			dir = "down"
		}
		amount := 3
		if a, ok := input["amount"].(float64); ok {
			amount = int(a)
		}
		if err := c.Scroll(dir, amount); err != nil {
			return &agent.ToolResult{Content: fmt.Sprintf("Scroll failed: %s", err)}, nil
		}
		elements, _ := c.InteractiveElements()
		return &agent.ToolResult{
			Content: fmt.Sprintf("Scrolled %s x%d\n\nInteractive elements:\n%s", dir, amount, elements),
		}, nil

	case "read":
		text, err := c.ReadPageText()
		if err != nil {
			return &agent.ToolResult{Content: fmt.Sprintf("Read failed: %s", err)}, nil
		}
		return &agent.ToolResult{Content: text}, nil

	case "stop":
		for _, s := range t.sessions.List() {
			_ = t.sessions.Stop(s.SessionID)
		}
		return &agent.ToolResult{Content: "Browser session closed."}, nil

	default:
		return nil, fmt.Errorf("unknown action: %s", action)
	}
}

// getOrCreateSession returns the first active session, or nil if none.
func (t *CrawlerTool) getOrCreateSession() *crawler.Crawler {
	sessions := t.sessions.List()
	for _, s := range sessions {
		if s.Status != crawler.StatusStopped {
			c, _ := t.sessions.Get(s.SessionID)
			return c
		}
	}
	return nil
}
