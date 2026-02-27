import { useCallback, useState, useEffect, useRef } from 'react'
import { AnimatePresence, motion } from 'framer-motion'
import { CommandBar } from './components/CommandBar'
import { PlannerPanel } from './components/PlannerPanel'
import { ExecutorPanel } from './components/ExecutorPanel'
import { HandoffAnimation } from './components/HandoffAnimation'
import { WorkflowHistory } from './components/WorkflowHistory'
import { MetricsStrip } from './components/MetricsStrip'
import { OutputPreview } from './components/OutputPreview'
import { MemoryPanel } from './components/MemoryPanel'
import { useSSE } from './hooks/useSSE'
import { useWorkflow } from './hooks/useWorkflow'
import { useMetrics } from './hooks/useMetrics'
import { submitPlan, sendChat } from './lib/api'
import type { SSEEvent } from './lib/types'

export default function App() {
  // Active workflow (displayed in the panels).
  const { state, startPlan, handleSSEEvent, setPhase } = useWorkflow()
  const [showHandoff, setShowHandoff] = useState(false)
  const [reconnecting, setReconnecting] = useState(false)
  const metrics = useMetrics()

  // Track all workflow IDs so we can switch between them.
  const [allWorkflowIDs, setAllWorkflowIDs] = useState<string[]>([])

  // Quick chat mode (non-plan: messages).
  const [quickChatMode, setQuickChatMode] = useState(false)
  const [chatMessages, setChatMessages] = useState<{role: 'user' | 'assistant'; content: string}[]>([])
  const [isChatting, setIsChatting] = useState(false)
  const chatEndRef = useRef<HTMLDivElement>(null)

  // Output file preview drawer.
  const [previewFile, setPreviewFile] = useState<string | null>(null)

  // Memory panel (Sprint 12).
  const [memoryOpen, setMemoryOpen] = useState(false)

  // SSE connection.
  const { connected } = useSSE({
    workflowID: state.id || null,
    onEvent: (evt: SSEEvent) => {
      handleSSEEvent(evt)
      if (evt.source === 'planner' && evt.type === 'handoff') {
        setShowHandoff(true)
      }
    },
  })

  // Track connection state for reconnecting banner.
  const prevConnected = useRef(connected)
  useEffect(() => {
    if (!connected && prevConnected.current) {
      setReconnecting(true)
    }
    if (connected && !prevConnected.current) {
      setReconnecting(false)
    }
    prevConnected.current = connected
  }, [connected])

  // Keyboard shortcuts.
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      // Cmd+1/2 — switch between workflows.
      if ((e.metaKey || e.ctrlKey) && e.key === '1' && allWorkflowIDs[0]) {
        e.preventDefault()
        handleSelectWorkflow(allWorkflowIDs[0])
      }
      if ((e.metaKey || e.ctrlKey) && e.key === '2' && allWorkflowIDs[1]) {
        e.preventDefault()
        handleSelectWorkflow(allWorkflowIDs[1])
      }
      // Esc — exit quick chat mode.
      if (e.key === 'Escape') {
        setQuickChatMode(false)
      }
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [allWorkflowIDs]) // eslint-disable-line react-hooks/exhaustive-deps

  const handleSubmit = useCallback(async (goal: string) => {
    setQuickChatMode(false)

    try {
      const res = await submitPlan(goal)
      startPlan(res.workflow_id, goal)
      setAllWorkflowIDs((prev) => [res.workflow_id, ...prev.filter((id) => id !== res.workflow_id)])
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Unknown error'
      startPlan('error', goal)
      handleSSEEvent({
        workflow_id: 'error',
        source: 'planner',
        type: 'error',
        payload: message,
      })
    }
  }, [startPlan, handleSSEEvent])

  const handleChat = useCallback(async (message: string) => {
    setQuickChatMode(true)
    setChatMessages((prev) => [...prev, { role: 'user', content: message }])
    setIsChatting(true)

    try {
      const res = await sendChat(message)
      setChatMessages((prev) => [...prev, { role: 'assistant', content: res.reply }])
    } catch (err) {
      const errMsg = err instanceof Error ? err.message : 'Unknown error'
      setChatMessages((prev) => [...prev, { role: 'assistant', content: `Error: ${errMsg}` }])
    } finally {
      setIsChatting(false)
    }
  }, [])

  // Auto-scroll chat.
  useEffect(() => {
    chatEndRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [chatMessages])

  const handleHandoffComplete = useCallback(() => {
    setShowHandoff(false)
    setPhase('executing')
  }, [setPhase])

  const handleSelectWorkflow = useCallback((_id: string) => {
    // Switching workflows would reload state from the API.
    // For now, this is wired but the full reload is deferred.
  }, [])

  const handleViewFile = useCallback((path: string) => {
    setPreviewFile(path)
  }, [])

  const isPlanning = state.phase === 'planning'

  return (
    <div className="flex h-screen flex-col bg-surface-900">
      {/* Reconnecting banner */}
      <AnimatePresence>
        {reconnecting && (
          <motion.div
            initial={{ height: 0, opacity: 0 }}
            animate={{ height: 'auto', opacity: 1 }}
            exit={{ height: 0, opacity: 0 }}
            className="overflow-hidden bg-executor/10 px-6 py-1.5 text-center"
          >
            <span className="font-mono text-xs text-executor">
              ⟳ Reconnecting to stream...
            </span>
          </motion.div>
        )}
      </AnimatePresence>

      {/* Top nav */}
      <header className="flex items-center justify-between border-b border-surface-600 px-6 py-3">
        <div className="flex items-center gap-3">
          <h1 className="font-display text-lg font-bold tracking-tight text-gray-100">
            ZBOT
          </h1>
          <div className="flex gap-1">
            <span className="h-1.5 w-1.5 rounded-full bg-planner" />
            <span className="h-1.5 w-1.5 rounded-full bg-executor" />
            <span className="h-1.5 w-1.5 rounded-full bg-green-400" />
          </div>
          {allWorkflowIDs.length > 0 && (
            <span className="font-mono text-xs text-gray-500">
              active workflows: {allWorkflowIDs.length}
            </span>
          )}
        </div>
        <div className="flex items-center gap-4">
          {state.phase !== 'idle' && (
            <span className="font-mono text-xs text-gray-500">
              workflow: {state.id.slice(0, 8)}
            </span>
          )}
          <button
            onClick={() => setMemoryOpen(true)}
            className="rounded p-1 font-mono text-xs text-gray-500 transition-colors hover:bg-surface-700 hover:text-amber-400"
            title="Memory panel"
          >
            🧠
          </button>
          <a href="/workflows" className="font-mono text-xs text-gray-500 transition-colors hover:text-gray-300">
            history
          </a>
          <a href="/audit" className="font-mono text-xs text-gray-500 transition-colors hover:text-gray-300">
            audit
          </a>
        </div>
      </header>

      {/* Command Bar */}
      <div className="border-b border-surface-600 px-6 py-4">
        <CommandBar
          onSubmit={(goal) => void handleSubmit(goal)}
          onChat={(msg) => void handleChat(msg)}
          isPlanning={isPlanning}
          isChatting={isChatting}
        />
      </div>

      {/* Metrics strip */}
      <div className="border-b border-surface-700">
        <MetricsStrip metrics={metrics} />
      </div>

      {/* Main content area */}
      <div className="relative flex flex-1 overflow-hidden">
        {quickChatMode ? (
          /* Quick Chat Mode — full width */
          <motion.div
            className="flex-1 overflow-y-auto p-6"
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
          >
            <div className="mx-auto max-w-2xl space-y-4">
              {chatMessages.map((msg, i) => (
                <motion.div
                  key={i}
                  initial={{ opacity: 0, y: 10 }}
                  animate={{ opacity: 1, y: 0 }}
                  className={`rounded-lg border p-4 ${
                    msg.role === 'user'
                      ? 'border-surface-600 bg-surface-800'
                      : 'border-amber-500/20 bg-amber-500/5'
                  }`}
                >
                  <div className="mb-1 font-mono text-[10px] text-gray-600">
                    {msg.role === 'user' ? 'You' : 'ZBOT'}
                  </div>
                  <div className="whitespace-pre-wrap font-mono text-xs text-gray-200">
                    {msg.content}
                  </div>
                </motion.div>
              ))}
              {isChatting && (
                <motion.div
                  initial={{ opacity: 0 }}
                  animate={{ opacity: 1 }}
                  className="rounded-lg border border-amber-500/20 bg-amber-500/5 p-4"
                >
                  <div className="mb-1 font-mono text-[10px] text-gray-600">ZBOT</div>
                  <div className="font-mono text-xs text-amber-400 animate-pulse">Thinking...</div>
                </motion.div>
              )}
              <div ref={chatEndRef} />
              {chatMessages.length === 0 && !isChatting && (
                <div className="flex flex-col items-center justify-center py-16 text-center">
                  <span className="text-2xl opacity-40">💬</span>
                  <p className="mt-3 font-mono text-xs text-gray-600">
                    Type a message to chat with ZBOT — memory-aware across sessions
                  </p>
                  <p className="mt-1 font-mono text-[10px] text-gray-700">
                    Use &quot;plan: your goal&quot; to switch to dual-brain mode
                  </p>
                </div>
              )}
            </div>
          </motion.div>
        ) : (
          /* Dual Brain Mode — split screen */
          <>
            <AnimatePresence>
              {/* Left: Planner */}
              <motion.div
                className="flex w-1/2 flex-col p-4 max-md:w-full"
                initial={{ opacity: 0 }}
                animate={{
                  opacity: state.phase === 'handoff' || state.phase === 'executing' || state.phase === 'complete' ? 0.7 : 1,
                }}
                transition={{ duration: 0.5 }}
              >
                <PlannerPanel
                  tokens={state.plannerTokens}
                  tasks={state.plannedTasks}
                  phase={state.phase}
                />
              </motion.div>

              {/* Divider */}
              <div className="w-px bg-surface-600 max-md:hidden" />

              {/* Right: Executor */}
              <motion.div
                className="flex w-1/2 flex-col p-4 max-md:w-full"
                initial={{ opacity: 0 }}
                animate={{ opacity: 1 }}
                transition={{ duration: 0.3 }}
              >
                <ExecutorPanel tasks={state.tasks} phase={state.phase} onViewFile={handleViewFile} />
              </motion.div>
            </AnimatePresence>

            {/* Handoff animation overlay */}
            <HandoffAnimation active={showHandoff} onComplete={handleHandoffComplete} />
          </>
        )}
      </div>

      {/* Bottom history bar */}
      <div className="border-t border-surface-600">
        <WorkflowHistory activeID={state.id} onSelect={handleSelectWorkflow} />
      </div>

      {/* Output file preview drawer */}
      <OutputPreview filePath={previewFile} onClose={() => setPreviewFile(null)} />

      {/* Memory panel (Sprint 12) */}
      <MemoryPanel open={memoryOpen} onClose={() => setMemoryOpen(false)} />
    </div>
  )
}
