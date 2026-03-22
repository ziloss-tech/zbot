import { useState, useCallback, useRef, useEffect } from 'react'
import { motion, AnimatePresence } from 'framer-motion'
import { ChatPane } from './ChatPane'
import { ThalamusPane } from './ThalamusPane'
import { WorkflowHistory } from './WorkflowHistory'
import { FileTreePane } from './FileTreePane'
import { CodePreviewPane } from './CodePreviewPane'
import type { WorkflowState } from '../lib/types'
import type { AgentEvent } from '../hooks/useEventBus'

// ─── Pane Registry ──────────────────────────────────────────────────────────

export type PaneType = 'chat' | 'cortex' | 'thalamus' | 'tasks' | 'history' | 'files' | 'code_preview'

interface PaneConfig {
  id: string
  type: PaneType
  label: string
  icon: string
}

const PANE_TEMPLATES: Record<PaneType, Omit<PaneConfig, 'id'>> = {
  chat:      { type: 'chat',      label: 'Chat',      icon: '💬' },
  cortex:    { type: 'cortex',    label: 'Cortex',    icon: '🧠' },
  thalamus:  { type: 'thalamus',  label: 'Auditor',   icon: '✅' },
  tasks:     { type: 'tasks',     label: 'Tasks',     icon: '📋' },
  history:   { type: 'history',   label: 'History',   icon: '📜' },
  files:        { type: 'files',        label: 'Files',   icon: '📁' },
  code_preview: { type: 'code_preview', label: 'Code',    icon: '📄' },
}

let paneCounter = 0
function createPane(type: PaneType): PaneConfig {
  paneCounter++
  return { id: `pane-${paneCounter}`, ...PANE_TEMPLATES[type] }
}

// ─── Split Pane Resizer ─────────────────────────────────────────────────────

interface ResizerProps {
  onResize: (delta: number) => void
}

function Resizer({ onResize }: ResizerProps) {
  const dragging = useRef(false)
  const startX = useRef(0)

  const onMouseDown = useCallback((e: React.MouseEvent) => {
    e.preventDefault()
    dragging.current = true
    startX.current = e.clientX

    const onMouseMove = (moveEvt: MouseEvent) => {
      if (!dragging.current) return
      const delta = moveEvt.clientX - startX.current
      startX.current = moveEvt.clientX
      onResize(delta)
    }

    const onMouseUp = () => {
      dragging.current = false
      document.removeEventListener('mousemove', onMouseMove)
      document.removeEventListener('mouseup', onMouseUp)
      document.body.style.cursor = ''
      document.body.style.userSelect = ''
    }

    document.body.style.cursor = 'col-resize'
    document.body.style.userSelect = 'none'
    document.addEventListener('mousemove', onMouseMove)
    document.addEventListener('mouseup', onMouseUp)
  }, [onResize])

  return (
    <div
      className="group relative w-1 cursor-col-resize shrink-0 hover:w-1"
      onMouseDown={onMouseDown}
    >
      <div className="absolute inset-y-0 -left-1 -right-1 z-10" />
      <div className="h-full w-px bg-white/[0.04] group-hover:bg-anthropic/40 transition-colors" />
    </div>
  )
}

// ─── Task List Pane ─────────────────────────────────────────────────────────

function TaskListPane({ workflowState }: { workflowState: WorkflowState }) {
  const tasks = workflowState.tasks

  return (
    <div className="flex h-full flex-col rounded-xl glass-panel">
      <div className="flex items-center justify-between border-b border-white/[0.04] px-4 py-3">
        <span className="font-display text-sm font-semibold text-white/70">Tasks</span>
        {tasks.length > 0 && (
          <span className="font-mono text-[10px] text-white/30">
            {tasks.filter(t => t.status === 'done').length}/{tasks.length}
          </span>
        )}
      </div>
      <div className="flex-1 overflow-y-auto p-3 space-y-1.5">
        {tasks.length === 0 ? (
          <div className="flex h-full items-center justify-center">
            <p className="font-mono text-xs text-white/15">No active tasks</p>
          </div>
        ) : (
          tasks.map((task, i) => (
            <motion.div
              key={task.id}
              initial={{ opacity: 0, y: 4 }}
              animate={{ opacity: 1, y: 0 }}
              transition={{ delay: i * 0.03 }}
              className={`rounded-lg border px-3 py-2 ${
                task.status === 'running'
                  ? 'border-anthropic/25 bg-anthropic/[0.06]'
                  : task.status === 'done'
                  ? 'border-white/[0.04] bg-white/[0.02]'
                  : 'border-white/[0.03] bg-transparent'
              }`}
            >
              <div className="flex items-center gap-2">
                <div className={`h-1.5 w-1.5 rounded-full shrink-0 ${
                  task.status === 'done' ? 'bg-emerald-400' :
                  task.status === 'running' ? 'bg-anthropic' :
                  task.status === 'failed' ? 'bg-red-400' : 'bg-white/10'
                }`} />
                <span className={`font-mono text-[11px] ${
                  task.status === 'done' ? 'text-white/40' :
                  task.status === 'running' ? 'text-white/80' : 'text-white/25'
                }`}>
                  {task.title || task.name}
                </span>
                {task.duration_ms && (
                  <span className="ml-auto font-mono text-[9px] text-white/15">
                    {(task.duration_ms / 1000).toFixed(1)}s
                  </span>
                )}
              </div>
              {task.status === 'running' && task.output && (
                <div className="mt-1 font-mono text-[9px] text-white/30 line-clamp-2 pl-4">
                  {task.output}
                </div>
              )}
            </motion.div>
          ))
        )}
      </div>
    </div>
  )
}

// FilesPane placeholder removed — now uses FileTreePane component directly.

// ─── Pane Manager ───────────────────────────────────────────────────────────

interface EventBusState {
  events: AgentEvent[]
  connected: boolean
  cortexWorking: boolean
  recentTools: AgentEvent[]
  clearEvents: () => void
}

interface PaneManagerProps {
  workflowState: WorkflowState
  onViewFile?: (path: string) => void
  eventBus?: EventBusState
}

export function PaneManager({ workflowState, onViewFile: _onViewFile, eventBus }: PaneManagerProps) {
  void _onViewFile // reserved for future file preview integration
  const [panes, setPanes] = useState<PaneConfig[]>([
    createPane('chat'),
  ])
  const [widths, setWidths] = useState<number[]>([100])
  const [previewFilePath, setPreviewFilePath] = useState<string | null>(null)
  const containerRef = useRef<HTMLDivElement>(null)
  const fileEventsSeen = useRef(false)

  // ─── Auto-split: open Thalamus when Cortex starts working ───────────────
  const prevPhaseRef = useRef(false)
  useEffect(() => {
    const wasIdle = !prevPhaseRef.current
    const nowWorking = eventBus?.cortexWorking ?? (workflowState.phase === 'executing' || workflowState.phase === 'planning')
    const hasThalamus = panes.some(p => p.type === 'thalamus')

    // Auto-open Thalamus when work begins
    if (wasIdle && nowWorking && !hasThalamus) {
      setPanes(prev => {
        const newPanes = [...prev, createPane('thalamus')]
        setWidths([58, 42]) // Cortex gets more space
        return newPanes
      })
    }

    // Auto-close Thalamus when work completes (optional — user can pin it)
    // Uncomment if you want auto-collapse:
    // if (workflowState.phase === 'complete' && hasThalamus) {
    //   removePane(panes.find(p => p.type === 'thalamus')?.id || '')
    // }

    prevPhaseRef.current = eventBus?.cortexWorking ?? (workflowState.phase !== 'idle')
  }, [workflowState.phase, eventBus?.cortexWorking, panes])

  // ─── Auto-split: open file tree + code preview on file events ───────────
  useEffect(() => {
    if (fileEventsSeen.current) return
    const events: AgentEvent[] = (eventBus?.events || [])
    const hasFileEvent = events.some(e => e.type === 'file_read' || e.type === 'file_write')
    if (!hasFileEvent) return

    const hasFiles = panes.some(p => p.type === 'files')
    if (hasFiles) return

    fileEventsSeen.current = true
    setPanes(prev => {
      const filePane = createPane('files')
      const newPanes = [filePane, ...prev]
      setWidths([18, ...prev.map(() => 82 / prev.length)])
      return newPanes
    })
  }, [eventBus?.events, panes])

  const addPane = useCallback((type: PaneType) => {
    setPanes((prev) => {
      const newPanes = [...prev, createPane(type)]
      // Redistribute widths equally
      const equalWidth = 100 / newPanes.length
      setWidths(newPanes.map(() => equalWidth))
      return newPanes
    })
  }, [])

  const removePane = useCallback((id: string) => {
    setPanes((prev) => {
      if (prev.length <= 1) return prev // keep at least one pane
      const newPanes = prev.filter((p) => p.id !== id)
      const equalWidth = 100 / newPanes.length
      setWidths(newPanes.map(() => equalWidth))
      return newPanes
    })
  }, [])

  const handleResize = useCallback((index: number, delta: number) => {
    if (!containerRef.current) return
    const containerWidth = containerRef.current.offsetWidth
    const deltaPct = (delta / containerWidth) * 100

    setWidths((prev) => {
      const next = [...prev]
      const minWidth = 15 // minimum 15% per pane
      const left = next[index]
      const right = next[index + 1]
      if (left === undefined || right === undefined) return next
      const newLeft = left + deltaPct
      const newRight = right - deltaPct
      if (newLeft >= minWidth && newRight >= minWidth) {
        next[index] = newLeft
        next[index + 1] = newRight
      }
      return next
    })
  }, [])

  const renderPaneContent = (pane: PaneConfig) => {
    switch (pane.type) {
      case 'chat':
        return <ChatPane workflowState={workflowState} eventBus={eventBus} />
      case 'cortex':
        return <ChatPane workflowState={workflowState} eventBus={eventBus} />
      case 'thalamus':
        return <ThalamusPane workflowState={workflowState} onClose={() => removePane(pane.id)} />
      case 'tasks':
        return <TaskListPane workflowState={workflowState} />
      case 'history':
        return <WorkflowHistory activeID={workflowState.id} onSelect={() => {}} />
      case 'files':
        return <FileTreePane onClose={() => removePane(pane.id)} onSelectFile={(path) => setPreviewFilePath(path)} />
      case 'code_preview':
        return <CodePreviewPane filePath={previewFilePath} onClose={() => removePane(pane.id)} />
      default:
        return null
    }
  }

  // Which pane types are not yet open
  const openTypes = new Set(panes.map((p) => p.type))
  const availableTypes = (Object.keys(PANE_TEMPLATES) as PaneType[]).filter(
    (t) => !openTypes.has(t)
  )

  return (
    <div className="flex h-full flex-col">
      {/* Pane tab bar */}
      <div className="flex items-center gap-1 border-b border-white/[0.04] bg-surface-900/40 px-2 py-1">
        {panes.map((pane) => (
          <div
            key={pane.id}
            className="flex items-center gap-1.5 rounded-md bg-white/[0.04] px-2.5 py-1 group"
          >
            <span className="text-[10px]">{pane.icon}</span>
            <span className="font-mono text-[10px] text-white/50">{pane.label}</span>
            {panes.length > 1 && (
              <button
                onClick={() => removePane(pane.id)}
                className="ml-0.5 font-mono text-[10px] text-white/15 hover:text-white/50 transition-colors opacity-0 group-hover:opacity-100"
              >
                ×
              </button>
            )}
          </div>
        ))}

        {/* Add pane dropdown */}
        {availableTypes.length > 0 && (
          <div className="relative group ml-auto">
            <button className="rounded-md px-2 py-1 font-mono text-[10px] text-white/20 hover:text-white/50 hover:bg-white/[0.04] transition-all">
              + Split
            </button>
            <div className="absolute right-0 top-full mt-1 hidden group-hover:block z-20">
              <div className="rounded-lg border border-white/[0.06] bg-surface-800 p-1 shadow-xl">
                {availableTypes.map((type) => (
                  <button
                    key={type}
                    onClick={() => addPane(type)}
                    className="flex items-center gap-2 w-full rounded-md px-3 py-1.5 font-mono text-[10px] text-white/50 hover:bg-white/[0.06] hover:text-white/80 transition-colors whitespace-nowrap"
                  >
                    <span>{PANE_TEMPLATES[type].icon}</span>
                    <span>{PANE_TEMPLATES[type].label}</span>
                  </button>
                ))}
              </div>
            </div>
          </div>
        )}
      </div>

      {/* Pane content area */}
      <div ref={containerRef} className="flex flex-1 min-h-0 gap-0 p-2">
        <AnimatePresence mode="popLayout">
          {panes.map((pane, i) => (
            <motion.div
              key={pane.id}
              layout
              initial={{ opacity: 0, scale: 0.95 }}
              animate={{ opacity: 1, scale: 1 }}
              exit={{ opacity: 0, scale: 0.95 }}
              transition={{ duration: 0.15 }}
              className="flex min-w-0 min-h-0"
              style={{ width: `${widths[i]}%` }}
            >
              <div className="flex-1 min-w-0 min-h-0 px-1">
                {renderPaneContent(pane)}
              </div>
              {/* Resizer between panes */}
              {i < panes.length - 1 && (
                <Resizer onResize={(delta) => handleResize(i, delta)} />
              )}
            </motion.div>
          ))}
        </AnimatePresence>
      </div>
    </div>
  )
}
