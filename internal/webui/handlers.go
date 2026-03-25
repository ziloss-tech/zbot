package webui

import (
	"encoding/json"
	"fmt"
	"html/template"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ─── TEMPLATES ───────────────────────────────────────────────────────────────

var funcMap = template.FuncMap{
	"truncate": func(s string, n int) string {
		if len(s) <= n {
			return s
		}
		return s[:n] + "…"
	},
	"timeAgo": func(t time.Time) string {
		d := time.Since(t)
		switch {
		case d < time.Minute:
			return "just now"
		case d < time.Hour:
			return fmt.Sprintf("%dm ago", int(d.Minutes()))
		case d < 24*time.Hour:
			return fmt.Sprintf("%dh ago", int(d.Hours()))
		default:
			return fmt.Sprintf("%dd ago", int(d.Hours()/24))
		}
	},
	"formatTime": func(t time.Time) string {
		return t.Format("2006-01-02 15:04:05")
	},
	"cost": func(inputTokens, outputTokens int) string {
		c := float64(inputTokens)*0.000003 + float64(outputTokens)*0.000015
		return fmt.Sprintf("$%.4f", c)
	},
	"statusBadge": func(status string) template.HTML {
		class := "badge-pending"
		switch status {
		case "done", "ok", "true":
			class = "badge-ok"
		case "failed", "error", "false":
			class = "badge-err"
		case "running":
			class = "badge-running"
		}
		return template.HTML(fmt.Sprintf(`<span class="badge %s">%s</span>`, class, status))
	},
}

const layoutHTML = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>ZBOT — {{.Title}}</title>
    <link rel="stylesheet" href="/static/style.css">
    <script src="https://unpkg.com/htmx.org@1.9.10"></script>
</head>
<body>
<nav>
    <span class="logo">⚡ ZBOT</span>
    <a href="/conversations" {{if eq .Active "conversations"}}class="active"{{end}}>Conversations</a>
    <a href="/memory" {{if eq .Active "memory"}}class="active"{{end}}>Memory</a>
    <a href="/workflows" {{if eq .Active "workflows"}}class="active"{{end}}>Workflows</a>
    <a href="/audit" {{if eq .Active "audit"}}class="active"{{end}}>Audit Log</a>
</nav>
<div class="container">
{{.Body}}
</div>
</body>
</html>`

func renderPage(w http.ResponseWriter, title, active, body string) {
	tmpl := template.Must(template.New("layout").Funcs(funcMap).Parse(layoutHTML))
	tmpl.Execute(w, map[string]any{
		"Title":  title,
		"Active": active,
		"Body":   template.HTML(body),
	})
}

// ─── INDEX ───────────────────────────────────────────────────────────────────

// handleIndex serves the React command center at the root path.
// Non-root unknown paths get 404.
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	// Known old-dashboard paths remain handled by their own HandleFunc registrations.
	// Unknown non-root paths → 404 (unless the React SPA handles them).
	frontendHandler().ServeHTTP(w, r)
}

// ─── CONVERSATIONS ───────────────────────────────────────────────────────────

func (s *Server) handleConversations(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(r.Context(),
		`SELECT session_id, MAX(created_at) as last_active,
		        SUM(input_tokens) as total_input, SUM(output_tokens) as total_output,
		        COUNT(*) as call_count
		 FROM zbot_audit_model_calls
		 GROUP BY session_id
		 ORDER BY last_active DESC LIMIT 50`)
	if err != nil {
		http.Error(w, "DB error: "+err.Error(), 500)
		return
	}
	defer rows.Close()

	var body strings.Builder
	body.WriteString(`<h1>Conversations</h1><table>
<tr><th>Session</th><th>Last Active</th><th>Model Calls</th><th>Tokens</th><th>Est. Cost</th></tr>`)

	for rows.Next() {
		var sessionID string
		var lastActive time.Time
		var totalInput, totalOutput, callCount int
		rows.Scan(&sessionID, &lastActive, &totalInput, &totalOutput, &callCount)
		cost := float64(totalInput)*0.000003 + float64(totalOutput)*0.000015
		body.WriteString(fmt.Sprintf(
			`<tr><td><a href="/conversations/%s">%s</a></td><td>%s</td><td>%d</td><td>%s</td><td>$%.4f</td></tr>`,
			sessionID, truncate(sessionID, 24), timeAgo(lastActive), callCount,
			fmt.Sprintf("%dK in / %dK out", totalInput/1000, totalOutput/1000), cost,
		))
	}
	body.WriteString(`</table>`)
	renderPage(w, "Conversations", "conversations", body.String())
}

func (s *Server) handleConversationDetail(w http.ResponseWriter, r *http.Request) {
	sessionID := strings.TrimPrefix(r.URL.Path, "/conversations/")
	if sessionID == "" {
		http.Redirect(w, r, "/conversations", http.StatusFound)
		return
	}

	var body strings.Builder
	body.WriteString(fmt.Sprintf(`<h1>Session: %s</h1>`, sessionID))

	// Model calls.
	body.WriteString(`<h2>Model Calls</h2><table>
<tr><th>Time</th><th>Model</th><th>Input Tokens</th><th>Output Tokens</th><th>Duration</th><th>Cost</th></tr>`)

	mRows, _ := s.db.Query(r.Context(),
		`SELECT model, input_tokens, output_tokens, duration_ms, created_at
		 FROM zbot_audit_model_calls WHERE session_id = $1
		 ORDER BY created_at DESC LIMIT 100`, sessionID)
	if mRows != nil {
		defer mRows.Close()
		for mRows.Next() {
			var model string
			var inTok, outTok int
			var durMs int64
			var created time.Time
			mRows.Scan(&model, &inTok, &outTok, &durMs, &created)
			cost := float64(inTok)*0.000003 + float64(outTok)*0.000015
			body.WriteString(fmt.Sprintf(
				`<tr><td>%s</td><td>%s</td><td>%d</td><td>%d</td><td>%dms</td><td>$%.4f</td></tr>`,
				created.Format("15:04:05"), model, inTok, outTok, durMs, cost,
			))
		}
	}
	body.WriteString(`</table>`)

	// Tool calls.
	body.WriteString(`<h2>Tool Calls</h2><table>
<tr><th>Time</th><th>Tool</th><th>Duration</th><th>Status</th><th>Input</th></tr>`)

	tRows, _ := s.db.Query(r.Context(),
		`SELECT tool_name, duration_ms, is_error, input, created_at
		 FROM zbot_audit_tool_calls WHERE session_id = $1
		 ORDER BY created_at DESC LIMIT 100`, sessionID)
	if tRows != nil {
		defer tRows.Close()
		for tRows.Next() {
			var toolName string
			var durMs int64
			var isError bool
			var inputJSON []byte
			var created time.Time
			tRows.Scan(&toolName, &durMs, &isError, &inputJSON, &created)
			status := "ok"
			if isError {
				status = "error"
			}
			body.WriteString(fmt.Sprintf(
				`<tr><td>%s</td><td>%s</td><td>%dms</td><td>%s</td><td>%s</td></tr>`,
				created.Format("15:04:05"), toolName, durMs,
				badgeHTML(status), truncate(string(inputJSON), 80),
			))
		}
	}
	body.WriteString(`</table>`)

	renderPage(w, "Session "+truncate(sessionID, 16), "conversations", body.String())
}

// ─── MEMORY ──────────────────────────────────────────────────────────────────

func (s *Server) handleMemory(w http.ResponseWriter, r *http.Request) {
	search := r.URL.Query().Get("q")

	var body strings.Builder
	body.WriteString(`<h1>Memory</h1>`)
	body.WriteString(`<div class="search-box">
<form method="get"><input type="text" name="q" placeholder="Search memories..." value="` + search + `">
<button type="submit" class="btn">Search</button></form></div>`)
	body.WriteString(`<table>
<tr><th>ID</th><th>Source</th><th>Tags</th><th>Content</th><th>Created</th><th></th></tr>`)

	query := `SELECT id, source, tags, content, created_at FROM zbot_memories ORDER BY created_at DESC LIMIT 100`
	args := []any{}
	if search != "" {
		query = `SELECT id, source, tags, content, created_at FROM zbot_memories WHERE content ILIKE $1 ORDER BY created_at DESC LIMIT 100`
		args = append(args, "%"+search+"%")
	}

	rows, err := s.db.Query(r.Context(), query, args...)
	if err != nil {
		body.WriteString(fmt.Sprintf(`<tr><td colspan="6">Error: %v</td></tr>`, err))
	} else {
		defer rows.Close()
		for rows.Next() {
			var id, source, content string
			var tags []string
			var created time.Time
			rows.Scan(&id, &source, &tags, &content, &created)
			body.WriteString(fmt.Sprintf(
				`<tr id="mem-%s"><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td>
				<td><button class="btn btn-danger" hx-delete="/memory/%s" hx-target="#mem-%s" hx-swap="outerHTML"
				  hx-confirm="Delete this memory?">✕</button></td></tr>`,
				id, truncate(id, 8), source, strings.Join(tags, ", "),
				truncate(content, 120), timeAgo(created), id, id,
			))
		}
	}
	body.WriteString(`</table>`)
	renderPage(w, "Memory", "memory", body.String())
}

func (s *Server) handleMemoryAction(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/memory/")
	if id == "" {
		http.Error(w, "missing ID", 400)
		return
	}
	if r.Method == http.MethodDelete {
		_, err := s.db.Exec(r.Context(), `DELETE FROM zbot_memories WHERE id = $1`, id)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.WriteHeader(200)
		return
	}
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

// ─── WORKFLOWS ───────────────────────────────────────────────────────────────

func (s *Server) handleWorkflows(w http.ResponseWriter, r *http.Request) {
	var body strings.Builder
	body.WriteString(`<h1>Workflows</h1><table>
<tr><th>Workflow ID</th><th>Status</th><th>Tasks</th><th>Created</th></tr>`)

	rows, err := s.db.Query(r.Context(),
		`SELECT w.id, w.status, w.created_at,
		        COUNT(t.id) as task_count,
		        COUNT(CASE WHEN t.status = 'done' THEN 1 END) as done_count
		 FROM zbot_workflows w
		 LEFT JOIN zbot_tasks t ON t.workflow_id = w.id
		 GROUP BY w.id, w.status, w.created_at
		 ORDER BY w.created_at DESC LIMIT 50`)
	if err != nil {
		body.WriteString(fmt.Sprintf(`<tr><td colspan="4">Error: %v</td></tr>`, err))
	} else {
		defer rows.Close()
		for rows.Next() {
			var id, status string
			var created time.Time
			var taskCount, doneCount int
			rows.Scan(&id, &status, &created, &taskCount, &doneCount)
			body.WriteString(fmt.Sprintf(
				`<tr><td>%s</td><td>%s</td><td>%d/%d done</td><td>%s</td></tr>`,
				truncate(id, 16), badgeHTML(status), doneCount, taskCount, timeAgo(created),
			))
		}
	}
	body.WriteString(`</table>`)
	renderPage(w, "Workflows", "workflows", body.String())
}

func (s *Server) handleWorkflowAction(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/workflows/"), "/")
	if len(parts) < 2 || parts[1] != "cancel" || r.Method != http.MethodPost {
		http.Error(w, "not found", 404)
		return
	}
	wfID := parts[0]
	_, err := s.db.Exec(r.Context(),
		`UPDATE zbot_tasks SET status = 'canceled', updated_at = NOW()
		 WHERE workflow_id = $1 AND status = 'pending'`, wfID)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	http.Redirect(w, r, "/workflows", http.StatusFound)
}

// ─── AUDIT ───────────────────────────────────────────────────────────────────

func (s *Server) handleAudit(w http.ResponseWriter, r *http.Request) {
	tab := r.URL.Query().Get("tab")
	if tab == "" {
		tab = "tools"
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	offset := (page - 1) * 50
	filterSession := r.URL.Query().Get("session")

	var body strings.Builder
	body.WriteString(`<h1>Audit Log</h1>`)

	// Tabs.
	body.WriteString(`<div class="tabs">`)
	for _, t := range []struct{ id, label string }{{"tools", "Tool Calls"}, {"models", "Model Calls"}, {"workflows", "Workflow Events"}} {
		cls := ""
		if tab == t.id {
			cls = " active"
		}
		body.WriteString(fmt.Sprintf(`<a class="tab%s" href="/audit?tab=%s">%s</a>`, cls, t.id, t.label))
	}
	body.WriteString(`</div>`)

	// Filter.
	body.WriteString(fmt.Sprintf(`<div class="search-box"><form method="get">
<input type="hidden" name="tab" value="%s">
<input type="text" name="session" placeholder="Filter by session ID..." value="%s">
<button type="submit" class="btn">Filter</button></form></div>`, tab, filterSession))

	switch tab {
	case "tools":
		s.renderToolCalls(&body, r, filterSession, offset)
	case "models":
		s.renderModelCalls(&body, r, filterSession, offset)
	case "workflows":
		s.renderWorkflowEvents(&body, r, filterSession, offset)
	}

	// Pagination.
	body.WriteString(`<div class="pagination">`)
	if page > 1 {
		body.WriteString(fmt.Sprintf(`<a href="/audit?tab=%s&page=%d&session=%s">← Prev</a>`, tab, page-1, filterSession))
	}
	body.WriteString(fmt.Sprintf(`<span>Page %d</span>`, page))
	body.WriteString(fmt.Sprintf(`<a href="/audit?tab=%s&page=%d&session=%s">Next →</a>`, tab, page+1, filterSession))
	body.WriteString(`</div>`)

	renderPage(w, "Audit Log", "audit", body.String())
}

func (s *Server) renderToolCalls(body *strings.Builder, r *http.Request, sessionFilter string, offset int) {
	body.WriteString(`<table>
<tr><th>Time</th><th>Session</th><th>Tool</th><th>Duration</th><th>Status</th><th>Input</th></tr>`)

	query := `SELECT session_id, tool_name, duration_ms, is_error, input, created_at
	          FROM zbot_audit_tool_calls`
	args := []any{}
	if sessionFilter != "" {
		query += ` WHERE session_id = $1`
		args = append(args, sessionFilter)
	}
	query += ` ORDER BY created_at DESC LIMIT 50 OFFSET ` + strconv.Itoa(offset)

	rows, err := s.db.Query(r.Context(), query, args...)
	if err != nil {
		body.WriteString(fmt.Sprintf(`<tr><td colspan="6">Error: %v</td></tr>`, err))
	} else {
		defer rows.Close()
		for rows.Next() {
			var sessionID, toolName string
			var durMs int64
			var isError bool
			var inputJSON []byte
			var created time.Time
			rows.Scan(&sessionID, &toolName, &durMs, &isError, &inputJSON, &created)
			status := "ok"
			if isError {
				status = "error"
			}
			body.WriteString(fmt.Sprintf(
				`<tr><td>%s</td><td>%s</td><td>%s</td><td>%dms</td><td>%s</td><td>%s</td></tr>`,
				created.Format("01-02 15:04"), truncate(sessionID, 16), toolName,
				durMs, badgeHTML(status), truncate(string(inputJSON), 60),
			))
		}
	}
	body.WriteString(`</table>`)
}

func (s *Server) renderModelCalls(body *strings.Builder, r *http.Request, sessionFilter string, offset int) {
	body.WriteString(`<table>
<tr><th>Time</th><th>Session</th><th>Model</th><th>In Tokens</th><th>Out Tokens</th><th>Duration</th><th>Est. Cost</th></tr>`)

	query := `SELECT session_id, model, input_tokens, output_tokens, duration_ms, created_at
	          FROM zbot_audit_model_calls`
	args := []any{}
	if sessionFilter != "" {
		query += ` WHERE session_id = $1`
		args = append(args, sessionFilter)
	}
	query += ` ORDER BY created_at DESC LIMIT 50 OFFSET ` + strconv.Itoa(offset)

	rows, err := s.db.Query(r.Context(), query, args...)
	if err != nil {
		body.WriteString(fmt.Sprintf(`<tr><td colspan="7">Error: %v</td></tr>`, err))
	} else {
		defer rows.Close()
		for rows.Next() {
			var sessionID, model string
			var inTok, outTok int
			var durMs int64
			var created time.Time
			rows.Scan(&sessionID, &model, &inTok, &outTok, &durMs, &created)
			cost := float64(inTok)*0.000003 + float64(outTok)*0.000015
			body.WriteString(fmt.Sprintf(
				`<tr><td>%s</td><td>%s</td><td>%s</td><td>%d</td><td>%d</td><td>%dms</td><td>$%.4f</td></tr>`,
				created.Format("01-02 15:04"), truncate(sessionID, 16), model,
				inTok, outTok, durMs, cost,
			))
		}
	}
	body.WriteString(`</table>`)
}

func (s *Server) renderWorkflowEvents(body *strings.Builder, r *http.Request, sessionFilter string, offset int) {
	body.WriteString(`<table>
<tr><th>Time</th><th>Workflow</th><th>Task</th><th>Event</th><th>Detail</th></tr>`)

	query := `SELECT workflow_id, task_id, event, detail, created_at
	          FROM zbot_audit_workflow_events`
	args := []any{}
	if sessionFilter != "" {
		query += ` WHERE workflow_id = $1`
		args = append(args, sessionFilter)
	}
	query += ` ORDER BY created_at DESC LIMIT 50 OFFSET ` + strconv.Itoa(offset)

	rows, err := s.db.Query(r.Context(), query, args...)
	if err != nil {
		body.WriteString(fmt.Sprintf(`<tr><td colspan="5">Error: %v</td></tr>`, err))
	} else {
		defer rows.Close()
		for rows.Next() {
			var wfID, event string
			var taskID, detail *string
			var created time.Time
			rows.Scan(&wfID, &taskID, &event, &detail, &created)
			tid := ""
			if taskID != nil {
				tid = truncate(*taskID, 12)
			}
			det := ""
			if detail != nil {
				det = truncate(*detail, 80)
			}
			body.WriteString(fmt.Sprintf(
				`<tr><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td></tr>`,
				created.Format("01-02 15:04"), truncate(wfID, 12), tid, event, det,
			))
		}
	}
	body.WriteString(`</table>`)
}

// ─── API STATS ───────────────────────────────────────────────────────────────

func (s *Server) handleAPIStats(w http.ResponseWriter, r *http.Request) {
	stats := map[string]any{}

	// Total token usage.
	var totalInput, totalOutput int
	s.db.QueryRow(r.Context(),
		`SELECT COALESCE(SUM(input_tokens),0), COALESCE(SUM(output_tokens),0) FROM zbot_audit_model_calls`).
		Scan(&totalInput, &totalOutput)
	stats["total_input_tokens"] = totalInput
	stats["total_output_tokens"] = totalOutput
	stats["estimated_cost"] = math.Round((float64(totalInput)*0.000003+float64(totalOutput)*0.000015)*10000) / 10000

	// Tool call counts.
	var toolCallCount int
	s.db.QueryRow(r.Context(), `SELECT COUNT(*) FROM zbot_audit_tool_calls`).Scan(&toolCallCount)
	stats["total_tool_calls"] = toolCallCount

	// Model call counts.
	var modelCallCount int
	s.db.QueryRow(r.Context(), `SELECT COUNT(*) FROM zbot_audit_model_calls`).Scan(&modelCallCount)
	stats["total_model_calls"] = modelCallCount

	// Memory count.
	var memCount int
	s.db.QueryRow(r.Context(), `SELECT COUNT(*) FROM zbot_memories`).Scan(&memCount)
	stats["total_memories"] = memCount

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// ─── HELPERS ─────────────────────────────────────────────────────────────────

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func timeAgo(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func badgeHTML(status string) string {
	class := "badge-pending"
	switch status {
	case "done", "ok", "running_ok":
		class = "badge-ok"
	case "failed", "error":
		class = "badge-err"
	case "running":
		class = "badge-running"
	}
	return fmt.Sprintf(`<span class="badge %s">%s</span>`, class, status)
}
