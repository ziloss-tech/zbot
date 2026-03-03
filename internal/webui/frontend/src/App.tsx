import { useCallback, useState, useEffect, useRef } from 'react'
import { AnimatePresence, motion } from 'framer-motion'
import { CommandBar } from './components/CommandBar'
import { PlannerPanel } from './components/PlannerPanel'
import { ExecutorPanel } from './components/ExecutorPanel'
import { HandoffAnimation } from './components/HandoffAnimation'
import { MetricsStrip } from './components/MetricsStrip'
import { OutputPreview } from './components/OutputPreview'
import { MemoryPanel } from './components/MemoryPanel'
import { WorkspacePanel } from './components/WorkspacePanel'
import { FilePreviewDrawer } from './components/FilePreviewDrawer'
import { SchedulePanel } from './components/SchedulePanel'
import { ResearchPanel } from './components/ResearchPanel'
import { Sidebar } from './components/Sidebar'
import { ObserverPanel } from './components/ObserverPanel'
import { DashboardPage } from './components/DashboardPage'
import { KnowledgeBasePage } from './components/KnowledgeBasePage'
import { useSSE } from './hooks/useSSE'
import { useWorkflow } from './hooks/useWorkflow'
import { useMetrics } from './hooks/useMetrics'
import { submitPlan } from './lib/api'
import type { NavPage } from './components/Sidebar'
import type { SSEEvent } from './lib/types'

type PanelFocus = 'balanced' | 'planner' | 'executor' | 'observer'

function panelWidths(focus: PanelFocus, observerExpanded: boolean): [string, string, string] {
  if (observerExpanded) return ['14%', '18%', '68%']
  switch (focus) {
    case 'planner':  return ['46%', '30%', '24%']
    case 'executor': return ['20%', '54%', '26%']
    case 'observer': return ['20%', '20%', '60%']
    default:         return ['33%', '40%', '27%']
  }
}

export default function App() {
  const { state, startPlan, handleSSEEvent, setPhase } = useWorkflow()
  const [showHandoff, setShowHandoff] = useState(false)
  const [reconnecting, setReconnecting] = useState(false)
  const metrics = useMetrics()

  const [activePage, setActivePage] = useState<NavPage>('workflows')
  const [panelFocus, setPanelFocus] = useState<PanelFocus>('balanced')
  const [observerExpanded, setObserverExpanded] = useState(false)

  const [previewFile, setPreviewFile] = useState<string | null>(null)
  const [memoryOpen, setMemoryOpen] = useState(false)
  const [workspaceOpen, setWorkspaceOpen] = useState(false)
  const [wsPreviewFile, setWsPreviewFile] = useState<string | null>(null)
  const [scheduleOpen, setScheduleOpen] = useState(false)
  const [researchOpen, setResearchOpen] = useState(false)

  const activeTask = state.tasks.find((t) => t.status === 'running') ?? null

  const { connected } = useSSE({
    workflowID: state.id || null,
    onEvent: (evt: SSEEvent) => {
      handleSSEEvent(evt)
      if (evt.source === 'planner' && evt.type === 'handoff') setShowHandoff(true)
    },
  })

  useEffect(() => {
    if (state.phase === 'planning') setPanelFocus('planner')
    else if (state.phase === 'executing') setPanelFocus('executor')
    else if (state.phase === 'complete' || state.phase === 'idle') setPanelFocus('balanced')
  }, [state.phase])

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
      handleSSEEvent({ workflow_id: 'error', source: 'planner', type: 'error', payload: message })
    }
  }, [startPlan, handleSSEEvent])

  const handleHandoffComplete = useCallback(() => {
    setShowHandoff(false)
    setPhase('executing')
  }, [setPhase])

  const [plannerW, executorW, observerW] = panelWidths(panelFocus, observerExpanded)
  const workflowActive = state.phase !== 'idle'

  return (
    // Full bleed dark canvas
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

        {/* Reconnect banner — minimal */}
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

        {/* Top chrome: command bar + metrics — tight */}
        <div className="shrink-0 border-b border-white/[0.04] bg-surface-900/60 backdrop-blur-sm">
          <div className="px-4 py-2.5">
            <CommandBar
              onSubmit={(goal) => void handleSubmit(goal)}
              onChat={() => setActivePage('workflows')}
              onResearch={() => setResearchOpen(true)}
              isPlanning={state.phase === 'planning'}
              isChatting={false}
            />
          </div>
          <MetricsStrip metrics={metrics} />
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
              <motion.div key="wf" className="absolute inset-0 flex gap-2 p-2.5"
                initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }}
                transition={{ duration: 0.1 }}>

                {/* GPT-4o Planner */}
                <motion.div
                  className="flex min-w-0 flex-col"
                  animate={{ width: plannerW }}
                  transition={{ type: 'spring', damping: 26, stiffness: 220 }}
                  onClick={() => setPanelFocus('planner')}
                  style={{ cursor: 'pointer' }}
                >
                  <PlannerPanel tokens={state.plannerTokens} tasks={state.plannedTasks} phase={state.phase} />
                </motion.div>

                {/* Claude Executor */}
                <motion.div
                  className="flex min-w-0 flex-col"
                  animate={{ width: executorW }}
                  transition={{ type: 'spring', damping: 26, stiffness: 220 }}
                  onClick={() => setPanelFocus('executor')}
                  style={{ cursor: 'pointer' }}
                >
                  <ExecutorPanel tasks={state.tasks} phase={state.phase} onViewFile={setPreviewFile} />
                </motion.div>

                {/* Observer (Claude) */}
                <motion.div
                  className="flex min-w-0 flex-col"
                  animate={{ width: observerW }}
                  transition={{ type: 'spring', damping: 26, stiffness: 220 }}
                  onClick={() => setPanelFocus('observer')}
                  style={{ cursor: 'pointer' }}
                >
                  <ObserverPanel
                    workflowState={state}
                    activeTask={activeTask}
                    isExpanded={observerExpanded}
                    onExpandToggle={() => setObserverExpanded((v) => !v)}
                  />
                </motion.div>

                <HandoffAnimation active={showHandoff} onComplete={handleHandoffComplete} />
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
