package webui

import (
	"encoding/json"
	"net/http"

	"github.com/ziloss-tech/zbot/internal/agent"
	"github.com/ziloss-tech/zbot/internal/crawler"
)

// ─── Request/Response Types ─────────────────────────────────────────────────

type crawlerStartReq struct {
	ViewportW int `json:"viewport_width"`
	ViewportH int `json:"viewport_height"`
	CellSize  int `json:"cell_size"`
}

type crawlerActionReq struct {
	SessionID string `json:"session_id"`
	URL       string `json:"url,omitempty"`
	GridCell  string `json:"grid_cell,omitempty"`
	Text      string `json:"text,omitempty"`
	Direction string `json:"direction,omitempty"`
	Amount    int    `json:"amount,omitempty"`
}

// ─── Handlers ───────────────────────────────────────────────────────────────

func (s *Server) handleCrawlerStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var req crawlerStartReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.ViewportW <= 0 {
		req.ViewportW = 1280
	}
	if req.ViewportH <= 0 {
		req.ViewportH = 960
	}
	if req.CellSize <= 0 {
		req.CellSize = 64
	}

	// Wire crawl events into the ZBOT event bus.
	var emitter crawler.EventEmitter
	if s.eventBus != nil {
		emitter = func(ev crawler.CrawlEvent) {
			s.eventBus.Emit(r.Context(), agentEventFromCrawl(ev))
		}
	}

	c, err := s.crawlerSessions.Start(req.ViewportW, req.ViewportH, req.CellSize, emitter)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(map[string]any{
		"session_id": c.SessionID(),
		"grid":       c.Grid(),
	})
}

func (s *Server) handleCrawlerNavigate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var req crawlerActionReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	c, err := s.crawlerSessions.Get(req.SessionID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if err := c.Navigate(req.URL); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	shot, _ := c.ScreenshotJPEG()
	json.NewEncoder(w).Encode(map[string]any{
		"success":    true,
		"url":        c.CurrentURL(),
		"screenshot": shot,
	})
}

func (s *Server) handleCrawlerClick(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var req crawlerActionReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	c, err := s.crawlerSessions.Get(req.SessionID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	info, clickErr := c.Click(req.GridCell)
	shot, _ := c.ScreenshotJPEG()
	resp := map[string]any{
		"success":    clickErr == nil,
		"screenshot": shot,
		"url":        c.CurrentURL(),
	}
	if info != nil {
		resp["element"] = info
	}
	if clickErr != nil {
		resp["error"] = clickErr.Error()
	}
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleCrawlerType(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var req crawlerActionReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	c, err := s.crawlerSessions.Get(req.SessionID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	typeErr := c.Type(req.Text)
	json.NewEncoder(w).Encode(map[string]any{
		"success": typeErr == nil,
	})
}

func (s *Server) handleCrawlerScroll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var req crawlerActionReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	c, err := s.crawlerSessions.Get(req.SessionID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if req.Amount <= 0 {
		req.Amount = 3
	}
	scrollErr := c.Scroll(req.Direction, req.Amount)
	shot, _ := c.ScreenshotJPEG()
	json.NewEncoder(w).Encode(map[string]any{
		"success":    scrollErr == nil,
		"screenshot": shot,
	})
}

func (s *Server) handleCrawlerScreenshot(w http.ResponseWriter, r *http.Request) {
	sid := r.URL.Query().Get("session_id")
	c, err := s.crawlerSessions.Get(sid)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	shot, err := c.ScreenshotJPEG()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(map[string]any{"screenshot": shot})
}

func (s *Server) handleCrawlerLog(w http.ResponseWriter, r *http.Request) {
	sid := r.URL.Query().Get("session_id")
	c, err := s.crawlerSessions.Get(sid)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	n := 50
	entries := c.Logger().Tail(n)
	json.NewEncoder(w).Encode(entries)
}

func (s *Server) handleCrawlerStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var req crawlerActionReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.crawlerSessions.Stop(req.SessionID); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(map[string]any{"stopped": true})
}

func (s *Server) handleCrawlerSessions(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(s.crawlerSessions.List())
}

// agentEventFromCrawl converts a CrawlEvent to an AgentEvent for the event bus.
func agentEventFromCrawl(ev crawler.CrawlEvent) agent.AgentEvent {
	detail := map[string]any{
		"action":     ev.Action,
		"grid_cell":  ev.GridCell,
		"url":        ev.URL,
		"status":     string(ev.Status),
		"duration_ms": ev.DurationMs,
		"page_title": ev.PageTitle,
	}
	if ev.ElementInfo != nil {
		detail["element_tag"] = ev.ElementInfo.Tag
		detail["element_text"] = ev.ElementInfo.Text
	}
	if ev.Screenshot != "" {
		detail["has_screenshot"] = true
		// Don't put base64 in the event bus detail — too large.
		// The frontend gets screenshots via SSE crawl_screenshot events.
	}
	return agent.AgentEvent{
		SessionID: ev.SessionID,
		Type:      agent.EventType(ev.Type),
		Summary:   crawlSummary(ev),
		Detail:    detail,
		Timestamp: ev.Timestamp,
	}
}

func crawlSummary(ev crawler.CrawlEvent) string {
	switch ev.Action {
	case "navigate":
		return "Navigated to " + ev.URL
	case "click":
		tag := ""
		if ev.ElementInfo != nil {
			tag = " <" + ev.ElementInfo.Tag + ">"
			if ev.ElementInfo.Text != "" {
				tag += " \"" + ev.ElementInfo.Text + "\""
			}
		}
		return "Clicked " + ev.GridCell + tag
	case "type":
		return "Typed text"
	case "scroll":
		return "Scrolled page"
	default:
		return string(ev.Type)
	}
}
