package webui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/ziloss-tech/zbot/internal/crawler"
)

// StartSessionRequest represents the request body for starting a session
type StartSessionRequest struct {
	ViewportWidth  int `json:"viewport_width"`
	ViewportHeight int `json:"viewport_height"`
}

// StartSessionResponse represents the response from starting a session
type StartSessionResponse struct {
	SessionID string `json:"session_id"`
	Grid      struct {
		Rows      int `json:"rows"`
		Cols      int `json:"cols"`
		CellW     int `json:"cell_w"`
		CellH     int `json:"cell_h"`
		ViewportW int `json:"viewport_w"`
		ViewportH int `json:"viewport_h"`
	} `json:"grid"`
}

// handleStartSession handles POST /api/crawler/start
func (s *Server) handleStartSession(w http.ResponseWriter, r *http.Request) {
	var req StartSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.ViewportWidth <= 0 || req.ViewportHeight <= 0 {
		writeError(w, http.StatusBadRequest, "viewport dimensions must be positive")
		return
	}

	viewport := crawler.ViewportSize{
		Width:  req.ViewportWidth,
		Height: req.ViewportHeight,
	}

	sessionID, err := s.crawlerSessions.StartSession(viewport)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create session")
		return
	}

	// Get the crawler to access grid info
	crawlerInstance, err := s.crawlerSessions.GetSession(sessionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get session")
		return
	}

	grid := crawlerInstance.Grid()
	resp := StartSessionResponse{
		SessionID: sessionID,
	}
	resp.Grid.Rows = grid.Rows
	resp.Grid.Cols = grid.Cols
	resp.Grid.CellW = grid.CellWidth
	resp.Grid.CellH = grid.CellHeight
	resp.Grid.ViewportW = grid.ViewportW
	resp.Grid.ViewportH = grid.ViewportH

	writeJSON(w, http.StatusOK, resp)
}

// NavigateRequest represents the request body for navigating
type NavigateRequest struct {
	SessionID string `json:"session_id"`
	URL       string `json:"url"`
}

// NavigateResponse represents the response from navigating
type NavigateResponse struct {
	Success    bool   `json:"success"`
	PageTitle  string `json:"page_title"`
	URL        string `json:"url"`
	Screenshot string `json:"screenshot"`
}

// handleNavigate handles POST /api/crawler/navigate
func (s *Server) handleNavigate(w http.ResponseWriter, r *http.Request) {
	var req NavigateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.SessionID == "" || req.URL == "" {
		writeError(w, http.StatusBadRequest, "session_id and url are required")
		return
	}

	crawlerInstance, err := s.crawlerSessions.GetSession(req.SessionID)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	if err := crawlerInstance.Navigate(req.URL); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to navigate")
		return
	}

	// Get page title and screenshot after navigation
	pageTitle := crawlerInstance.PageTitle()
	screenshot, err := crawlerInstance.Screenshot()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get screenshot")
		return
	}

	resp := NavigateResponse{
		Success:    true,
		PageTitle:  pageTitle,
		URL:        req.URL,
		Screenshot: screenshot,
	}

	writeJSON(w, http.StatusOK, resp)
}

// ClickRequest represents the request body for clicking
type ClickRequest struct {
	SessionID string `json:"session_id"`
	GridCell  string `json:"grid_cell"`
}

// ClickResponse represents the response from clicking
type ClickResponse struct {
	Success    bool   `json:"success"`
	Element    map[string]interface{} `json:"element"`
	Screenshot string `json:"screenshot"`
}

// handleClick handles POST /api/crawler/click
func (s *Server) handleClick(w http.ResponseWriter, r *http.Request) {
	var req ClickRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.SessionID == "" || req.GridCell == "" {
		writeError(w, http.StatusBadRequest, "session_id and grid_cell are required")
		return
	}

	crawlerInstance, err := s.crawlerSessions.GetSession(req.SessionID)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	result, err := crawlerInstance.Click(req.GridCell)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to click element")
		return
	}

	// Convert ElementInfo to map for JSON response
	elementMap := map[string]interface{}{
		"tag":  result.Element.Tag,
		"text": result.Element.Text,
	}

	resp := ClickResponse{
		Success:    true,
		Element:    elementMap,
		Screenshot: result.AfterShot,
	}

	writeJSON(w, http.StatusOK, resp)
}

// TypeRequest represents the request body for typing
type TypeRequest struct {
	SessionID string `json:"session_id"`
	Text      string `json:"text"`
}

// TypeResponse represents the response from typing
type TypeResponse struct {
	Success bool `json:"success"`
}

// handleType handles POST /api/crawler/type
func (s *Server) handleType(w http.ResponseWriter, r *http.Request) {
	var req TypeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.SessionID == "" {
		writeError(w, http.StatusBadRequest, "session_id is required")
		return
	}

	crawlerInstance, err := s.crawlerSessions.GetSession(req.SessionID)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	if err := crawlerInstance.Type(req.Text); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to type text")
		return
	}

	resp := TypeResponse{
		Success: true,
	}

	writeJSON(w, http.StatusOK, resp)
}

// ScrollRequest represents the request body for scrolling
type ScrollRequest struct {
	SessionID string `json:"session_id"`
	Direction string `json:"direction"`
	Amount    int    `json:"amount"`
}

// ScrollResponse represents the response from scrolling
type ScrollResponse struct {
	Success    bool   `json:"success"`
	Screenshot string `json:"screenshot"`
}

// handleScroll handles POST /api/crawler/scroll
func (s *Server) handleScroll(w http.ResponseWriter, r *http.Request) {
	var req ScrollRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.SessionID == "" || req.Direction == "" {
		writeError(w, http.StatusBadRequest, "session_id and direction are required")
		return
	}

	crawlerInstance, err := s.crawlerSessions.GetSession(req.SessionID)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	if err := crawlerInstance.Scroll(req.Direction, req.Amount); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to scroll")
		return
	}

	// Get screenshot after scroll
	screenshot, err := crawlerInstance.Screenshot()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get screenshot")
		return
	}

	resp := ScrollResponse{
		Success:    true,
		Screenshot: screenshot,
	}

	writeJSON(w, http.StatusOK, resp)
}

// ScreenshotResponse represents the response from getting a screenshot
type ScreenshotResponse struct {
	Screenshot string `json:"screenshot"`
	Grid       bool   `json:"grid"`
}

// handleGetScreenshot handles GET /api/crawler/screenshot
func (s *Server) handleGetScreenshot(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session_id")
	gridStr := r.URL.Query().Get("grid")
	includeGrid := gridStr == "true"

	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "session_id is required")
		return
	}

	crawlerInstance, err := s.crawlerSessions.GetSession(sessionID)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	var screenshot string
	if includeGrid {
		ss, err := crawlerInstance.ScreenshotWithGrid()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to get screenshot with grid")
			return
		}
		screenshot = ss
	} else {
		ss, err := crawlerInstance.Screenshot()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to get screenshot")
			return
		}
		screenshot = ss
	}

	resp := ScreenshotResponse{
		Screenshot: screenshot,
		Grid:       includeGrid,
	}

	writeJSON(w, http.StatusOK, resp)
}

// ElementsResponse represents the response from getting elements
type ElementsResponse struct {
	Elements  []map[string]interface{} `json:"elements"`
	Formatted []string                 `json:"formatted"`
}

// handleGetElements handles GET /api/crawler/elements
func (s *Server) handleGetElements(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session_id")

	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "session_id is required")
		return
	}

	crawlerInstance, err := s.crawlerSessions.GetSession(sessionID)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	// Get grid to enumerate all cells and find elements
	grid := crawlerInstance.Grid()
	elements := make([]map[string]interface{}, 0)
	formatted := make([]string, 0)

	// Scan all grid cells for elements
	cells := grid.AllCells()
	for _, gridCell := range cells {
		elemInfo, err := crawlerInstance.ElementAtGrid(gridCell.Label)
		if err != nil {
			continue
		}
		if elemInfo != nil && elemInfo.Tag != "unknown" && elemInfo.Tag != "" {
			elemMap := map[string]interface{}{
				"grid_cell": gridCell.Label,
				"tag":       elemInfo.Tag,
				"text":      elemInfo.Text,
			}
			if len(elemInfo.Attrs) > 0 {
				elemMap["attrs"] = elemInfo.Attrs
			}
			elements = append(elements, elemMap)
			formatted = append(formatted, fmt.Sprintf("%s: <%s> '%s'", gridCell.Label, elemInfo.Tag, elemInfo.Text))
		}
	}

	resp := ElementsResponse{
		Elements:  elements,
		Formatted: formatted,
	}

	writeJSON(w, http.StatusOK, resp)
}

// ActionEntry represents a single action log entry
type ActionEntry struct {
	Timestamp string                 `json:"timestamp"`
	Action    string                 `json:"action"`
	Details   map[string]interface{} `json:"details"`
}

// handleGetLog handles GET /api/crawler/log
func (s *Server) handleGetLog(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session_id")
	tailStr := r.URL.Query().Get("tail")

	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "session_id is required")
		return
	}

	tail := 50
	if tailStr != "" {
		if t, err := strconv.Atoi(tailStr); err == nil && t > 0 {
			tail = t
		}
	}

	crawlerInstance, err := s.crawlerSessions.GetSession(sessionID)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	// Get action log from logger
	logger := crawlerInstance.Logger()
	logEntries := logger.Tail(tail)

	writeJSON(w, http.StatusOK, logEntries)
}

// StopSessionRequest represents the request body for stopping a session
type StopSessionRequest struct {
	SessionID string `json:"session_id"`
}

// StopSessionResponse represents the response from stopping a session
type StopSessionResponse struct {
	Success bool `json:"success"`
}

// handleStopSession handles POST /api/crawler/stop
func (s *Server) handleStopSession(w http.ResponseWriter, r *http.Request) {
	var req StopSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.SessionID == "" {
		writeError(w, http.StatusBadRequest, "session_id is required")
		return
	}

	if err := s.crawlerSessions.StopSession(req.SessionID); err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	resp := StopSessionResponse{
		Success: true,
	}

	writeJSON(w, http.StatusOK, resp)
}

// SessionInfo represents information about a single session
type SessionInfo struct {
	SessionID    string `json:"session_id"`
	CreatedAt    string `json:"created_at"`
	CurrentURL   string `json:"current_url"`
	ViewportW    int    `json:"viewport_w"`
	ViewportH    int    `json:"viewport_h"`
	ActionCount  int    `json:"action_count"`
}

// ListSessionsResponse represents the response from listing sessions
type ListSessionsResponse struct {
	Sessions []SessionInfo `json:"sessions"`
}

// handleListSessions handles GET /api/crawler/sessions
func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	sessions := s.crawlerSessions.ListSessions()

	sessionInfos := make([]SessionInfo, len(sessions))
	for i, sess := range sessions {
		sessionInfos[i] = SessionInfo{
			SessionID:   sess.SessionID,
			CreatedAt:   sess.CreatedAt.String(),
			CurrentURL:  sess.CurrentURL,
			ViewportW:   sess.Viewport.Width,
			ViewportH:   sess.Viewport.Height,
			ActionCount: sess.ActionCount,
		}
	}

	resp := ListSessionsResponse{
		Sessions: sessionInfos,
	}

	writeJSON(w, http.StatusOK, resp)
}

// ErrorResponse represents an error response
type ErrorResponse struct {
	Error string `json:"error"`
}

// writeJSON writes a JSON response with the given status code and data
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// writeError writes a JSON error response with the given status code and message
func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(ErrorResponse{
		Error: message,
	})
}
