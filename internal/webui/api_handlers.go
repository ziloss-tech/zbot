package webui

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zbot-ai/zbot/internal/research"
	"github.com/zbot-ai/zbot/internal/scheduler"
)

// ─── SSE STREAM ──────────────────────────────────────────────────────────────

// handleSSEStream serves Server-Sent Events for a workflow.
// GET /api/stream/:workflowID
func (s *Server) handleSSEStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	workflowID := strings.TrimPrefix(r.URL.Path, "/api/stream/")
	if workflowID == "" {
		http.Error(w, "missing workflow_id", http.StatusBadRequest)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	flusher.Flush()

	// Replay last 100 events from Postgres on reconnect.
	s.replayEvents(r.Context(), w, flusher, workflowID)

	// Subscribe to live events.
	ch, unsub := s.hub.Subscribe(workflowID)
	defer unsub()

	ping := time.NewTicker(15 * time.Second)
	defer ping.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case evt, ok := <-ch:
			if !ok {
				return
			}
			data, _ := json.Marshal(evt)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		case <-ping.C:
			fmt.Fprintf(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}

// replayEvents sends stored stream events from Postgres for SSE reconnect.
func (s *Server) replayEvents(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, workflowID string) {
	// Guard: skip replay if no Postgres connection.
	if s.db == nil {
		return
	}
	rows, err := s.db.Query(ctx,
		`SELECT workflow_id, COALESCE(task_id, ''), source, event_type, payload
		 FROM zbot_stream_events
		 WHERE workflow_id = $1
		 ORDER BY id ASC
		 LIMIT 100`,
		workflowID,
	)
	if err != nil {
		s.logger.Warn("replay events query failed", "workflow_id", workflowID, "err", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var evt Event
		if err := rows.Scan(&evt.WorkflowID, &evt.TaskID, &evt.Source, &evt.Type, &evt.Payload); err != nil {
			continue
		}
		data, _ := json.Marshal(evt)
		fmt.Fprintf(w, "data: %s\n\n", data)
	}
	flusher.Flush()
}

// ─── POST /api/plan ──────────────────────────────────────────────────────────

type planRequest struct {
	Goal string `json:"goal"`
}

type planResponse struct {
	WorkflowID string `json:"workflow_id"`
	Status     string `json:"status"`
}

// handlePlanAPI accepts a goal and kicks off streaming planning + execution.
func (s *Server) handlePlanAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.WriteHeader(http.StatusOK)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// v2: Single-brain — orchestrator handles both planning and execution.
	if s.orch == nil {
		http.Error(w, "orchestrator not available", http.StatusServiceUnavailable)
		return
	}

	var req planRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Goal == "" {
		http.Error(w, "goal is required", http.StatusBadRequest)
		return
	}

	// Generate workflow ID immediately so client can subscribe to SSE.
	workflowID := generateWorkflowID()

	// Return workflow ID immediately.
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(planResponse{
		WorkflowID: workflowID,
		Status:     "planning",
	})

	// Launch background goroutine: stream plan → submit to orchestrator.
	go s.runPlanAndExecute(workflowID, req.Goal)
}

// runPlanAndExecute uses the orchestrator's built-in decomposition (single-brain v2)
// to plan and execute the goal. Claude does both planning and execution.
func (s *Server) runPlanAndExecute(workflowID, goal string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// v2: Single-brain — use orchestrator.Submit() which asks Claude to decompose
	// the goal into tasks, then executes them. No separate GPT-4o planner.
	planningEvt := Event{
		WorkflowID: workflowID,
		Source:     "agent",
		Type:       "status",
		Payload:    "decomposing goal into tasks...",
	}
	s.hub.Publish(planningEvt)
	s.persistEvent(ctx, planningEvt)

	wfID, submitErr := s.orch.Submit(ctx, "webui-"+workflowID, goal)
	if submitErr != nil {
		errEvt := Event{
			WorkflowID: workflowID,
			Source:     "agent",
			Type:       "error",
			Payload:    "submit failed: " + submitErr.Error(),
		}
		s.hub.Publish(errEvt)
		s.persistEvent(ctx, errEvt)
		s.logger.Error("plan submit failed", "workflow_id", workflowID, "err", submitErr)
		return
	}

	// Publish plan complete event.
	completeEvt := Event{
		WorkflowID: workflowID,
		Source:     "agent",
		Type:       "complete",
		Payload:    fmt.Sprintf("workflow %s submitted", wfID),
	}
	s.hub.Publish(completeEvt)
	s.persistEvent(ctx, completeEvt)

	// Store the goal on the workflow for display.
	s.storeWorkflowGoal(ctx, wfID, goal)

	// Publish handoff event — triggers the animation in the UI.
	handoffEvt := Event{
		WorkflowID: workflowID,
		Source:     "planner",
		Type:       "handoff",
		Payload:    wfID,
	}
	s.hub.Publish(handoffEvt)
	s.persistEvent(ctx, handoffEvt)

	s.logger.Info("plan submitted to orchestrator",
		"display_id", workflowID,
		"workflow_id", wfID,
	)
}

// ─── GET /api/workflows/list ─────────────────────────────────────────────────

type workflowListItem struct {
	ID        string `json:"id"`
	Goal      string `json:"goal"`
	Status    string `json:"status"`
	TaskCount int    `json:"task_count"`
	DoneCount int    `json:"done_count"`
	CreatedAt string `json:"created_at"`
}

func (s *Server) handleWorkflowsAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	rows, err := s.db.Query(r.Context(),
		`SELECT w.id, COALESCE(w.goal, ''), w.status, w.created_at,
		        COUNT(t.id) as task_count,
		        COUNT(CASE WHEN t.status = 'done' THEN 1 END) as done_count
		 FROM zbot_workflows w
		 LEFT JOIN zbot_tasks t ON t.workflow_id = w.id
		 GROUP BY w.id, w.goal, w.status, w.created_at
		 ORDER BY w.created_at DESC LIMIT 20`)
	if err != nil {
		http.Error(w, "db error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var items []workflowListItem
	for rows.Next() {
		var item workflowListItem
		var created time.Time
		if err := rows.Scan(&item.ID, &item.Goal, &item.Status, &created, &item.TaskCount, &item.DoneCount); err != nil {
			continue
		}
		item.CreatedAt = created.Format(time.RFC3339)
		items = append(items, item)
	}

	if items == nil {
		items = []workflowListItem{}
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(items)
}

// ─── GET /api/workflow/:id ───────────────────────────────────────────────────

type workflowDetail struct {
	ID     string       `json:"id"`
	Goal   string       `json:"goal"`
	Status string       `json:"status"`
	Tasks  []taskDetail `json:"tasks"`
}

type taskDetail struct {
	ID          string   `json:"id"`
	Step        int      `json:"step"`
	Name        string   `json:"name"`
	Status      string   `json:"status"`
	Output      string   `json:"output"`
	Error       string   `json:"error"`
	DependsOn   []string `json:"depends_on"`
	OutputFiles []string `json:"output_files,omitempty"` // Sprint 13
	StartedAt   *string  `json:"started_at,omitempty"`
	FinishedAt  *string  `json:"finished_at,omitempty"`
}

func (s *Server) handleWorkflowDetailAPI(w http.ResponseWriter, r *http.Request) {
	// Sprint 13: route /api/workflow/:id/files to workflow files handler.
	path := strings.TrimPrefix(r.URL.Path, "/api/workflow/")
	if strings.HasSuffix(path, "/files") {
		s.handleWorkflowFilesAPI(w, r)
		return
	}

	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	wfID := path
	if wfID == "" {
		http.Error(w, "missing workflow id", http.StatusBadRequest)
		return
	}

	var detail workflowDetail
	err := s.db.QueryRow(r.Context(),
		`SELECT id, COALESCE(goal, ''), status FROM zbot_workflows WHERE id = $1`, wfID).
		Scan(&detail.ID, &detail.Goal, &detail.Status)
	if err != nil {
		http.Error(w, "workflow not found", http.StatusNotFound)
		return
	}

	rows, err := s.db.Query(r.Context(),
		`SELECT id, step, name, status, COALESCE(output, ''), COALESCE(error_msg, ''),
		        depends_on, COALESCE(output_files, '{}'), started_at, finished_at
		 FROM zbot_tasks WHERE workflow_id = $1 ORDER BY step ASC`, wfID)
	if err != nil {
		http.Error(w, "db error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var td taskDetail
		var deps []string
		var outputFiles []string
		var startedAt, finishedAt *time.Time
		if err := rows.Scan(&td.ID, &td.Step, &td.Name, &td.Status, &td.Output, &td.Error,
			&deps, &outputFiles, &startedAt, &finishedAt); err != nil {
			continue
		}
		td.DependsOn = deps
		if td.DependsOn == nil {
			td.DependsOn = []string{}
		}
		td.OutputFiles = outputFiles
		if td.OutputFiles == nil {
			td.OutputFiles = []string{}
		}
		if startedAt != nil {
			ts := startedAt.Format(time.RFC3339)
			td.StartedAt = &ts
		}
		if finishedAt != nil {
			ts := finishedAt.Format(time.RFC3339)
			td.FinishedAt = &ts
		}
		detail.Tasks = append(detail.Tasks, td)
	}

	if detail.Tasks == nil {
		detail.Tasks = []taskDetail{}
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(detail)
}

// ─── GET /api/metrics ────────────────────────────────────────────────────────

func (s *Server) handleMetricsAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	metrics := map[string]any{}

	// Guard: return empty metrics if no Postgres connection.
	if s.db == nil {
		metrics["active_workflows"] = 0
		metrics["total_tasks"] = 0
		metrics["done_tasks"] = 0
		metrics["tokens_today"] = 0
		metrics["cost_today"] = "$0.00"
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		json.NewEncoder(w).Encode(metrics)
		return
	}

	var activeCount int
	s.db.QueryRow(r.Context(),
		`SELECT COUNT(*) FROM zbot_workflows WHERE status = 'running'`).
		Scan(&activeCount)
	metrics["active_workflows"] = activeCount

	var totalTasks, doneTasks int
	s.db.QueryRow(r.Context(),
		`SELECT COUNT(*), COUNT(CASE WHEN status = 'done' THEN 1 END) FROM zbot_tasks`).
		Scan(&totalTasks, &doneTasks)
	metrics["total_tasks"] = totalTasks
	metrics["done_tasks"] = doneTasks

	var todayInput, todayOutput int
	s.db.QueryRow(r.Context(),
		`SELECT COALESCE(SUM(input_tokens),0), COALESCE(SUM(output_tokens),0)
		 FROM zbot_audit_model_calls WHERE created_at >= CURRENT_DATE`).
		Scan(&todayInput, &todayOutput)
	metrics["tokens_today"] = todayInput + todayOutput
	cost := float64(todayInput)*0.000003 + float64(todayOutput)*0.000015
	metrics["cost_today"] = fmt.Sprintf("$%.2f", cost)

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(metrics)
}

// ─── GET /api/file ──────────────────────────────────────────────────────────

// handleFilePreview serves a file from ~/zbot-workspace/ for the output preview drawer.
// Only files inside the workspace are allowed — prevents path traversal.
func (s *Server) handleFilePreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "missing path parameter", http.StatusBadRequest)
		return
	}

	// Security: only allow files under ~/zbot-workspace/
	if !strings.HasPrefix(path, "~/zbot-workspace/") {
		http.Error(w, "access denied: only ~/zbot-workspace/ files are allowed", http.StatusForbidden)
		return
	}

	// Expand ~ to home directory.
	home, err := os.UserHomeDir()
	if err != nil {
		http.Error(w, "cannot resolve home directory", http.StatusInternalServerError)
		return
	}
	absPath := strings.Replace(path, "~", home, 1)

	// Prevent path traversal.
	absPath = filepath.Clean(absPath)
	workspaceRoot := filepath.Join(home, "zbot-workspace")
	if !strings.HasPrefix(absPath, workspaceRoot) {
		http.Error(w, "access denied: path traversal detected", http.StatusForbidden)
		return
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		http.Error(w, "file not found: "+err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Write(data)
}

// ─── HELPERS ─────────────────────────────────────────────────────────────────

// persistEvent writes an SSE event to Postgres for replay on reconnect.
func (s *Server) persistEvent(ctx context.Context, evt Event) {
	_, err := s.db.Exec(ctx,
		`INSERT INTO zbot_stream_events (workflow_id, task_id, source, event_type, payload)
		 VALUES ($1, $2, $3, $4, $5)`,
		evt.WorkflowID, nullIfEmpty(evt.TaskID), evt.Source, evt.Type, evt.Payload,
	)
	if err != nil {
		s.logger.Warn("persist event failed", "err", err)
	}
}

// storeWorkflowGoal saves the goal on the workflow for display.
func (s *Server) storeWorkflowGoal(ctx context.Context, dbWorkflowID, goal string) {
	_, _ = s.db.Exec(ctx,
		`UPDATE zbot_workflows SET goal = $2 WHERE id = $1`,
		dbWorkflowID, goal,
	)
}

func generateWorkflowID() string {
	b := make([]byte, 6)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// ─── Sprint 12: MEMORY PANEL API ─────────────────────────────────────────────

type memoryItem struct {
	ID        string   `json:"id"`
	Content   string   `json:"content"`
	Source    string   `json:"source"`
	Tags      []string `json:"tags"`
	CreatedAt string   `json:"created_at"`
	Score     float32  `json:"score,omitempty"`
}

type memoriesResponse struct {
	Total    int64        `json:"total"`
	Memories []memoryItem `json:"memories"`
}

// handleMemoriesAPI handles GET /api/memories?q={query}&limit=20
func (s *Server) handleMemoriesAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.memStore == nil {
		http.Error(w, "memory store not available", http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	query := r.URL.Query().Get("q")
	limitStr := r.URL.Query().Get("limit")
	limit := 20
	if limitStr != "" {
		if l := parseInt(limitStr); l > 0 && l <= 100 {
			limit = l
		}
	}

	var resp memoriesResponse

	if query != "" {
		// Search mode.
		facts, err := s.memStore.Search(r.Context(), query, limit)
		if err != nil {
			http.Error(w, "search error: "+err.Error(), http.StatusInternalServerError)
			return
		}
		for _, f := range facts {
			resp.Memories = append(resp.Memories, memoryItem{
				ID:        f.ID,
				Content:   f.Content,
				Source:    f.Source,
				Tags:      f.Tags,
				CreatedAt: f.CreatedAt.Format(time.RFC3339),
				Score:     f.Score,
			})
		}
		resp.Total = int64(len(facts))
	} else {
		// List mode — show recent memories.
		// Use direct DB query since List is on the concrete Store, not the interface.
		rows, err := s.db.Query(r.Context(),
			`SELECT id, content, source, tags, created_at
			 FROM zbot_memories ORDER BY created_at DESC LIMIT $1`, limit)
		if err != nil {
			http.Error(w, "db error: "+err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		for rows.Next() {
			var m memoryItem
			var tags []string
			var created time.Time
			if err := rows.Scan(&m.ID, &m.Content, &m.Source, &tags, &created); err != nil {
				continue
			}
			m.Tags = tags
			if m.Tags == nil {
				m.Tags = []string{}
			}
			m.CreatedAt = created.Format(time.RFC3339)
			resp.Memories = append(resp.Memories, m)
		}

		// Get total count.
		var count int64
		s.db.QueryRow(r.Context(), `SELECT COUNT(*) FROM zbot_memories`).Scan(&count)
		resp.Total = count
	}

	if resp.Memories == nil {
		resp.Memories = []memoryItem{}
	}

	json.NewEncoder(w).Encode(resp)
}

// handleMemoryDeleteAPI handles DELETE /api/memory/:id
func (s *Server) handleMemoryDeleteAPI(w http.ResponseWriter, r *http.Request) {
	// Handle CORS preflight.
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.WriteHeader(http.StatusOK)
		return
	}
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.memStore == nil {
		http.Error(w, "memory store not available", http.StatusServiceUnavailable)
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/api/memory/")
	if id == "" {
		http.Error(w, "missing memory id", http.StatusBadRequest)
		return
	}

	if err := s.memStore.Delete(r.Context(), id); err != nil {
		http.Error(w, "delete error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(map[string]string{"status": "deleted", "id": id})
}

// ─── Sprint 12: QUICK CHAT API ──────────────────────────────────────────────

type chatRequest struct {
	Message string `json:"message"`
}

type chatResponse struct {
	Reply string `json:"reply"`
}

// handleQuickChatAPI handles POST /api/chat — memory-aware quick chat.
func (s *Server) handleQuickChatAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.WriteHeader(http.StatusOK)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.quickChat == nil {
		http.Error(w, "quick chat not available", http.StatusServiceUnavailable)
		return
	}

	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Message == "" {
		http.Error(w, "message is required", http.StatusBadRequest)
		return
	}

	reply, err := s.quickChat(r.Context(), req.Message)
	if err != nil {
		http.Error(w, "chat error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(chatResponse{Reply: reply})
}

// ─── Sprint 13: WORKSPACE FILE PANEL API ──────────────────────────────────

// WorkspaceFile represents a file in the workspace.
type WorkspaceFile struct {
	Name       string `json:"name"`
	Path       string `json:"path"`       // relative to workspace root
	Size       int64  `json:"size"`
	SizeHuman  string `json:"size_human"`  // "12 KB", "1.2 MB"
	Extension  string `json:"extension"`   // "md", "csv", "json", "py", "txt"
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
	WorkflowID string `json:"workflow_id,omitempty"` // if file was created by a workflow
}

type workspaceResponse struct {
	Files         []WorkspaceFile `json:"files"`
	Total         int             `json:"total"`
	WorkspacePath string          `json:"workspace_path"`
}

// handleWorkspaceAPI handles GET /api/workspace
// Query params: ?ext=md&sort=newest&limit=50
func (s *Server) handleWorkspaceAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	extFilter := r.URL.Query().Get("ext")
	sortOrder := r.URL.Query().Get("sort")
	if sortOrder == "" {
		sortOrder = "newest"
	}
	limit := 50
	if l := parseInt(r.URL.Query().Get("limit")); l > 0 && l <= 200 {
		limit = l
	}

	home, err := os.UserHomeDir()
	if err != nil {
		http.Error(w, "cannot resolve home directory", http.StatusInternalServerError)
		return
	}
	wsRoot := filepath.Join(home, "zbot-workspace")

	var files []WorkspaceFile
	_ = filepath.Walk(wsRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip errors
		}
		// Skip hidden files and directories.
		name := info.Name()
		if strings.HasPrefix(name, ".") {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info.IsDir() {
			return nil
		}

		relPath, _ := filepath.Rel(wsRoot, path)
		ext := strings.TrimPrefix(filepath.Ext(name), ".")

		// Extension filter.
		if extFilter != "" && ext != extFilter {
			return nil
		}

		files = append(files, WorkspaceFile{
			Name:      name,
			Path:      relPath,
			Size:      info.Size(),
			SizeHuman: humanSize(info.Size()),
			Extension: ext,
			CreatedAt: info.ModTime().Format(time.RFC3339),
			UpdatedAt: info.ModTime().Format(time.RFC3339),
		})
		return nil
	})

	// Sort.
	switch sortOrder {
	case "oldest":
		sortWorkspaceFiles(files, func(a, b WorkspaceFile) bool { return a.UpdatedAt < b.UpdatedAt })
	case "largest":
		sortWorkspaceFiles(files, func(a, b WorkspaceFile) bool { return a.Size > b.Size })
	default: // newest
		sortWorkspaceFiles(files, func(a, b WorkspaceFile) bool { return a.UpdatedAt > b.UpdatedAt })
	}

	total := len(files)
	if len(files) > limit {
		files = files[:limit]
	}
	if files == nil {
		files = []WorkspaceFile{}
	}

	json.NewEncoder(w).Encode(workspaceResponse{
		Files:         files,
		Total:         total,
		WorkspacePath: "~/zbot-workspace",
	})
}

// handleWorkspaceDownloadAPI handles GET /api/workspace/download?path={relative_path}
func (s *Server) handleWorkspaceDownloadAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	relPath := r.URL.Query().Get("path")
	if relPath == "" {
		http.Error(w, "missing path parameter", http.StatusBadRequest)
		return
	}

	// Security: reject path traversal.
	if strings.Contains(relPath, "..") || strings.HasPrefix(relPath, "/") {
		http.Error(w, "access denied: invalid path", http.StatusForbidden)
		return
	}

	home, err := os.UserHomeDir()
	if err != nil {
		http.Error(w, "cannot resolve home directory", http.StatusInternalServerError)
		return
	}
	wsRoot := filepath.Join(home, "zbot-workspace")
	absPath := filepath.Clean(filepath.Join(wsRoot, relPath))
	if !strings.HasPrefix(absPath, wsRoot) {
		http.Error(w, "access denied: path traversal detected", http.StatusForbidden)
		return
	}

	info, err := os.Stat(absPath)
	if err != nil {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}

	// Set Content-Type based on extension.
	ext := strings.ToLower(filepath.Ext(absPath))
	contentType := "application/octet-stream"
	switch ext {
	case ".md", ".txt":
		contentType = "text/plain; charset=utf-8"
	case ".csv":
		contentType = "text/csv; charset=utf-8"
	case ".json":
		contentType = "application/json; charset=utf-8"
	case ".py", ".go", ".js", ".ts", ".sh":
		contentType = "text/plain; charset=utf-8"
	case ".html":
		contentType = "text/html; charset=utf-8"
	case ".pdf":
		contentType = "application/pdf"
	case ".png":
		contentType = "image/png"
	case ".jpg", ".jpeg":
		contentType = "image/jpeg"
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename=%q`, filepath.Base(absPath)))
	w.Header().Set("Content-Length", fmt.Sprintf("%d", info.Size()))
	w.Header().Set("Access-Control-Allow-Origin", "*")

	http.ServeFile(w, r, absPath)
}

// handleWorkspaceDeleteAPI handles DELETE /api/workspace/file?path={relative_path}
func (s *Server) handleWorkspaceDeleteAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.WriteHeader(http.StatusOK)
		return
	}
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	relPath := r.URL.Query().Get("path")
	if relPath == "" {
		http.Error(w, "missing path parameter", http.StatusBadRequest)
		return
	}

	if strings.Contains(relPath, "..") || strings.HasPrefix(relPath, "/") {
		http.Error(w, "access denied: invalid path", http.StatusForbidden)
		return
	}

	home, err := os.UserHomeDir()
	if err != nil {
		http.Error(w, "cannot resolve home directory", http.StatusInternalServerError)
		return
	}
	wsRoot := filepath.Join(home, "zbot-workspace")
	absPath := filepath.Clean(filepath.Join(wsRoot, relPath))
	if !strings.HasPrefix(absPath, wsRoot) {
		http.Error(w, "access denied: path traversal detected", http.StatusForbidden)
		return
	}

	if err := os.Remove(absPath); err != nil {
		http.Error(w, "delete error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusNoContent)
}

// handleWorkspacePreviewAPI handles GET /api/workspace/preview?path={relative_path}
// Returns file contents as text (max 50KB).
func (s *Server) handleWorkspacePreviewAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	relPath := r.URL.Query().Get("path")
	if relPath == "" {
		http.Error(w, "missing path parameter", http.StatusBadRequest)
		return
	}

	if strings.Contains(relPath, "..") || strings.HasPrefix(relPath, "/") {
		http.Error(w, "access denied: invalid path", http.StatusForbidden)
		return
	}

	home, err := os.UserHomeDir()
	if err != nil {
		http.Error(w, "cannot resolve home directory", http.StatusInternalServerError)
		return
	}
	wsRoot := filepath.Join(home, "zbot-workspace")
	absPath := filepath.Clean(filepath.Join(wsRoot, relPath))
	if !strings.HasPrefix(absPath, wsRoot) {
		http.Error(w, "access denied: path traversal detected", http.StatusForbidden)
		return
	}

	// Check if binary.
	ext := strings.ToLower(filepath.Ext(absPath))
	binaryExts := map[string]bool{
		".png": true, ".jpg": true, ".jpeg": true, ".gif": true,
		".pdf": true, ".zip": true, ".tar": true, ".gz": true,
		".exe": true, ".bin": true, ".so": true, ".dll": true,
	}
	if binaryExts[ext] {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		json.NewEncoder(w).Encode(map[string]string{
			"error": "binary file — download only",
		})
		return
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		http.Error(w, "file not found: "+err.Error(), http.StatusNotFound)
		return
	}

	const maxPreview = 50 * 1024 // 50KB
	truncated := false
	if len(data) > maxPreview {
		data = data[:maxPreview]
		truncated = true
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Write(data)
	if truncated {
		w.Write([]byte("\n\n[TRUNCATED — file exceeds 50KB preview limit]"))
	}
}

// handleWorkflowFilesAPI handles GET /api/workflow/:id/files
// Returns files created during a specific workflow.
func (s *Server) handleWorkflowFilesAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	wfID := strings.TrimPrefix(r.URL.Path, "/api/workflow/")
	wfID = strings.TrimSuffix(wfID, "/files")
	if wfID == "" {
		http.Error(w, "missing workflow id", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Query output_files from tasks in this workflow.
	rows, err := s.db.Query(r.Context(),
		`SELECT COALESCE(output_files, '{}') FROM zbot_tasks WHERE workflow_id = $1 AND output_files IS NOT NULL`, wfID)
	if err != nil {
		http.Error(w, "db error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var allFiles []string
	for rows.Next() {
		var files []string
		if err := rows.Scan(&files); err != nil {
			continue
		}
		allFiles = append(allFiles, files...)
	}

	if allFiles == nil {
		allFiles = []string{}
	}

	json.NewEncoder(w).Encode(map[string]any{
		"workflow_id": wfID,
		"files":       allFiles,
	})
}

// humanSize formats bytes into human-readable string.
func humanSize(b int64) string {
	switch {
	case b < 1024:
		return fmt.Sprintf("%d B", b)
	case b < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(b)/1024)
	case b < 1024*1024*1024:
		return fmt.Sprintf("%.1f MB", float64(b)/(1024*1024))
	default:
		return fmt.Sprintf("%.1f GB", float64(b)/(1024*1024*1024))
	}
}

// sortWorkspaceFiles sorts files with a custom less function.
func sortWorkspaceFiles(files []WorkspaceFile, less func(a, b WorkspaceFile) bool) {
	for i := 1; i < len(files); i++ {
		for j := i; j > 0 && less(files[j], files[j-1]); j-- {
			files[j], files[j-1] = files[j-1], files[j]
		}
	}
}

// parseInt parses an integer from a string, returning 0 on failure.
func parseInt(s string) int {
	n := 0
	for _, c := range s {
		if c >= '0' && c <= '9' {
			n = n*10 + int(c-'0')
		} else {
			return 0
		}
	}
	return n
}

// ─── SPRINT 14: SCHEDULE API ────────────────────────────────────────────────

type scheduleCreateRequest struct {
	Name            string `json:"name"`
	Goal            string `json:"goal"`
	NaturalSchedule string `json:"natural_schedule"`
}

type scheduleJobResponse struct {
	ID              string  `json:"id"`
	Name            string  `json:"name"`
	Goal            string  `json:"goal"`
	CronExpr        string  `json:"cron_expr"`
	NaturalSchedule string  `json:"natural_schedule"`
	Status          string  `json:"status"`
	NextRun         string  `json:"next_run"`
	LastRun         *string `json:"last_run"`
	RunCount        int     `json:"run_count"`
	CreatedAt       string  `json:"created_at"`
}

func jobToResponse(j scheduler.Job) scheduleJobResponse {
	resp := scheduleJobResponse{
		ID:              j.ID,
		Name:            j.Name,
		Goal:            j.Goal,
		CronExpr:        j.CronExpr,
		NaturalSchedule: j.NaturalSchedule,
		Status:          j.Status,
		NextRun:         j.NextRun.Format(time.RFC3339),
		RunCount:        j.RunCount,
		CreatedAt:       j.CreatedAt.Format(time.RFC3339),
	}
	if j.LastRun != nil {
		lr := j.LastRun.Format(time.RFC3339)
		resp.LastRun = &lr
	}
	if resp.Status == "" {
		if j.Active {
			resp.Status = "active"
		} else {
			resp.Status = "paused"
		}
	}
	return resp
}

// POST /api/schedule — create a new scheduled job.
func (s *Server) handleScheduleCreateAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.sched == nil || s.jobStore == nil {
		http.Error(w, "scheduler not available", http.StatusServiceUnavailable)
		return
	}

	var req scheduleCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Goal == "" {
		http.Error(w, "goal is required", http.StatusBadRequest)
		return
	}
	if req.NaturalSchedule == "" {
		http.Error(w, "natural_schedule is required", http.StatusBadRequest)
		return
	}

	// Parse natural language schedule to cron expression.
	if s.llmClient == nil {
		http.Error(w, "LLM not available for schedule parsing", http.StatusServiceUnavailable)
		return
	}
	cronExpr, err := scheduler.ParseSchedule(r.Context(), s.llmClient, req.NaturalSchedule)
	if err != nil {
		http.Error(w, "failed to parse schedule: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Compute next run.
	nextRun, err := scheduler.NextCronTimeFromExpr(cronExpr, time.Now())
	if err != nil {
		http.Error(w, "invalid cron: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Generate ID.
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	id := hex.EncodeToString(b)

	name := req.Name
	if name == "" {
		// Auto-generate name from goal.
		name = req.Goal
		if len(name) > 60 {
			name = name[:60] + "..."
		}
	}

	job := scheduler.Job{
		ID:              id,
		Name:            name,
		Goal:            req.Goal,
		CronExpr:        cronExpr,
		NaturalSchedule: req.NaturalSchedule,
		Instruction:     req.Goal,
		Status:          "active",
		NextRun:         nextRun,
		Active:          true,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}

	if err := s.sched.Add(r.Context(), job); err != nil {
		http.Error(w, "failed to create job: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(jobToResponse(job))
}

// GET /api/schedules — list all scheduled jobs.
func (s *Server) handleScheduleListAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.jobStore == nil {
		http.Error(w, "scheduler not available", http.StatusServiceUnavailable)
		return
	}

	jobs, err := s.jobStore.LoadAll(r.Context())
	if err != nil {
		http.Error(w, "failed to load jobs: "+err.Error(), http.StatusInternalServerError)
		return
	}

	result := make([]scheduleJobResponse, 0, len(jobs))
	for _, j := range jobs {
		result = append(result, jobToResponse(j))
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(result)
}

// handleScheduleActionAPI routes schedule sub-actions:
// PUT  /api/schedule/:id/pause
// PUT  /api/schedule/:id/resume
// DELETE /api/schedule/:id
// POST /api/schedule/:id/run
func (s *Server) handleScheduleActionAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "PUT, DELETE, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if s.sched == nil || s.jobStore == nil {
		http.Error(w, "scheduler not available", http.StatusServiceUnavailable)
		return
	}

	// Parse: /api/schedule/:id or /api/schedule/:id/action
	path := strings.TrimPrefix(r.URL.Path, "/api/schedule/")
	parts := strings.SplitN(path, "/", 2)
	id := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	if id == "" {
		http.Error(w, "missing job ID", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	switch {
	case r.Method == http.MethodPut && action == "pause":
		if err := s.sched.Pause(r.Context(), id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "paused"})

	case r.Method == http.MethodPut && action == "resume":
		if err := s.sched.Resume(r.Context(), id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "active"})

	case r.Method == http.MethodDelete:
		if err := s.sched.Remove(r.Context(), id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})

	case r.Method == http.MethodPost && action == "run":
		// Run now — fire the job immediately.
		job, ok := s.sched.Get(id)
		if !ok {
			http.Error(w, "job not found", http.StatusNotFound)
			return
		}
		// Fire in background via the scheduler's handler.
		go func() {
			ctx := context.Background()
			instruction := job.Goal
			if instruction == "" {
				instruction = job.Instruction
			}
			// Mark running.
			_ = s.jobStore.UpdateStatus(ctx, id, "running")

			// Use the scheduler's handler directly.
			s.sched.FireNow(ctx, job)
		}()
		json.NewEncoder(w).Encode(map[string]string{"status": "triggered"})

	default:
		http.Error(w, "unknown action", http.StatusBadRequest)
	}
}

// ─── SPRINT 14: MONITOR API ─────────────────────────────────────────────────

type monitorCreateRequest struct {
	Name                 string `json:"name"`
	URL                  string `json:"url"`
	CheckIntervalMinutes int    `json:"check_interval_minutes"`
	NotifyOnChange       string `json:"notify_on_change"`
}

type monitorResponse struct {
	ID                   string `json:"id"`
	Name                 string `json:"name"`
	URL                  string `json:"url"`
	CheckIntervalMinutes int    `json:"check_interval_minutes"`
	NotifyOnChange       string `json:"notify_on_change"`
	Active               bool   `json:"active"`
	CreatedAt            string `json:"created_at"`
}

// POST /api/monitor — start watching a URL.
func (s *Server) handleMonitorCreateAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Need pgJobStore to be a *PGJobStore for monitor methods.
	pgStore, ok := s.jobStore.(*scheduler.PGJobStore)
	if !ok || pgStore == nil {
		http.Error(w, "monitor store not available", http.StatusServiceUnavailable)
		return
	}

	var req monitorCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.URL == "" {
		http.Error(w, "url is required", http.StatusBadRequest)
		return
	}
	if req.CheckIntervalMinutes <= 0 {
		req.CheckIntervalMinutes = 60
	}

	b := make([]byte, 8)
	_, _ = rand.Read(b)
	id := hex.EncodeToString(b)

	entry := scheduler.MonitorEntry{
		ID:                   id,
		Name:                 req.Name,
		URL:                  req.URL,
		CheckIntervalMinutes: req.CheckIntervalMinutes,
		NotifyOnChange:       req.NotifyOnChange,
		Active:               true,
		CreatedAt:            time.Now(),
	}

	if err := pgStore.SaveMonitor(r.Context(), entry); err != nil {
		http.Error(w, "failed to save monitor: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(monitorResponse{
		ID:                   entry.ID,
		Name:                 entry.Name,
		URL:                  entry.URL,
		CheckIntervalMinutes: entry.CheckIntervalMinutes,
		NotifyOnChange:       entry.NotifyOnChange,
		Active:               entry.Active,
		CreatedAt:            entry.CreatedAt.Format(time.RFC3339),
	})
}

// GET /api/monitors — list active monitors.
func (s *Server) handleMonitorListAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	pgStore, ok := s.jobStore.(*scheduler.PGJobStore)
	if !ok || pgStore == nil {
		http.Error(w, "monitor store not available", http.StatusServiceUnavailable)
		return
	}

	monitors, err := pgStore.LoadMonitors(r.Context())
	if err != nil {
		http.Error(w, "failed to load monitors: "+err.Error(), http.StatusInternalServerError)
		return
	}

	result := make([]monitorResponse, 0, len(monitors))
	for _, m := range monitors {
		result = append(result, monitorResponse{
			ID:                   m.ID,
			Name:                 m.Name,
			URL:                  m.URL,
			CheckIntervalMinutes: m.CheckIntervalMinutes,
			NotifyOnChange:       m.NotifyOnChange,
			Active:               m.Active,
			CreatedAt:            m.CreatedAt.Format(time.RFC3339),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(result)
}

// DELETE /api/monitor/:id — stop watching a URL.
func (s *Server) handleMonitorDeleteAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	pgStore, ok := s.jobStore.(*scheduler.PGJobStore)
	if !ok || pgStore == nil {
		http.Error(w, "monitor store not available", http.StatusServiceUnavailable)
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/api/monitor/")
	if id == "" {
		http.Error(w, "missing monitor ID", http.StatusBadRequest)
		return
	}

	if err := pgStore.DeleteMonitor(r.Context(), id); err != nil {
		http.Error(w, "failed to delete monitor: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
}

// ─── DEEP RESEARCH API ──────────────────────────────────────────────────────

type researchCreateRequest struct {
	Goal string `json:"goal"`
}

type researchSessionResponse struct {
	ID              string  `json:"id"`
	Goal            string  `json:"goal"`
	Status          string  `json:"status"`
	Iterations      int     `json:"iterations"`
	ConfidenceScore float64 `json:"confidence_score"`
	FinalReport     string  `json:"final_report,omitempty"`
	CostUSD         float64 `json:"cost_usd"`
	Error           string  `json:"error,omitempty"`
	CreatedAt       string  `json:"created_at"`
	UpdatedAt       string  `json:"updated_at"`
}

func sessionToResponse(s research.ResearchSession) researchSessionResponse {
	return researchSessionResponse{
		ID:              s.ID,
		Goal:            s.Goal,
		Status:          s.Status,
		Iterations:      s.Iterations,
		ConfidenceScore: s.ConfidenceScore,
		FinalReport:     s.FinalReport,
		CostUSD:         s.CostUSD,
		Error:           s.Error,
		CreatedAt:       s.CreatedAt.Format(time.RFC3339),
		UpdatedAt:       s.UpdatedAt.Format(time.RFC3339),
	}
}

// POST /api/research — start a deep research session.
// Returns 202 Accepted immediately — research runs in a background goroutine.
func (s *Server) handleResearchCreateAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.researchOrch == nil {
		http.Error(w, "deep research not available", http.StatusServiceUnavailable)
		return
	}

	var req researchCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Goal == "" {
		http.Error(w, "goal is required", http.StatusBadRequest)
		return
	}

	// Generate session ID.
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	sessionID := "res_" + hex.EncodeToString(b)

	// Persist the session as "running" in Postgres.
	if s.researchStore != nil {
		if err := s.researchStore.CreateSession(r.Context(), sessionID, req.Goal); err != nil {
			s.logger.Error("failed to create research session", "err", err)
			http.Error(w, "failed to create session: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	// Run in background goroutine — returns 202 immediately.
	go func() {
		ctx := context.Background()
		state, err := s.researchOrch.RunDeepResearch(ctx, req.Goal, sessionID)
		if err != nil {
			s.logger.Error("deep research failed",
				"session_id", sessionID,
				"err", err,
			)
			if s.researchStore != nil {
				_ = s.researchStore.FailSession(ctx, sessionID, err.Error())
			}
			return
		}

		if s.researchStore != nil {
			_ = s.researchStore.CompleteSession(ctx, sessionID, state.FinalReport, state)
		}

		s.logger.Info("deep research completed",
			"session_id", sessionID,
			"iterations", state.Iteration,
			"cost", fmt.Sprintf("$%.4f", state.CostUSD),
		)

		// ── Slack notification ───────────────────────────────────────────
		s.logger.Info("research notify check",
			"notifier_nil", s.slackNotifier == nil,
			"channel", s.notifyChannelID,
		)
		if s.slackNotifier != nil && s.notifyChannelID != "" {
			goal := req.Goal
			if len(goal) > 80 {
				goal = goal[:80] + "..."
			}
			msg := fmt.Sprintf("🔬 *Deep Research Complete*\n_%s_\n\n", goal)
			msg += fmt.Sprintf("*Confidence:* %.0f%% | *Iterations:* %d | *Sources:* %d | *Cost:* $%.4f\n\n",
				state.Critique.ConfidenceScore*100,
				state.Iteration,
				len(state.Sources),
				state.CostUSD,
			)
			// Preview: first 400 chars of the final report.
			preview := state.FinalReport
			if len(preview) > 400 {
				preview = preview[:400] + "..."
			}
			msg += preview + "\n\n"
			msg += fmt.Sprintf("<http://localhost:18790|View full report in ZBOT UI>")
			if notifyErr := s.slackNotifier.Send(ctx, s.notifyChannelID, msg); notifyErr != nil {
				s.logger.Error("research Slack notification failed", "err", notifyErr)
			} else {
				s.logger.Info("research Slack notification sent", "channel", s.notifyChannelID)
			}
		}
	}()

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{
		"session_id": sessionID,
		"status":     "running",
		"message":    "Deep research started — use /api/research/stream/" + sessionID + " for live updates.",
	})
}

// GET /api/research/list — list recent research sessions.
func (s *Server) handleResearchListAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.researchStore == nil {
		http.Error(w, "research store not available", http.StatusServiceUnavailable)
		return
	}

	limit := 20
	if l := parseInt(r.URL.Query().Get("limit")); l > 0 && l <= 100 {
		limit = l
	}

	sessions, err := s.researchStore.ListSessions(r.Context(), limit)
	if err != nil {
		http.Error(w, "failed to list sessions: "+err.Error(), http.StatusInternalServerError)
		return
	}

	result := make([]researchSessionResponse, 0, len(sessions))
	for _, sess := range sessions {
		result = append(result, sessionToResponse(sess))
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(result)
}

// GET /api/research/:id — get a single research session detail.
func (s *Server) handleResearchDetailAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.researchStore == nil {
		http.Error(w, "research store not available", http.StatusServiceUnavailable)
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/api/research/")
	// Strip sub-paths like /api/research/:id/anything.
	if idx := strings.Index(id, "/"); idx >= 0 {
		id = id[:idx]
	}
	if id == "" {
		http.Error(w, "missing session ID", http.StatusBadRequest)
		return
	}

	sess, err := s.researchStore.GetSession(r.Context(), id)
	if err != nil {
		http.Error(w, "session not found: "+err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(sessionToResponse(*sess))
}

// GET /api/research/stream/:id — SSE stream for live research progress.
func (s *Server) handleResearchStreamAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.researchOrch == nil {
		http.Error(w, "research not available", http.StatusServiceUnavailable)
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/api/research/stream/")
	if id == "" {
		http.Error(w, "missing session ID", http.StatusBadRequest)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	flusher.Flush()

	// Get the emitter for this session.
	emitter := s.researchOrch.GetEmitter(id)
	if emitter == nil {
		// Session may have already finished — send a done event.
		fmt.Fprintf(w, "data: {\"stage\":\"done\",\"message\":\"Session not active — may have already completed.\"}\n\n")
		flusher.Flush()
		return
	}

	ping := time.NewTicker(15 * time.Second)
	defer ping.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case evt, ok := <-emitter.Events():
			if !ok {
				// Emitter closed — research complete.
				fmt.Fprintf(w, "data: {\"stage\":\"stream_end\"}\n\n")
				flusher.Flush()
				return
			}
			data, _ := json.Marshal(evt)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		case <-ping.C:
			fmt.Fprintf(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

// GET /api/research/budget — today's spend and daily limit.
func (s *Server) handleResearchBudgetAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	resp := map[string]any{
		"daily_limit_usd": research.DailyBudgetUSD,
		"today_spent_usd": 0.0,
		"sessions_today":  0,
		"remaining_usd":   research.DailyBudgetUSD,
	}

	if s.researchStore != nil {
		totalCost, sessionCount, err := s.researchStore.GetDailyStats(r.Context())
		if err == nil {
			resp["today_spent_usd"] = totalCost
			resp["sessions_today"] = sessionCount
			resp["remaining_usd"] = research.DailyBudgetUSD - totalCost
		}
	}

	json.NewEncoder(w).Encode(resp)
}

// ─── Sprint 20: PERSISTENT CLAUDE CHAT API ───────────────────────────────────

// handleClaudeChatAPI handles POST /api/claude/chat
// Saves user message, calls Claude with full history, saves reply, broadcasts SSE.
func (s *Server) handleClaudeChatAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.WriteHeader(http.StatusOK)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.chatStore == nil || s.persistentChat == nil {
		http.Error(w, "chat not configured", http.StatusServiceUnavailable)
		return
	}

	var req struct {
		Message string `json:"message"`
		Source  string `json:"source"` // "ui" | "slack"
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if req.Message == "" {
		http.Error(w, "message required", http.StatusBadRequest)
		return
	}
	if req.Source == "" {
		req.Source = "ui"
	}

	ctx := r.Context()

	// Load conversation history (last 40 messages).
	history, err := s.chatStore.History(ctx, 40)
	if err != nil {
		s.logger.Error("chat history load failed", "err", err)
		history = nil
	}

	// Save user message first.
	userMsg := ChatMessage{
		ID:        newChatID(),
		Role:      "user",
		Content:   req.Message,
		Source:    req.Source,
		CreatedAt: time.Now().UTC(),
	}
	if saveErr := s.chatStore.Save(ctx, userMsg); saveErr != nil {
		s.logger.Error("failed to save user message", "err", saveErr)
	}

	// Broadcast user message via SSE.
	s.hub.Publish(Event{
		WorkflowID: "claude_chat",
		Source:     "user",
		Type:       "chat_message",
		Payload:    chatMsgJSON(userMsg),
	})

	// Call Claude with full history.
	reply, err := s.persistentChat(ctx, history, req.Message)
	if err != nil {
		s.logger.Error("persistent chat failed", "err", err)
		http.Error(w, "claude error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Save assistant reply.
	assistantMsg := ChatMessage{
		ID:        newChatID(),
		Role:      "assistant",
		Content:   reply,
		Source:    "claude",
		CreatedAt: time.Now().UTC(),
	}
	if saveErr := s.chatStore.Save(ctx, assistantMsg); saveErr != nil {
		s.logger.Error("failed to save assistant message", "err", saveErr)
	}

	// Broadcast assistant message via SSE.
	s.hub.Publish(Event{
		WorkflowID: "claude_chat",
		Source:     "claude",
		Type:       "chat_message",
		Payload:    chatMsgJSON(assistantMsg),
	})

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(map[string]any{
		"user_message":  userMsg,
		"reply_message": assistantMsg,
	})
}

// handleClaudeHistoryAPI handles GET /api/claude/history
func (s *Server) handleClaudeHistoryAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	if s.chatStore == nil {
		json.NewEncoder(w).Encode(map[string]any{"messages": []ChatMessage{}})
		return
	}

	limit := 100
	history, err := s.chatStore.History(r.Context(), limit)
	if err != nil {
		s.logger.Error("chat history load failed", "err", err)
		json.NewEncoder(w).Encode(map[string]any{"messages": []ChatMessage{}})
		return
	}
	if history == nil {
		history = []ChatMessage{}
	}
	json.NewEncoder(w).Encode(map[string]any{"messages": history})
}

// handleClaudeStreamAPI handles GET /api/claude/stream
// SSE stream for live chat messages.
func (s *Server) handleClaudeStreamAPI(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	flusher.Flush()

	// Subscribe to claude_chat SSE events.
	ch, unsub := s.hub.Subscribe("claude_chat")
	defer unsub()

	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case evt := <-ch:
			if evt.Type == "chat_message" {
				fmt.Fprintf(w, "data: %s\n\n", evt.Payload)
				flusher.Flush()
			}
		case <-ping.C:
			fmt.Fprintf(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}

// chatMsgJSON serializes a ChatMessage to JSON string for SSE payload.
func chatMsgJSON(msg ChatMessage) string {
	b, _ := json.Marshal(msg)
	return string(b)
}
