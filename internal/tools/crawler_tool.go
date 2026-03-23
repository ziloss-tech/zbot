package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/ziloss-tech/zbot/internal/agent"
	"github.com/ziloss-tech/zbot/internal/crawler"
)

// CrawlerTool implements the Tool interface for web crawling
type CrawlerTool struct {
	sessions *crawler.SessionManager
}

// NewCrawlerTool creates a new CrawlerTool instance
func NewCrawlerTool(sessions *crawler.SessionManager) *CrawlerTool {
	return &CrawlerTool{
		sessions: sessions,
	}
}

// Name returns the tool name
func (t *CrawlerTool) Name() string {
	return "web_crawl"
}

// Definition returns the tool definition matching agent.Tool interface
func (t *CrawlerTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "web_crawl",
		Description: "Browse the web visually. Navigate to URLs, click elements by grid coordinate, type text, and scroll. Every action is logged with screenshots. Use the 'elements' action to see all clickable elements with their grid positions.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"action": map[string]any{
					"type": "string",
					"enum": []string{"navigate", "screenshot", "click", "type", "scroll", "read", "elements", "start", "stop"},
					"description": "The action to perform",
				},
				"url": map[string]any{
					"type": "string",
					"description": "URL to navigate to (for 'navigate' action)",
				},
				"grid": map[string]any{
					"type": "string",
					"description": "Grid cell to click (for 'click' action), e.g. 'C7'",
				},
				"text": map[string]any{
					"type": "string",
					"description": "Text to type (for 'type' action)",
				},
				"direction": map[string]any{
					"type": "string",
					"enum": []string{"up", "down", "left", "right"},
					"description": "Scroll direction (for 'scroll' action)",
				},
				"amount": map[string]any{
					"type": "integer",
					"description": "Scroll amount in units (for 'scroll' action)",
				},
				"session_id": map[string]any{
					"type": "string",
					"description": "Session ID (optional, uses most recent session if not provided)",
				},
			},
			"required": []string{"action"},
		},
	}
}

// Execute processes tool input and performs the requested action
func (t *CrawlerTool) Execute(ctx context.Context, input map[string]any) (*agent.ToolResult, error) {
	// Extract action from input
	actionRaw, ok := input["action"]
	if !ok {
		return &agent.ToolResult{
			Content: "",
			IsError: true,
		}, fmt.Errorf("action is required")
	}

	action, ok := actionRaw.(string)
	if !ok {
		return &agent.ToolResult{
			Content: "",
			IsError: true,
		}, fmt.Errorf("action must be a string")
	}

	// Extract optional parameters
	var params struct {
		Action    string
		URL       string
		Grid      string
		Text      string
		Direction string
		Amount    int
		SessionID string
	}

	params.Action = action
	if url, ok := input["url"].(string); ok {
		params.URL = url
	}
	if grid, ok := input["grid"].(string); ok {
		params.Grid = grid
	}
	if text, ok := input["text"].(string); ok {
		params.Text = text
	}
	if direction, ok := input["direction"].(string); ok {
		params.Direction = direction
	}
	if amount, ok := input["amount"].(float64); ok {
		params.Amount = int(amount)
	}
	if sessionID, ok := input["session_id"].(string); ok {
		params.SessionID = sessionID
	}

	// Validate action
	action = strings.ToLower(strings.TrimSpace(params.Action))

	// Route to appropriate action handler
	switch action {
	case "start":
		content, err := t.handleStart()
		if err != nil {
			return &agent.ToolResult{Content: "", IsError: true}, err
		}
		return &agent.ToolResult{Content: content, IsError: false}, nil
	case "navigate":
		content, err := t.handleNavigate(params.URL, params.SessionID)
		if err != nil {
			return &agent.ToolResult{Content: "", IsError: true}, err
		}
		return &agent.ToolResult{Content: content, IsError: false}, nil
	case "screenshot":
		content, err := t.handleScreenshot(params.SessionID)
		if err != nil {
			return &agent.ToolResult{Content: "", IsError: true}, err
		}
		return &agent.ToolResult{Content: content, IsError: false}, nil
	case "click":
		content, err := t.handleClick(params.Grid, params.SessionID)
		if err != nil {
			return &agent.ToolResult{Content: "", IsError: true}, err
		}
		return &agent.ToolResult{Content: content, IsError: false}, nil
	case "type":
		content, err := t.handleType(params.Text, params.SessionID)
		if err != nil {
			return &agent.ToolResult{Content: "", IsError: true}, err
		}
		return &agent.ToolResult{Content: content, IsError: false}, nil
	case "scroll":
		content, err := t.handleScroll(params.Direction, params.Amount, params.SessionID)
		if err != nil {
			return &agent.ToolResult{Content: "", IsError: true}, err
		}
		return &agent.ToolResult{Content: content, IsError: false}, nil
	case "read":
		content, err := t.handleRead(params.SessionID)
		if err != nil {
			return &agent.ToolResult{Content: "", IsError: true}, err
		}
		return &agent.ToolResult{Content: content, IsError: false}, nil
	case "elements":
		content, err := t.handleElements(params.SessionID)
		if err != nil {
			return &agent.ToolResult{Content: "", IsError: true}, err
		}
		return &agent.ToolResult{Content: content, IsError: false}, nil
	case "stop":
		content, err := t.handleStop(params.SessionID)
		if err != nil {
			return &agent.ToolResult{Content: "", IsError: true}, err
		}
		return &agent.ToolResult{Content: content, IsError: false}, nil
	default:
		return &agent.ToolResult{Content: "", IsError: true}, fmt.Errorf("unknown action: %s", action)
	}
}

// getOrCreateSession retrieves an existing session or creates a new one
func (t *CrawlerTool) getOrCreateSession(sessionID string) (*crawler.Crawler, string, error) {
	if sessionID != "" {
		// Try to get existing session
		session, err := t.sessions.GetSession(sessionID)
		if err == nil && session != nil {
			return session, sessionID, nil
		}
	}

	// Try to get most recent session
	sessions := t.sessions.ListSessions()
	if len(sessions) > 0 {
		// Return the first session if available
		session, err := t.sessions.GetSession(sessions[0].SessionID)
		if err == nil && session != nil {
			return session, sessions[0].SessionID, nil
		}
	}

	// Create a new session with default viewport
	viewport := crawler.ViewportSize{
		Width:  1280,
		Height: 720,
	}
	newSessionID, err := t.sessions.StartSession(viewport)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create session: %w", err)
	}

	newSession, err := t.sessions.GetSession(newSessionID)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get new session: %w", err)
	}

	return newSession, newSessionID, nil
}

// handleStart starts a new crawl session
func (t *CrawlerTool) handleStart() (string, error) {
	viewport := crawler.ViewportSize{
		Width:  1280,
		Height: 720,
	}

	sessionID, err := t.sessions.StartSession(viewport)
	if err != nil {
		return "", fmt.Errorf("failed to start session: %w", err)
	}

	session, err := t.sessions.GetSession(sessionID)
	if err != nil {
		return "", fmt.Errorf("failed to get session: %w", err)
	}

	grid := session.Grid()
	return fmt.Sprintf("Started new crawl session. Session ID: %s. Grid: %dx%d cells. Ready to navigate.", sessionID, grid.Cols, grid.Rows), nil
}

// handleNavigate navigates to a URL
func (t *CrawlerTool) handleNavigate(url string, sessionID string) (string, error) {
	if url == "" {
		return "", fmt.Errorf("url is required for navigate action")
	}

	session, actualSessionID, err := t.getOrCreateSession(sessionID)
	if err != nil {
		return "", err
	}

	if err := session.Navigate(url); err != nil {
		return "", fmt.Errorf("failed to navigate to %s: %w", url, err)
	}

	title := session.PageTitle()
	return fmt.Sprintf("Navigated to %s. Page title: %s. Session: %s. Use 'elements' action to see interactive elements.", url, title, actualSessionID), nil
}

// handleScreenshot returns a text description of the page
func (t *CrawlerTool) handleScreenshot(sessionID string) (string, error) {
	session, _, err := t.getOrCreateSession(sessionID)
	if err != nil {
		return "", err
	}

	// Get page info
	title := session.PageTitle()
	url := session.CurrentURL()
	status := session.Status()

	// Format response
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Page: %s\n", title))
	sb.WriteString(fmt.Sprintf("URL: %s\n", url))
	sb.WriteString(fmt.Sprintf("Status: %s\n", status))
	sb.WriteString("Use 'elements' action to see all interactive elements.\n")

	return sb.String(), nil
}

// handleClick clicks on a grid cell
func (t *CrawlerTool) handleClick(gridCell string, sessionID string) (string, error) {
	if gridCell == "" {
		return "", fmt.Errorf("grid cell is required for click action")
	}

	session, _, err := t.getOrCreateSession(sessionID)
	if err != nil {
		return "", err
	}

	gridCell = strings.ToUpper(strings.TrimSpace(gridCell))

	// Click the element
	result, err := session.Click(gridCell)
	if err != nil {
		return "", fmt.Errorf("failed to click at %s: %w", gridCell, err)
	}

	// Get updated page info
	newURL := session.CurrentURL()
	newTitle := session.PageTitle()

	return fmt.Sprintf("Clicked %s → <%s> '%s'. Page is now: %s (%s)", gridCell, result.Element.Tag, result.Element.Text, newURL, newTitle), nil
}

// handleType types text into the focused element
func (t *CrawlerTool) handleType(text string, sessionID string) (string, error) {
	if text == "" {
		return "", fmt.Errorf("text is required for type action")
	}

	session, _, err := t.getOrCreateSession(sessionID)
	if err != nil {
		return "", err
	}

	if err := session.Type(text); err != nil {
		return "", fmt.Errorf("failed to type text: %w", err)
	}

	return fmt.Sprintf("Typed '%s' into focused element.", text), nil
}

// handleScroll scrolls the page
func (t *CrawlerTool) handleScroll(direction string, amount int, sessionID string) (string, error) {
	if direction == "" {
		return "", fmt.Errorf("direction is required for scroll action")
	}

	if amount <= 0 {
		amount = 3 // default scroll amount
	}

	session, _, err := t.getOrCreateSession(sessionID)
	if err != nil {
		return "", err
	}

	direction = strings.ToLower(strings.TrimSpace(direction))

	// Validate direction
	validDirections := map[string]bool{"up": true, "down": true, "left": true, "right": true}
	if !validDirections[direction] {
		return "", fmt.Errorf("invalid direction: %s (must be up, down, left, or right)", direction)
	}

	if err := session.Scroll(direction, amount); err != nil {
		return "", fmt.Errorf("failed to scroll: %w", err)
	}

	return fmt.Sprintf("Scrolled %s by %d units. Use 'elements' to see what's now visible.", direction, amount), nil
}

// handleRead extracts and returns page text content
func (t *CrawlerTool) handleRead(sessionID string) (string, error) {
	session, _, err := t.getOrCreateSession(sessionID)
	if err != nil {
		return "", err
	}

	text, err := session.PageText()
	if err != nil {
		return "", fmt.Errorf("failed to read page text: %w", err)
	}

	// Truncate if too long
	if len(text) > 4000 {
		text = text[:4000] + "\n[truncated...]"
	}

	return text, nil
}

// handleElements lists all interactive elements with grid positions
func (t *CrawlerTool) handleElements(sessionID string) (string, error) {
	session, _, err := t.getOrCreateSession(sessionID)
	if err != nil {
		return "", err
	}

	grid := session.Grid()
	var sb strings.Builder
	sb.WriteString("Interactive elements on page:\n\n")

	elementCount := 0
	// Scan all grid cells for elements
	cells := grid.AllCells()
	for _, gridCell := range cells {
		elem, err := session.ElementAtGrid(gridCell.Label)
		if err != nil || elem == nil || elem.Tag == "unknown" || elem.Tag == "" {
			continue
		}
		elementCount++
		sb.WriteString(fmt.Sprintf("%s: <%s>\n", gridCell.Label, elem.Tag))
		if elem.Text != "" {
			sb.WriteString(fmt.Sprintf("    Text: '%s'\n", elem.Text))
		}
		if len(elem.Attrs) > 0 {
			for k, v := range elem.Attrs {
				if k == "href" || k == "placeholder" || k == "type" {
					sb.WriteString(fmt.Sprintf("    %s: %s\n", strings.Title(k), v))
				}
			}
		}
		sb.WriteString("\n")
	}

	if elementCount == 0 {
		sb.WriteString("(no interactive elements found)\n")
	}

	return sb.String(), nil
}

// handleStop stops the current crawl session
func (t *CrawlerTool) handleStop(sessionID string) (string, error) {
	_, actualSessionID, err := t.getOrCreateSession(sessionID)
	if err != nil {
		return "", err
	}

	if err := t.sessions.StopSession(actualSessionID); err != nil {
		return "", fmt.Errorf("failed to stop session: %w", err)
	}

	return fmt.Sprintf("Crawl session %s stopped.", actualSessionID), nil
}
