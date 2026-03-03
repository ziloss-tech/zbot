import { useState, useEffect, useRef, useCallback } from 'react'
import { motion, AnimatePresence } from 'framer-motion'
import { startResearch, fetchResearchSessions, fetchResearchBudget } from '../lib/api'
import type { ResearchEvent, ResearchSession, ResearchBudget } from '../lib/types'

// ─── MODEL COLOR PALETTE ───────────────────────────────────────────────────

const MODEL_COLORS: Record<string, { bg: string; text: string; accent: string; label: string }> = {
  'mistralai/mistral-large':            { bg: 'bg-orange-500/10', text: 'text-orange-400', accent: '#f97316', label: 'Mistral AI' },
  'meta-llama/llama-4-scout':           { bg: 'bg-blue-500/10',   text: 'text-blue-400',   accent: '#3b82f6', label: 'Meta' },
  'meta-llama/llama-3.1-405b-instruct': { bg: 'bg-blue-500/10',   text: 'text-blue-400',   accent: '#3b82f6', label: 'Meta' },
  'gpt-4o':                             { bg: 'bg-emerald-500/10', text: 'text-emerald-400', accent: '#10b981', label: 'OpenAI' },
  'claude-sonnet-4-6':                  { bg: 'bg-amber-500/10',  text: 'text-amber-400',  accent: '#d97706', label: 'Anthropic' },
}

const STAGE_ICONS: Record<string, string> = {
  planning: '🧠', searching: '🔍', extracting: '📋',
  critiquing: '⚖️', evaluated: '✅', synthesizing: '✍️',
  complete: '📄', error: '❌',
}

function getModelStyle(modelId: string) {
  return MODEL_COLORS[modelId] || { bg: 'bg-gray-500/10', text: 'text-gray-400', accent: '#6b7280', label: 'AI' }
}

interface Props {
  open: boolean
  onClose: () => void
}

export function ResearchPanel({ open, onClose }: Props) {
  const [goal, setGoal] = useState('')
  const [activeSessionID, setActiveSessionID] = useState<string | null>(null)
  const [events, setEvents] = useState<ResearchEvent[]>([])
  const [sessions, setSessions] = useState<ResearchSession[]>([])
  const [budget, setBudget] = useState<ResearchBudget | null>(null)
  const [isStarting, setIsStarting] = useState(false)
  const [error, setError] = useState('')
  const [report, setReport] = useState('')
  const [tab, setTab] = useState<'conversation' | 'report' | 'history'>('conversation')

  const eventsEndRef = useRef<HTMLDivElement>(null)
  const eventSourceRef = useRef<EventSource | null>(null)

  // Load sessions + budget when panel opens.
  useEffect(() => {
    if (!open) return
    fetchResearchSessions().then(setSessions).catch(() => {})
    fetchResearchBudget().then(setBudget).catch(() => {})
  }, [open])

  // Auto-scroll events.
  useEffect(() => {
    eventsEndRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [events])

  // SSE connection for active session.
  const connectSSE = useCallback((sessionID: string) => {
    // Close existing connection.
    if (eventSourceRef.current) {
      eventSourceRef.current.close()
    }

    const es = new EventSource(`/api/research/stream/${sessionID}`)
    eventSourceRef.current = es

    es.onmessage = (e) => {
      try {
        const evt: ResearchEvent = JSON.parse(e.data)

        if (evt.stage === 'stream_end' || evt.stage === 'done') {
          es.close()
          fetchResearchSessions().then(setSessions).catch(() => {})
          fetchResearchBudget().then(setBudget).catch(() => {})
          return
        }

        if (evt.stage === 'complete' && evt.report) {
          setReport(evt.report)
          setTab('report')
        }

        setEvents((prev) => [...prev, evt])
      } catch { /* ignore parse errors */ }
    }

    es.onerror = () => {
      es.close()
    }
  }, [])

  // Cleanup SSE on unmount.
  useEffect(() => {
    return () => {
      if (eventSourceRef.current) {
        eventSourceRef.current.close()
      }
    }
  }, [])

  const handleStart = async () => {
    if (!goal.trim() || isStarting) return
    setIsStarting(true)
    setError('')
    setEvents([])
    setReport('')
    setTab('conversation')

    try {
      const res = await startResearch(goal.trim())
      setActiveSessionID(res.session_id)
      connectSSE(res.session_id)
      setGoal('')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to start research')
    } finally {
      setIsStarting(false)
    }
  }

  const handleViewSession = (sess: ResearchSession) => {
    setActiveSessionID(sess.id)
    setEvents([])
    if (sess.final_report) {
      setReport(sess.final_report)
      setTab('report')
    } else if (sess.status === 'running') {
      setTab('conversation')
      connectSSE(sess.id)
    }
  }

  // Confidence meter — visual ring.
  const latestConfidence = events.length > 0
    ? events.filter((e) => e.confidence > 0).pop()?.confidence || 0
    : 0

  const currentCost = events.length > 0
    ? events[events.length - 1].cost_usd || 0
    : 0

  if (!open) return null

  return (
    <AnimatePresence>
      <motion.div
        initial={{ opacity: 0 }}
        animate={{ opacity: 1 }}
        exit={{ opacity: 0 }}
        className="fixed inset-0 z-50 flex items-start justify-center bg-black/60 pt-8"
        onClick={onClose}
      >
        <motion.div
          initial={{ scale: 0.95, opacity: 0, y: -10 }}
          animate={{ scale: 1, opacity: 1, y: 0 }}
          exit={{ scale: 0.95, opacity: 0, y: -10 }}
          className="mx-4 flex h-[85vh] w-full max-w-4xl flex-col rounded-lg border border-surface-600 bg-surface-800 shadow-2xl"
          onClick={(e) => e.stopPropagation()}
        >
          {/* Header */}
          <div className="flex items-center justify-between border-b border-surface-600 px-6 py-4">
            <div className="flex items-center gap-3">
              <span className="text-xl">🔬</span>
              <h2 className="font-display text-sm font-bold text-gray-100">Deep Research</h2>
              <span className="font-mono text-[10px] text-gray-600">5 AI models • iterative verification</span>
            </div>
            <div className="flex items-center gap-4">
              {/* Budget bar */}
              {budget && (
                <div className="flex items-center gap-2">
                  <div className="h-1.5 w-24 overflow-hidden rounded-full bg-surface-700">
                    <div
                      className="h-full rounded-full transition-all"
                      style={{
                        width: `${Math.min(100, (budget.today_spent_usd / budget.daily_limit_usd) * 100)}%`,
                        background: budget.today_spent_usd / budget.daily_limit_usd > 0.8 ? '#ef4444' : '#10b981',
                      }}
                    />
                  </div>
                  <span className="font-mono text-[10px] text-gray-500">
                    ${budget.today_spent_usd.toFixed(2)} / ${budget.daily_limit_usd.toFixed(2)}
                  </span>
                </div>
              )}
              <button
                onClick={onClose}
                className="rounded p-1 text-gray-500 transition-colors hover:bg-surface-700 hover:text-gray-300"
              >
                ✕
              </button>
            </div>
          </div>

          {/* Goal input */}
          <div className="border-b border-surface-700 px-6 py-3">
            <form
              onSubmit={(e) => {
                e.preventDefault()
                void handleStart()
              }}
              className="flex gap-2"
            >
              <input
                type="text"
                value={goal}
                onChange={(e) => setGoal(e.target.value)}
                placeholder="What should 5 AI models research for you?"
                disabled={isStarting}
                className="flex-1 rounded border border-surface-600 bg-surface-900 px-4 py-2 font-mono text-xs text-gray-200 placeholder-gray-600 outline-none focus:border-cyan-500/50"
              />
              <button
                type="submit"
                disabled={!goal.trim() || isStarting}
                className="rounded bg-cyan-600 px-5 py-2 font-mono text-xs font-bold text-white transition-opacity disabled:opacity-40 hover:bg-cyan-500"
              >
                {isStarting ? 'Starting...' : 'Research'}
              </button>
            </form>
            {error && (
              <p className="mt-2 font-mono text-xs text-red-400">{error}</p>
            )}
          </div>

          {/* Tab bar */}
          <div className="flex gap-1 border-b border-surface-700 px-6">
            {(['conversation', 'report', 'history'] as const).map((t) => (
              <button
                key={t}
                onClick={() => setTab(t)}
                className={`border-b-2 px-4 py-2 font-mono text-xs transition-colors ${
                  tab === t
                    ? 'border-cyan-500 text-cyan-400'
                    : 'border-transparent text-gray-500 hover:text-gray-300'
                }`}
              >
                {t === 'conversation' ? '💬 The Team' : t === 'report' ? '📄 Report' : '📚 History'}
              </button>
            ))}
            {/* Live stats */}
            {events.length > 0 && tab === 'conversation' && (
              <div className="ml-auto flex items-center gap-3 py-2">
                <span className="font-mono text-[10px] text-gray-600">
                  confidence: <span style={{ color: latestConfidence >= 0.7 ? '#10b981' : '#f59e0b' }}>
                    {(latestConfidence * 100).toFixed(0)}%
                  </span>
                </span>
                <span className="font-mono text-[10px] text-gray-600">
                  cost: ${currentCost.toFixed(4)}
                </span>
              </div>
            )}
          </div>

          {/* Content area */}
          <div className="flex-1 overflow-y-auto px-6 py-4">
            {tab === 'conversation' && (
              <ConversationView events={events} eventsEndRef={eventsEndRef} />
            )}
            {tab === 'report' && (
              <ReportView report={report} />
            )}
            {tab === 'history' && (
              <HistoryView sessions={sessions} onSelect={handleViewSession} />
            )}
          </div>
        </motion.div>
      </motion.div>
    </AnimatePresence>
  )
}

// ─── CONVERSATION VIEW (The Team) ─────────────────────────────────────────

function ConversationView({ events, eventsEndRef }: { events: ResearchEvent[]; eventsEndRef: React.RefObject<HTMLDivElement | null> }) {
  if (events.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center py-16 text-center">
        <span className="text-3xl opacity-40">🔬</span>
        <p className="mt-3 font-mono text-xs text-gray-600">
          Start a research session to watch 5 AI models collaborate
        </p>
        <p className="mt-1 font-mono text-[10px] text-gray-700">
          Planner → Searcher → Extractor → Critic → Synthesizer
        </p>
      </div>
    )
  }

  return (
    <div className="space-y-3">
      {events.map((evt, i) => {
        const style = getModelStyle(evt.model_id)
        const icon = STAGE_ICONS[evt.stage] || '💬'

        return (
          <motion.div
            key={i}
            initial={{ opacity: 0, y: 8 }}
            animate={{ opacity: 1, y: 0 }}
            className={`rounded-lg border border-surface-600/50 ${style.bg} p-4`}
          >
            <div className="mb-1.5 flex items-center gap-2">
              <span className="text-sm">{icon}</span>
              <span className={`font-mono text-[11px] font-bold ${style.text}`}>
                {evt.model || 'Pipeline'}
              </span>
              <span className="font-mono text-[10px] text-gray-600">
                iter {evt.iteration} • {evt.stage}
              </span>
              {evt.confidence > 0 && (
                <span className="ml-auto font-mono text-[10px]" style={{
                  color: evt.confidence >= 0.7 ? '#10b981' : evt.confidence >= 0.4 ? '#f59e0b' : '#ef4444',
                }}>
                  {(evt.confidence * 100).toFixed(0)}% confidence
                </span>
              )}
            </div>
            <p className="font-mono text-xs text-gray-300 leading-relaxed">
              {evt.message}
            </p>
            {(evt.sources > 0 || evt.claims > 0) && (
              <div className="mt-2 flex gap-3 font-mono text-[10px] text-gray-600">
                {evt.sources > 0 && <span>{evt.sources} sources</span>}
                {evt.claims > 0 && <span>{evt.claims} claims</span>}
                {evt.cost_usd > 0 && <span>${evt.cost_usd.toFixed(4)}</span>}
              </div>
            )}
          </motion.div>
        )
      })}
      <div ref={eventsEndRef} />
    </div>
  )
}

// ─── REPORT VIEW ─────────────────────────────────────────────────────────

function ReportView({ report }: { report: string }) {
  if (!report) {
    return (
      <div className="flex flex-col items-center justify-center py-16 text-center">
        <span className="text-3xl opacity-40">📄</span>
        <p className="mt-3 font-mono text-xs text-gray-600">
          Report will appear here when research is complete
        </p>
      </div>
    )
  }

  // Simple markdown rendering — handle citations [1], [2], etc.
  const rendered = report
    .replace(/\[(\d+)\]/g, '<sup class="text-cyan-400 cursor-pointer">[$1]</sup>')
    .replace(/^### (.*$)/gm, '<h3 class="mt-4 mb-2 text-sm font-bold text-gray-100">$1</h3>')
    .replace(/^## (.*$)/gm, '<h2 class="mt-5 mb-2 text-base font-bold text-gray-100">$1</h2>')
    .replace(/^# (.*$)/gm, '<h1 class="mt-6 mb-3 text-lg font-bold text-gray-100">$1</h1>')
    .replace(/\*\*(.*?)\*\*/g, '<strong class="text-gray-200">$1</strong>')
    .replace(/\n\n/g, '</p><p class="mt-3 font-mono text-xs text-gray-300 leading-relaxed">')

  return (
    <div className="prose-sm">
      <p
        className="font-mono text-xs text-gray-300 leading-relaxed"
        dangerouslySetInnerHTML={{ __html: rendered }}
      />
    </div>
  )
}

// ─── HISTORY VIEW ───────────────────────────────────────────────────────

function HistoryView({ sessions, onSelect }: { sessions: ResearchSession[]; onSelect: (s: ResearchSession) => void }) {
  if (sessions.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center py-16 text-center">
        <span className="text-3xl opacity-40">📚</span>
        <p className="mt-3 font-mono text-xs text-gray-600">No research sessions yet</p>
      </div>
    )
  }

  return (
    <div className="space-y-2">
      {sessions.map((sess) => (
        <button
          key={sess.id}
          onClick={() => onSelect(sess)}
          className="flex w-full items-center gap-4 rounded-lg border border-surface-600/50 bg-surface-900/50 p-4 text-left transition-colors hover:bg-surface-700"
        >
          <div className="flex-1">
            <p className="font-mono text-xs text-gray-200">{sess.goal}</p>
            <div className="mt-1 flex gap-3 font-mono text-[10px] text-gray-600">
              <span className={sess.status === 'complete' ? 'text-emerald-400' : sess.status === 'failed' ? 'text-red-400' : 'text-amber-400'}>
                {sess.status}
              </span>
              <span>{sess.iterations} iterations</span>
              <span>{(sess.confidence_score * 100).toFixed(0)}% confidence</span>
              <span>${sess.cost_usd.toFixed(4)}</span>
            </div>
          </div>
          <span className="font-mono text-[10px] text-gray-700">
            {new Date(sess.created_at).toLocaleDateString()}
          </span>
        </button>
      ))}
    </div>
  )
}
