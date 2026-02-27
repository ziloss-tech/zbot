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

	"github.com/jeremylerwick-max/zbot/internal/planner"
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

	if s.planner == nil {
		http.Error(w, "planner not available", http.StatusServiceUnavailable)
		return
	}
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

// runPlanAndExecute streams the planner output, persists events, then submits
// the task graph to the orchestrator for Claude execution.
func (s *Server) runPlanAndExecute(workflowID, goal string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	tokens := make(chan string, 128)

	// Stream planner tokens to SSE hub + Postgres.
	go func() {
		for token := range tokens {
			evt := Event{
				WorkflowID: workflowID,
				Source:     "planner",
				Type:       "token",
				Payload:    token,
			}
			s.hub.Publish(evt)
			s.persistEvent(ctx, evt)
		}
	}()

	graph, err := s.planner.PlanStream(ctx, goal, tokens)
	close(tokens)

	if err != nil {
		errEvt := Event{
			WorkflowID: workflowID,
			Source:     "planner",
			Type:       "error",
			Payload:    err.Error(),
		}
		s.hub.Publish(errEvt)
		s.persistEvent(ctx, errEvt)
		s.logger.Error("plan stream failed", "workflow_id", workflowID, "err", err)
		return
	}

	// Publish plan complete event with task summary.
	taskSummary, _ := json.Marshal(graph.Tasks)
	completeEvt := Event{
		WorkflowID: workflowID,
		Source:     "planner",
		Type:       "complete",
		Payload:    string(taskSummary),
	}
	s.hub.Publish(completeEvt)
	s.persistEvent(ctx, completeEvt)

	// Submit to orchestrator — creates the workflow + tasks in Postgres.
	wfID, submitErr := planner.Submit(ctx, s.orch.Store(), graph, "webui-"+workflowID)
	if submitErr != nil {
		errEvt := Event{
			WorkflowID: workflowID,
			Source:     "planner",
			Type:       "error",
			Payload:    "submit failed: " + submitErr.Error(),
		}
		s.hub.Publish(errEvt)
		s.persistEvent(ctx, errEvt)
		s.logger.Error("plan submit failed", "workflow_id", workflowID, "err", submitErr)
		return
	}

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
		"tasks", len(graph.Tasks),
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
