# ZBOT Sprint 13 — Coworker Mission Brief
## Objective: Workspace File Panel — See Everything ZBOT Creates

You are working on ZBOT, a personal AI agent for Jeremy Lerwick (CEO, Ziloss Technologies).
The codebase is at: ~/Desktop/zbot
GitHub: https://github.com/jeremylerwick-max/zbot (private)
GCP Project: ziloss (project number: 203743871797)

Sprint 12 is complete. Memory is wired. ZBOT remembers across sessions.
Your job is to make the files ZBOT creates visible, previewable, and downloadable in the UI.

---

## Current State (Sprint 12 Complete)

- Dual Brain Command Center UI live at http://localhost:18790 ✅
- GPT-4o planner + Claude executor + GPT-4o critic ✅
- Cross-session memory via pgvector ✅
- Memory panel in UI ✅
- Skills: search, memory, GHL, GitHub, Google Sheets, Email ✅
- ZBOT creates files in ~/zbot-workspace/ during tasks ✅
- BUT: no way to see/download/preview those files in the UI ❌
- Files just pile up invisibly on disk ❌

---

## Architecture Overview

```
~/zbot-workspace/           — ZBOT's working directory
  reports/                  — markdown + text reports
  data/                     — CSVs, JSON outputs
  code/                     — scripts ZBOT writes
  exports/                  — formatted deliverables
  screenshots/              — captured images

Target:
  GET /api/workspace        — list files with metadata
  GET /api/workspace/file   — download a file
  DELETE /api/workspace/file — delete a file
  WorkspacePanel.tsx        — file browser panel in UI
  File preview drawer       — render markdown, CSV, code inline
```

---

## Sprint 13 Tasks — Complete ALL in Order

### PHASE 1: Workspace API

File: `internal/webui/api_handlers.go` — add workspace endpoints

```go
// WorkspaceFile represents a file in the workspace
type WorkspaceFile struct {
    Name      string    `json:"name"`
    Path      string    `json:"path"`      // relative to workspace root
    Size      int64     `json:"size"`
    SizeHuman string    `json:"size_human"` // "12 KB", "1.2 MB"
    Extension string    `json:"extension"`  // "md", "csv", "json", "py", "txt"
    CreatedAt time.Time `json:"created_at"`
    UpdatedAt time.Time `json:"updated_at"`
    WorkflowID string   `json:"workflow_id,omitempty"` // if file was created by a workflow
}

// GET /api/workspace
// Query params: ?ext=md&sort=newest&limit=50
// Returns: { files: WorkspaceFile[], total: int, workspace_path: string }
// Walk ~/zbot-workspace recursively, return all files sorted by newest first
// Skip hidden files and directories

// GET /api/workspace/download?path={relative_path}
// Streams file contents as download
// Validate path is within workspace root (prevent path traversal)
// Set appropriate Content-Type based on extension

// DELETE /api/workspace/file?path={relative_path}
// Delete the file
// Validate path is within workspace root
// Return 204 on success

// GET /api/workspace/preview?path={relative_path}
// Returns file contents as text (max 50KB)
// For binary files, return { error: "binary file — download only" }
```

**Checkpoint:** Build passes. `curl http://localhost:18790/api/workspace` returns file list.

---

### PHASE 2: Workspace Panel Component

File: `internal/webui/frontend/src/components/WorkspacePanel.tsx` (NEW)
File: `internal/webui/frontend/src/App.tsx` — add workspace panel toggle

**WorkspacePanel layout:**
```
┌─────────────────────────────────────┐
│ 📁 Workspace          [🔍] [↑ sort] │
│ ~/zbot-workspace  •  47 files       │
├─────────────────────────────────────┤
│ [All] [.md] [.csv] [.json] [.py]   │
├─────────────────────────────────────┤
│ 📄 competitor_analysis.md           │
│    12 KB • 2 min ago • workflow-4a  │
│    [Preview] [Download] [Delete]    │
│─────────────────────────────────────│
│ 📊 leads_export.csv                 │
│    84 KB • 1 hour ago               │
│    [Preview] [Download] [Delete]    │
└─────────────────────────────────────┘
```

- Slide-in panel from right (same pattern as MemoryPanel)
- Triggered by folder icon in top nav
- Filter tabs: All / .md / .csv / .json / .py / other
- Sort toggle: Newest / Oldest / Largest
- Each file card shows: icon by type, name, size, age, workflow ID if available
- Three action buttons per file: Preview, Download, Delete
- Auto-refresh every 10 seconds (poll /api/workspace)
- Empty state: "No files yet — ZBOT will save files here as it works"
- Dark ops theme: #0a0b0d background, blue-violet accent for files

**File type icons (use emoji or SVG):**
- .md → 📄 (document)
- .csv → 📊 (chart)
- .json → {} (code)
- .py / .js / .go → 💻 (code)
- .pdf → 📕 (book)
- .png / .jpg → 🖼️ (image)
- other → 📁

**Checkpoint:** Folder icon opens panel. Files from ~/zbot-workspace appear.

---

### PHASE 3: File Preview Drawer

File: `internal/webui/frontend/src/components/FilePreviewDrawer.tsx` (NEW)

When user clicks "Preview" on a file, open a full-height drawer over the workspace panel:

**Markdown files (.md):**
- Render with marked.js (import from CDN: https://cdnjs.cloudflare.com/ajax/libs/marked/9.1.6/marked.min.js)
- Dark styled: white text, code blocks with syntax highlighting
- "Download" button at top right

**CSV files (.csv):**
- Parse with PapaParse (import from CDN)
- Render as a styled table: sticky header, zebra rows, scrollable
- Show row count: "247 rows × 8 columns"
- "Download" button at top right

**JSON files (.json):**
- Pretty-print with syntax highlighting (color keys/values/strings)
- Collapsible nested objects
- "Download" button at top right

**Code files (.py, .js, .go, .ts, .sh):**
- Monospace font (JetBrains Mono — already in the UI)
- Line numbers
- Syntax highlight based on extension
- "Download" button at top right

**Plain text (.txt, other):**
- Monospace, line-wrapped
- "Download" button at top right

**Binary / unknown:**
- Show: "Binary file — preview not available"
- Show file metadata: size, created, modified
- "Download" button only

**Checkpoint:** Click Preview on a .md file → rendered markdown drawer opens. Click Preview on .csv → table renders.

---

### PHASE 4: Workflow File Linking

File: `internal/workflow/orchestrator.go` — track files created per workflow
File: `internal/webui/api_handlers.go` — expose workflow files endpoint

When Claude executes a task that creates a file in ~/zbot-workspace, log it:

```go
// Add to zbot_tasks table: output_files TEXT[] (array of relative file paths)
// After each task completes, scan workspace for new files created during task execution
// Compare file mtimes to task started_at — any file newer than task start is attributed to that task
// Store in output_files column

// New endpoint:
// GET /api/workflow/:id/files
// Returns files created during this workflow
```

In the UI, add to TaskCard component:
- If task has output files, show a small file chip: "📄 report.md" 
- Clicking the chip opens FilePreviewDrawer directly
- This creates a direct link from "Claude executed this task" to "here's what it created"

**Checkpoint:** Run a plan that creates a file. See file chip appear on the task card that created it.

---

### PHASE 5: File Creation Tool Improvement

File: `internal/tools/` — check existing write_file tool

Find the existing write_file tool (or create it if missing).
Ensure it:
1. Always writes inside ~/zbot-workspace/ — never outside (path safety check)
2. Creates subdirectories automatically if needed
3. Returns the relative path of the created file (for workflow file linking)
4. Supports these extensions cleanly: .md, .csv, .json, .txt, .py, .js, .html
5. Logs: "file created: {relative_path} ({size})"

Also ensure a read_file tool exists:
1. Can read any file inside ~/zbot-workspace/
2. Returns contents as string (max 100KB — truncate with notice if larger)
3. Can list directory contents

**Checkpoint:** Claude can create a markdown report and it appears in the workspace panel.

---

## Definition of Done

1. `go build ./...` passes clean
2. GET /api/workspace returns files from ~/zbot-workspace
3. WorkspacePanel opens via folder icon, shows all files
4. Filter tabs work (All / .md / .csv / etc)
5. Preview drawer renders markdown, CSV, JSON, code correctly
6. Download button streams file to browser
7. Delete button removes file from disk and updates panel
8. Task cards show file chips for files created during that task
9. Manual test: run "research GoHighLevel competitors and write a report" → report.md appears in workspace panel → click Preview → rendered markdown shows

---

## Final Git Commit

```bash
cd ~/Desktop/zbot
git add -A
git commit -m "Sprint 13: Workspace file panel — see everything ZBOT creates

- webui: GET /api/workspace, /api/workspace/download, /api/workspace/preview, DELETE /api/workspace/file
- WorkspacePanel.tsx: file browser with filter tabs and sort
- FilePreviewDrawer.tsx: markdown render, CSV table, JSON highlight, code viewer
- orchestrator.go: track output_files per task (zbot_tasks.output_files)
- TaskCard.tsx: file chips link tasks to created files
- tools/write_file: path safety, subdirectory creation, relative path return
- tools/read_file: workspace-scoped file reading"

git tag -a v1.13.0 -m "Sprint 13: Workspace file panel"
git push origin main
git push origin v1.13.0
```

---

## Important Notes

- Workspace root is ~/zbot-workspace — ALL file operations must be scoped to this directory
- Path traversal prevention is mandatory: reject any path containing ".." or starting with "/"
- Preview endpoint: max 50KB returned — truncate larger files with a notice
- Auto-refresh every 10s is fine — do not use SSE for workspace (polling is sufficient)
- go build ./... must pass after every phase before moving to the next
- All secrets come from GCP Secret Manager — never hardcode credentials
