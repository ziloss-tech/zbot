# ZBOT v2 — Dynamic Split-Pane UI Spec
_Created: 2026-03-16 | For: Next update_

---

## Concept

Replace the fixed 3-panel Planner/Executor/Observer layout with a **dynamic split-pane system** where:
- User starts with a single **Chat** pane (main conversation with ZBOT)
- User can request additional panes on the fly: "Z, open a coding panel" → window splits
- Each pane is an independent context with its own purpose
- Panes can be resized, rearranged, and closed
- ZBOT manages context across panes (chat knows what code panel is doing and vice versa)

Think: VS Code's split editor + Claude's artifact panel + tmux, but for an AI agent.

---

## Pane Types

### 1. Chat (always present, default)
- Main conversation with ZBOT
- Natural language input
- Where you give commands, ask questions, get responses
- Shows status of background tasks: "Deep research: 23/50 sources..."
- Cannot be closed (but can be minimized)

### 2. Code
- Full code editor with syntax highlighting
- ZBOT writes code here, user can edit
- Language detection + appropriate formatting
- Run button → executes in Docker sandbox
- Output panel below the code
- File tabs if working on multiple files
- "Z, write a Python script to..." → code appears in this pane

### 3. Research
- Deep research output panel
- Shows: sources being gathered, synthesis progress, final report
- Live updating as Haiku gathers + thinking model synthesizes
- Citation links, source cards
- "Z, research AR display technology" → research panel opens + populates

### 4. Terminal
- Direct shell access on the host machine
- ZBOT can execute commands here, user can type too
- Output streams in real-time
- "Z, open a terminal" → terminal pane appears

### 5. File Viewer
- Preview files: images, PDFs, markdown, code
- Side-by-side with chat for "look at this and tell me..."
- Drag-and-drop file upload

### 6. Memory
- Visual browser of ZBOT's memory (pgvector contents)
- Search, view, edit, delete memories
- Daily notes timeline
- "Z, what do you remember about the GHL project?" → memory panel opens with results

### 7. Web Preview
- Rendered web page preview
- For when ZBOT builds HTML/CSS or is scraping a site
- Shows the page as a user would see it

---

## Dynamic Layout Behavior

### Splitting
- User says "open a code editor" or "I want to code" → Chat shrinks to left, Code pane opens on right
- User says "also research this topic" → splits again, now 3 panes
- Max 4 panes visible at once (beyond that it gets unusable)
- Smart defaults: Chat always stays, new pane opens to the right or below

### Layout Presets (user can also manually resize)
```
[Chat]                          ← default, single pane

[Chat     |  Code]              ← "I want to code"

[Chat     |  Code]              ← "also research this"
           |  Research]

[Chat | Code | Research]        ← 3-way horizontal split

[  Chat  |  Terminal  ]         ← "open a terminal"
[     Research        ]         ← stacked layout
```

### Resizing
- Drag borders between panes to resize (like VS Code split)
- Double-click border to reset to equal widths
- Keyboard shortcuts: Cmd+1/2/3/4 to focus panes

### Closing
- Click X on any pane (except Chat) to close it
- "Z, close the code panel" → closes it
- Content is preserved if you re-open the same type

---

## What to REMOVE from Current UI

1. **PlannerPanel.tsx** — DELETE. No more separate planner. Chat IS the planner now.
2. **ExecutorPanel.tsx** — DELETE. Execution happens in Code/Terminal/Research panes.
3. **ObserverPanel.tsx** — DELETE. No more observer/critic model. Single brain.
4. **HandoffAnimation.tsx** — DELETE. No more model-to-model handoff.
5. **CriticBadge.tsx** — DELETE. No critic.
6. **MetricsStrip.tsx** — KEEP but simplify. Show: active tasks, token usage, cost.
7. **The 3-panel workflow view** — REPLACE with dynamic split-pane system.

## What to KEEP
1. **CommandBar.tsx** — KEEP. This is the main input. Stays at top.
2. **Sidebar.tsx** — KEEP but update navigation items.
3. **MemoryPanel.tsx** — KEEP, becomes a pane type instead of drawer.
4. **WorkspacePanel.tsx** — EVOLVE into File Viewer pane.
5. **ResearchPanel.tsx** — KEEP, becomes a pane type instead of drawer.
6. **SchedulePanel.tsx** — KEEP as sidebar item or pane.
7. **DashboardPage.tsx** — KEEP for overview/stats.
8. **KnowledgeBasePage.tsx** — KEEP.

---

## Technical Implementation

### Pane Manager (new: PaneManager.tsx)
```tsx
type PaneType = 'chat' | 'code' | 'research' | 'terminal' | 'file' | 'memory' | 'web'

interface Pane {
  id: string
  type: PaneType
  title: string
  state: any  // pane-specific state
}

interface PaneLayout {
  direction: 'horizontal' | 'vertical'
  children: (Pane | PaneLayout)[]
  sizes: number[]  // percentage widths
}
```

### Split Pane Library
- Use `react-resizable-panels` (lightweight, well-maintained)
- Or `allotment` (used by VS Code's web version)
- Both support: horizontal/vertical splits, resize handles, min/max sizes, persistence

### Pane Communication
- Each pane gets a unique ID
- Panes communicate through a shared event bus (React context + useReducer)
- Chat pane can reference other panes: "run the code in the code panel"
- ZBOT backend tracks which panes are open and routes output to the right one

### State Persistence
- Pane layout saved to localStorage
- Re-opening ZBOT restores your last layout
- Pane contents (code, research results) persisted to pgvector/filesystem

---

## Example User Flows

### Flow 1: "Help me write a script"
1. User types in Chat: "Write me a Python script that scrapes Hacker News"
2. ZBOT opens Code pane (splits right)
3. Code appears in Code pane with syntax highlighting
4. Chat shows: "I've written the script in the code panel. Want me to run it?"
5. User: "Run it"
6. Terminal pane opens below Code pane, shows output

### Flow 2: "Research something while I code"
1. User is in Chat + Code layout
2. User types: "Also research waveguide AR display technology"
3. Research pane opens (third column or stacked below)
4. Haiku starts gathering sources (live progress in Research pane)
5. User continues coding in Code pane
6. Chat shows: "Research is 60% done (34/50 sources)..."
7. Research completes, final report appears in Research pane
8. All three panels active simultaneously

### Flow 3: "Check my email and fix this bug"
1. User: "Check my email, anything urgent?"
2. Chat: "You have 3 urgent emails. Brian needs the campaign numbers by EOD."
3. User: "OK also help me fix this bug, open the code"
4. Code pane opens with the relevant file
5. Both tasks handled concurrently — email triage in background, code help in foreground

---

## Sprint Scope for v2 Update

### Must Have (this update)
- [ ] Dynamic pane splitting (Chat + one additional pane)
- [ ] Code pane with syntax highlighting + run button
- [ ] Research pane (existing ResearchPanel adapted)
- [ ] Remove Planner/Executor/Observer panels
- [ ] Remove HandoffAnimation, CriticBadge
- [ ] Simplify MetricsStrip
- [ ] Pane resize via drag handles

### Nice to Have (next update)
- [ ] Terminal pane
- [ ] File viewer pane
- [ ] Memory pane (as split, not drawer)
- [ ] Web preview pane
- [ ] Keyboard shortcuts
- [ ] Layout persistence
- [ ] 3+ simultaneous panes

---

_Filed: ~/Desktop/Projects/zbot/docs/ZBOT_V2_UI_SPEC.md_
