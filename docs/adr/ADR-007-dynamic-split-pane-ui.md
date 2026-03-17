# ADR-007: Dynamic Split-Pane UI Replacing Fixed 3-Panel Layout

**Status:** Accepted
**Date:** 2026-03-16
**Supersedes:** Sprint 11 PlannerPanel/ExecutorPanel/ObserverPanel design

## Context

The Sprint 11 UI was designed around the three-model orchestration pattern: a fixed left panel for GPT-4o planning, a fixed right panel for Claude execution, and an observer panel for the critic. With ADR-004 killing that pattern, the UI must change to match.

Beyond just removing dead panels, this is an opportunity to rethink the UI entirely. The fixed 3-panel layout forced users into a single workflow: submit goal → watch plan → watch execution. Real usage is more dynamic: users want to code while research runs in the background, or check their memory while chatting.

## Decision

**Replace the fixed 3-panel layout with a dynamic split-pane system** where users start with a single Chat pane and can open additional panes on demand.

### Design Principles

1. **Chat is always present.** It's the primary interaction point. Can be minimized but never closed.
2. **Panes open on demand.** User says "open a code editor" or ZBOT decides to open a research panel. No fixed layout.
3. **Max 4 visible panes.** Beyond this, usability degrades on any screen size. Users can close and reopen panes freely.
4. **Content preserved on close.** Closing a pane doesn't destroy its content. Reopening the same pane type restores state.
5. **ZBOT routes output to the right pane.** Backend knows which panes are open and sends code to the Code pane, research to the Research pane, etc.

### Pane Types

| Type | Purpose | Trigger |
|------|---------|---------|
| Chat | Main conversation | Always present |
| Code | Syntax-highlighted editor + run button | "write a script", "help me code" |
| Research | Deep research progress + report | "research X", deep_research tool call |
| Terminal | Direct shell access | "open a terminal" |
| File Viewer | Preview images/PDFs/markdown | File attached or referenced |
| Memory | Browse pgvector contents | "what do you remember about X" |
| Web Preview | Rendered HTML | Building HTML/CSS, scraping preview |

### Layout Engine

Using `react-resizable-panels` (lightweight, well-maintained, VS Code-style split handles).

```typescript
type PaneType = 'chat' | 'code' | 'research' | 'terminal' | 'file' | 'memory' | 'web'

interface Pane {
  id: string
  type: PaneType
  title: string
  state: PaneState  // pane-specific state (typed per PaneType)
}

interface PaneLayout {
  direction: 'horizontal' | 'vertical'
  children: (Pane | PaneLayout)[]
  sizes: number[]
}
```

### Pane Communication

Panes communicate through a shared `PaneContext` (React Context + useReducer). Events:

- `PANE_OPEN` — new pane requested
- `PANE_CLOSE` — pane closed by user or ZBOT
- `PANE_OUTPUT` — content routed to a specific pane
- `PANE_FOCUS` — bring a pane to the foreground

The Go backend includes a `pane_id` field in SSE events so the frontend routes output to the correct pane.

## Components to Delete

| Component | Reason |
|-----------|--------|
| PlannerPanel.tsx | No separate planner model |
| ExecutorPanel.tsx | Replaced by Code/Terminal/Research panes |
| ObserverPanel.tsx | No observer/critic model |
| HandoffAnimation.tsx | No model-to-model handoff |
| CriticBadge.tsx | No critic |

## Components to Keep/Evolve

| Component | Action |
|-----------|--------|
| CommandBar.tsx | Keep — main input, stays at top |
| Sidebar.tsx | Keep — update navigation items |
| MetricsStrip.tsx | Simplify — show: active tasks, token usage, cost |
| MemoryPanel.tsx | Evolve into Memory pane type |
| ResearchPanel.tsx | Evolve into Research pane type |
| WorkspacePanel.tsx | Evolve into File Viewer pane type |
| DashboardPage.tsx | Keep for overview/stats |

## Consequences

### Positive

- **Matches the single-brain architecture.** No more UI components for a multi-model pattern that no longer exists.
- **Flexible workflows.** Users can combine panes to match their task (code + research, chat + terminal, etc.).
- **Familiar mental model.** VS Code, tmux, and tiling window managers have trained users to think in split panes.
- **Future-proof.** Adding new pane types (e.g., Z-Glass preview, email composer) is trivial — implement a new pane component, register it in the PaneManager.

### Negative

- **More complex state management.** Dynamic layouts require tracking pane positions, sizes, and content across resizes and reloads. Mitigated by `react-resizable-panels` handling the hard parts and localStorage for persistence.
- **Mobile complexity.** Split panes don't work on small screens. Mobile layout must stack panes vertically with a tab switcher. This is additional work.
- **4-pane limit may feel restrictive.** Power users might want 5+. Starting with 4, will evaluate based on usage.

## Must-Have (This Update)

- Dynamic pane splitting (Chat + one additional pane)
- Code pane with syntax highlighting + Docker sandbox run
- Research pane (adapted from existing ResearchPanel)
- Remove all deleted components (Planner, Executor, Observer, Handoff, Critic)
- Simplify MetricsStrip
- Pane resize via drag handles

## Nice-to-Have (Next Update)

- Terminal pane
- File viewer pane
- Memory pane (as split, not drawer)
- Web preview pane
- Keyboard shortcuts (Cmd+1/2/3/4 to focus panes)
- Layout persistence across sessions
- 3+ simultaneous panes
