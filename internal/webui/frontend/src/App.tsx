import { useCallback, useState, useEffect, useRef } from 'react'
import { AnimatePresence, motion } from 'framer-motion'
import { CommandBar } from './components/CommandBar'
import { PaneManager } from './components/PaneManager'
import { MetricsStrip } from './components/MetricsStrip'
import { OutputPreview } from './components/OutputPreview'
import { MemoryPanel } from './components/MemoryPanel'
import { WorkspacePanel } from './components/WorkspacePanel'
import { FilePreviewDrawer } from './components/FilePreviewDrawer'
import { SchedulePanel } from './components/SchedulePanel'
import { ResearchPanel } from './components/ResearchPanel'
import { Sidebar } from './components/Sidebar'
import { DashboardPage } from './components/DashboardPage'
import { KnowledgeBasePage } from './components/KnowledgeBasePage'
import { useSSE } from './hooks/useSSE'
import { useEventBus } from './hooks/useEventBus'
import { useWorkflow } from './hooks/useWorkflow'
import { useMetrics } from './hooks/useMetrics'
import { submitPlan } from './lib/api'
import type { NavPage } from './components/Sidebar'
import type { SSEEvent } from './lib/types'

export default function App() {
  const { state, startPlan, handleSSEEvent } = useWorkflow()
  const [reconnecting, setReconnecting] = useState(false)
  const metrics = useMetrics()
  const eventBus = useEventBus({ sessionID: 'web-chat' })

  // Event bus passed directly to PaneManager → ChatPane as props (no window globals)

  const [activePage, setActivePage] = useState<NavPage>('workflows')

  const [previewFile, setPreviewFile] = useState<string | null>(null)
  const [memoryOpen, setMemoryOpen] = useState(false)
  const [workspaceOpen, setWorkspaceOpen] = useState(false)
  const [wsPreviewFile, setWsPreviewFile] = useState<string | null>(null)
  const [scheduleOpen, setScheduleOpen] = useState(false)
  const [researchOpen, setResearchOpen] = useState(false)

  const { connected } = useSSE({
    workflowID: state.id || null,
    onEvent: (evt: SSEEvent) => {
      handleSSEEvent(evt)
    },
  })

  const prevConnected = useRef(connected)
  useEffect(() => {
    if (!connected && prevConnected.current) setReconnecting(true)
    if (connected && !prevConnected.current) setReconnecting(false)
    prevConnected.current = connected
  }, [connected])

  const handleSubmit = useCallback(async (goal: string) => {
    setActivePage('workflows')
    try {
      const res = await submitPlan(goal)
      startPlan(res.workflow_id, goal)
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Unknown error'
      startPlan('error', goal)
      handleSSEEvent({ workflow_id: 'error', source: 'agent', type: 'error', payload: message })
    }
  }, [startPlan, handleSSEEvent])

  const workflowActive = state.phase !== 'idle'

  return (
    <div className="flex h-screen overflow-hidden bg-surface-950 text-white">

      {/* Collapsible icon sidebar */}
      <Sidebar
        activePage={activePage}
        onNavigate={(page) => {
          if (page === 'memory')    { setMemoryOpen(true);   return }
          if (page === 'schedules') { setScheduleOpen(true); return }
          if (page === 'research')  { setResearchOpen(true); return }
          setActivePage(page)
        }}
        workflowActive={workflowActive}
      />

      {/* Main column */}
      <div className="flex flex-1 flex-col min-w-0 overflow-hidden">

        {/* Reconnect banner */}
        <AnimatePresence>
          {reconnecting && (
            <motion.div
              initial={{ height: 0 }} animate={{ height: 32 }} exit={{ height: 0 }}
              className="flex items-center justify-center overflow-hidden bg-anthropic/10 border-b border-anthropic/20"
            >
              <span className="font-mono text-[11px] text-anthropic/80">
                <motion.span animate={{ opacity: [1, 0.3, 1] }} transition={{ duration: 1.2, repeat: Infinity }}>⟳</motion.span>
                {' '}Reconnecting to event stream
              </span>
            </motion.div>
          )}
        </AnimatePresence>

        {/* Top chrome: command bar + metrics */}
        <div className="shrink-0 border-b border-white/[0.04] bg-surface-900/60 backdrop-blur-sm">
          <div className="px-4 py-2.5">
            <CommandBar
              onSubmit={(goal) => void handleSubmit(goal)}
              onChat={() => setActivePage('workflows')}
              onResearch={() => setResearchOpen(true)}
              isPlanning={state.phase === 'planning' || state.phase === 'executing'}
              isChatting={false}
            />
          </div>
          <div className="flex items-center">
            <MetricsStrip metrics={metrics} />
            <div className="flex items-center gap-3 px-4 py-2 ml-auto border-l border-white/[0.03]">
              <div className="flex items-center gap-1.5">
                <span className={`h-1.5 w-1.5 rounded-full ${eventBus.cortexWorking ? 'bg-cyan-400 animate-pulse' : eventBus.connected ? 'bg-emerald-400' : 'bg-red-400'}`} />
                <span className="font-mono text-[9px] text-white/30">
                  {eventBus.cortexWorking ? 'Cortex working' : eventBus.connected ? 'Cortex online' : 'disconnected'}
                </span>
              </div>
              {eventBus.recentTools.length > 0 && eventBus.cortexWorking && (
                <span className="font-mono text-[9px] text-cyan-400/50">
                  {eventBus.recentTools[eventBus.recentTools.length - 1]?.summary}
                </span>
              )}
            </div>
          </div>
        </div>

        {/* Pages */}
        <div className="relative flex flex-1 min-h-0 overflow-hidden">
          <AnimatePresence mode="wait">

            {activePage === 'dashboard' && (
              <motion.div key="dash" className="absolute inset-0 overflow-auto"
                initial={{ opacity: 0, y: 6 }} animate={{ opacity: 1, y: 0 }} exit={{ opacity: 0, y: -6 }}
                transition={{ duration: 0.15 }}>
                <DashboardPage onNavigate={(p) => setActivePage(p as NavPage)} />
              </motion.div>
            )}

            {activePage === 'workflows' && (
              <motion.div key="wf" className="absolute inset-0"
                initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }}
                transition={{ duration: 0.1 }}>

                {/* v2: Dynamic split-pane layout replacing fixed 3-panel */}
                <PaneManager
                  workflowState={state}
                  onViewFile={setPreviewFile}
                  eventBus={eventBus}
                />
              </motion.div>
            )}

            {activePage === 'knowledge' && (
              <motion.div key="kb" className="absolute inset-0 overflow-auto"
                initial={{ opacity: 0, y: 6 }} animate={{ opacity: 1, y: 0 }} exit={{ opacity: 0, y: -6 }}
                transition={{ duration: 0.15 }}>
                <KnowledgeBasePage />
              </motion.div>
            )}

          </AnimatePresence>
        </div>
      </div>

      {/* Drawers */}
      <OutputPreview filePath={previewFile} onClose={() => setPreviewFile(null)} />
      <MemoryPanel open={memoryOpen} onClose={() => setMemoryOpen(false)} />
      <WorkspacePanel open={workspaceOpen} onClose={() => setWorkspaceOpen(false)} onPreview={(path) => { setWsPreviewFile(path); setWorkspaceOpen(false) }} />
      <FilePreviewDrawer filePath={wsPreviewFile} onClose={() => setWsPreviewFile(null)} />
      <SchedulePanel open={scheduleOpen} onClose={() => setScheduleOpen(false)} />
      <ResearchPanel open={researchOpen} onClose={() => setResearchOpen(false)} />
    </div>
  )
}
