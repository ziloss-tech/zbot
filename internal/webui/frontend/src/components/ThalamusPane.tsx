import { useState, useRef, useEffect, useCallback } from 'react'
import { motion, AnimatePresence } from 'framer-motion'
import type { WorkflowState } from '../lib/types'
import type { AgentEvent } from '../hooks/useEventBus'

// ─── Types ──────────────────────────────────────────────────────────────────

interface ThalamusMessage {
  id: string
  role: 'user' | 'thalamus' | 'system'
  content: string
  timestamp: number
}

interface VerificationResult {
  id: string
  approved: boolean
  confidence: number
  issues: string[]
  suggestion: string
  timestamp: number
}

interface PlanSummary {
  id: string
  type: string
  complexity: string
  steps: number
  verification: string
  timestamp: number
}

interface ThalamusPaneProps {
  workflowState: WorkflowState
  eventBus?: {
    events: AgentEvent[]
    connected: boolean
    cortexWorking: boolean
    recentTools: AgentEvent[]
    clearEvents: () => void
  }
  className?: string
  onClose?: () => void
}

// ─── Brain Icon ─────────────────────────────────────────────────────────────

function ThalamusIcon({ size = 14 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="1.3">
      <ellipse cx="10" cy="10" rx="7" ry="8" />
      <path d="M10 2c0 0-3 4-3 8s3 8 3 8" strokeLinecap="round" />
      <path d="M10 2c0 0 3 4 3 8s-3 8-3 8" strokeLinecap="round" />
      <path d="M3.5 7h13M3.5 13h13" strokeLinecap="round" />
    </svg>
  )
}

// ─── Cognitive Cards ────────────────────────────────────────────────────────

function PlanCard({ plan }: { plan: PlanSummary }) {
  return (
    <motion.div
      initial={{ opacity: 0, y: 4 }}
      animate={{ opacity: 1, y: 0 }}
      className="rounded-lg border border-violet-500/20 bg-violet-500/[0.06] p-3"
    >
      <div className="flex items-center gap-2 mb-1.5">
        <span className="font-mono text-[9px] uppercase tracking-wider text-violet-400/60">Frontal Lobe</span>
        <span className="inline-flex items-center rounded-full px-1.5 py-0.5 font-mono text-[9px] bg-violet-500/15 text-violet-300">
          plan
        </span>
      </div>
      <div className="font-mono text-[11px] text-white/70">
        Plan: <span className="text-violet-300">{plan.type}</span>, {plan.complexity}, {plan.steps} step{plan.steps !== 1 ? 's' : ''}
      </div>
      <div className="font-mono text-[9px] text-white/30 mt-1">
        verification: {plan.verification}
      </div>
    </motion.div>
  )
}

function VerificationCard({ result }: { result: VerificationResult }) {
  const isApproved = result.approved
  return (
    <motion.div
      initial={{ opacity: 0, y: 4 }}
      animate={{ opacity: 1, y: 0 }}
      className={`rounded-lg border p-3 ${
        isApproved
          ? 'border-emerald-500/20 bg-emerald-500/[0.06]'
          : 'border-amber-500/20 bg-amber-500/[0.06]'
      }`}
    >
      <div className="flex items-center gap-2 mb-1.5">
        <span className={`font-mono text-[9px] uppercase tracking-wider ${
          isApproved ? 'text-emerald-400/60' : 'text-amber-400/60'
        }`}>Auditor verification</span>
      </div>
      <div className={`font-mono text-[11px] ${isApproved ? 'text-emerald-300' : 'text-amber-300'}`}>
        {isApproved ? `Verified ✓` : 'Revision needed'} (confidence: {result.confidence}%)
      </div>
      {!isApproved && result.issues.length > 0 && (
        <div className="mt-2 space-y-1">
          {result.issues.map((issue, i) => (
            <div key={i} className="font-mono text-[10px] text-amber-400/70">
              — {issue}
            </div>
          ))}
          {result.suggestion && (
            <div className="font-mono text-[10px] text-white/40 mt-1.5 italic">
              Suggestion: {result.suggestion}
            </div>
          )}
        </div>
      )}
    </motion.div>
  )
}

// ─── Real Event Bus Chip ────────────────────────────────────────────────────

function EventChip({ event }: { event: AgentEvent }) {
  const colors: Record<string, string> = {
    plan_start: 'text-violet-400 border-violet-500/20',
    plan_complete: 'text-violet-300 border-violet-500/15',
    verify_start: 'text-amber-400 border-amber-500/20',
    verify_complete: 'text-emerald-400 border-emerald-500/20',
    review_finding: 'text-orange-400 border-orange-500/20',
    review_cycle: 'text-orange-300 border-orange-500/15',
    review_error: 'text-red-400 border-red-500/20',
    tool_called: 'text-cyan-400 border-cyan-500/20',
    tool_result: 'text-cyan-300 border-cyan-500/15',
    tool_error: 'text-red-400 border-red-500/20',
    memory_loaded: 'text-blue-400 border-blue-500/20',
    file_read: 'text-cyan-300 border-cyan-500/15',
    file_write: 'text-amber-400 border-amber-500/20',
    crawl_screenshot: 'text-green-400 border-green-500/20',
    crawl_action: 'text-green-300 border-green-500/15',
    turn_start: 'text-white/40 border-white/10',
    turn_complete: 'text-emerald-300 border-emerald-500/15',
  }
  const color = colors[event.type] || 'text-white/40 border-white/10'

  return (
    <div className={`flex items-center gap-1.5 rounded-md border bg-white/[0.02] px-2 py-1 ${color}`}>
      <span className="font-mono text-[9px] uppercase opacity-60">{event.type.replace(/_/g, ' ')}</span>
      <span className="font-mono text-[10px] opacity-80 truncate">{event.summary}</span>
    </div>
  )
}

// ─── Main Component ─────────────────────────────────────────────────────────

export function ThalamusPane({ workflowState, eventBus, className = '', onClose }: ThalamusPaneProps) {
  const [messages, setMessages] = useState<ThalamusMessage[]>([])
  const [plans, setPlans] = useState<PlanSummary[]>([])
  const [verifications, setVerifications] = useState<VerificationResult[]>([])
  const [busEvents, setBusEvents] = useState<AgentEvent[]>([])
  const [input, setInput] = useState('')
  const [loading, setLoading] = useState(false)
  const endRef = useRef<HTMLDivElement>(null)
  const inputRef = useRef<HTMLInputElement>(null)
  const processedEventsRef = useRef<Set<string>>(new Set())

  // Auto-scroll
  useEffect(() => {
    endRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [messages, plans, verifications, busEvents])

  // ─── Consume real event bus via eventBus prop ──────────────────────────
  useEffect(() => {
    const rawEvents = eventBus?.events || []
    if (rawEvents.length === 0) return

    // Only process new events
    const newEvents = rawEvents.filter(e => !processedEventsRef.current.has(e.id))
    if (newEvents.length === 0) return

    for (const evt of newEvents) {
      processedEventsRef.current.add(evt.id)

      // Extract plan_complete into structured plan summary
      if (evt.type === 'plan_complete' && evt.detail) {
        setPlans(prev => [...prev, {
          id: evt.id,
          type: String(evt.detail?.type || 'unknown'),
          complexity: String(evt.detail?.complexity || 'unknown'),
          steps: Number(evt.detail?.steps || 0),
          verification: String(evt.detail?.verification || 'none'),
          timestamp: Date.now(),
        }])
      }

      // Extract verify_complete into structured verification result
      if (evt.type === 'verify_complete' && evt.detail) {
        setVerifications(prev => [...prev, {
          id: evt.id,
          approved: Boolean(evt.detail?.approved),
          confidence: Number(evt.detail?.confidence || 0),
          issues: Array.isArray(evt.detail?.issues) ? evt.detail.issues as string[] : [],
          suggestion: String(evt.detail?.suggestion || ''),
          timestamp: Date.now(),
        }])
      }
    }

    // Keep last 30 events for the bus strip
    setBusEvents(rawEvents.slice(-30))
  }, [eventBus?.events])

  // Show a system message when Thalamus first observes activity
  useEffect(() => {
    if (busEvents.length > 0 && messages.length === 0) {
      setMessages([{
        id: `sys-${Date.now()}`,
        role: 'system',
        content: 'Thalamus online — observing Cortex via event bus',
        timestamp: Date.now(),
      }])
    }
  }, [busEvents.length, messages.length])

  // Send message to Thalamus (manual Q&A — uses workflowState.agentTokens and goal)
  const send = useCallback(async () => {
    const text = input.trim()
    if (!text || loading) return

    setInput('')
    setMessages(prev => [...prev, {
      id: `user-${Date.now()}`,
      role: 'user',
      content: text,
      timestamp: Date.now(),
    }])
    setLoading(true)

    try {
      // Build event context from real bus events
      const eventSummary = busEvents.slice(-10).map(e => `[${e.type}] ${e.summary}`).join('\n')
      const cortexOutput = workflowState.agentTokens.slice(-500)
      const thalamusQuery = `[AUDITOR MODE] You are the Auditor, the quality assurance engine. Cortex is currently working on: "${workflowState.goal}". Recent events:\n${eventSummary}\n\nCortex's latest output (last 500 chars):\n${cortexOutput}\n\nUser asks the Auditor: ${text}\n\nRespond as the Auditor — concise, observational, helpful. You can see what Cortex is doing but you're a separate engine.`

      const res = await fetch('/api/chat', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ message: thalamusQuery }),
      })

      if (!res.ok) throw new Error('Thalamus query failed')
      const data = await res.json()

      setMessages(prev => [...prev, {
        id: `thalamus-${Date.now()}`,
        role: 'thalamus',
        content: data.reply,
        timestamp: Date.now(),
      }])
    } catch {
      setMessages(prev => [...prev, {
        id: `err-${Date.now()}`,
        role: 'thalamus',
        content: 'Error reaching Thalamus.',
        timestamp: Date.now(),
      }])
    } finally {
      setLoading(false)
    }
  }, [input, loading, busEvents, workflowState])

  const handleKey = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter') {
      e.preventDefault()
      void send()
    }
  }

  return (
    <div className={`flex h-full flex-col rounded-xl glass-panel ${className}`}>
      {/* Header */}
      <div className="flex items-center justify-between border-b border-white/[0.04] px-4 py-3">
        <div className="flex items-center gap-2.5">
          <div className="flex items-center justify-center w-7 h-7 rounded-lg bg-purple-500/20 text-purple-400">
            <ThalamusIcon size={14} />
          </div>
          <div>
            <div className="flex items-center gap-2">
              <span className="font-display text-sm font-semibold text-white/90">Auditor</span>
              <span className="inline-flex items-center gap-1 rounded-full px-2 py-0.5 font-mono text-[9px] bg-purple-500/15 text-purple-400">
                <span className="h-1 w-1 rounded-full bg-current" />
                watching
              </span>
            </div>
            <p className="font-mono text-[9px] text-white/20 uppercase tracking-widest">Quality assurance</p>
          </div>
        </div>

        {onClose && (
          <button
            onClick={onClose}
            className="rounded-md p-1 text-white/20 hover:text-white/50 hover:bg-white/[0.04] transition-colors"
          >
            <svg viewBox="0 0 16 16" className="w-3.5 h-3.5" fill="none" stroke="currentColor" strokeWidth="1.5">
              <path d="M4 4l8 8M12 4l-8 8" strokeLinecap="round" />
            </svg>
          </button>
        )}
      </div>

      {/* Real event bus strip */}
      <AnimatePresence>
        {busEvents.length > 0 && (
          <motion.div
            initial={{ height: 0, opacity: 0 }}
            animate={{ height: 'auto', opacity: 1 }}
            exit={{ height: 0, opacity: 0 }}
            className="overflow-hidden border-b border-white/[0.04] bg-white/[0.015]"
          >
            <div className="px-3 py-2 space-y-1 max-h-24 overflow-y-auto">
              <span className="font-mono text-[8px] text-white/15 uppercase tracking-widest">Cortex event bus</span>
              <div className="flex flex-wrap gap-1">
                {busEvents.slice(-6).map(evt => (
                  <EventChip key={evt.id} event={evt} />
                ))}
              </div>
            </div>
          </motion.div>
        )}
      </AnimatePresence>

      {/* Cognitive results + Messages */}
      <div className="flex-1 overflow-y-auto p-4 space-y-3">
        {messages.length === 0 && plans.length === 0 && verifications.length === 0 && (
          <div className="flex h-full flex-col items-center justify-center text-center py-8">
            <ThalamusIcon size={28} />
            <p className="mt-4 font-mono text-xs text-white/20">
              Thalamus activates when Cortex is working.
            </p>
            <p className="mt-1 font-mono text-[10px] text-white/12">
              Ask questions about what Cortex is doing, request preparations, or flag concerns.
            </p>
          </div>
        )}

        {/* Plan summaries — shown at the top */}
        {plans.map(plan => (
          <PlanCard key={plan.id} plan={plan} />
        ))}

        {/* Verification results — shown proactively */}
        {verifications.map(v => (
          <VerificationCard key={v.id} result={v} />
        ))}

        {/* Chat messages */}
        {messages.map(msg => (
          <motion.div
            key={msg.id}
            initial={{ opacity: 0, y: 4 }}
            animate={{ opacity: 1, y: 0 }}
            className={`rounded-lg border p-3 ${
              msg.role === 'user'
                ? 'border-white/[0.06] bg-white/[0.04] ml-6'
                : msg.role === 'system'
                ? 'border-purple-500/10 bg-purple-500/[0.03] text-center mx-4'
                : 'border-purple-500/15 bg-purple-500/[0.04] mr-4'
            }`}
          >
            {msg.role !== 'system' && (
              <div className="flex items-center gap-2 mb-1.5">
                <span className="font-mono text-[9px] uppercase tracking-wider text-white/30">
                  {msg.role === 'user' ? 'You' : 'Thalamus'}
                </span>
              </div>
            )}
            <div className={`whitespace-pre-wrap font-mono text-[11px] leading-relaxed ${
              msg.role === 'system' ? 'text-purple-400/50 text-[10px]' : 'text-white/70'
            }`}>
              {msg.content}
            </div>
          </motion.div>
        ))}

        {loading && (
          <motion.div
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            className="rounded-lg border border-purple-500/15 bg-purple-500/[0.04] p-3 mr-4"
          >
            <div className="flex gap-1">
              {[0, 1, 2].map(i => (
                <motion.span
                  key={i}
                  className="h-1.5 w-1.5 rounded-full bg-purple-400/50"
                  animate={{ opacity: [0.3, 1, 0.3] }}
                  transition={{ duration: 0.9, delay: i * 0.2, repeat: Infinity }}
                />
              ))}
            </div>
          </motion.div>
        )}

        <div ref={endRef} />
      </div>

      {/* Input */}
      <div className="border-t border-white/[0.04] p-3">
        <div className="flex gap-2">
          <input
            ref={inputRef}
            value={input}
            onChange={e => setInput(e.target.value)}
            onKeyDown={handleKey}
            placeholder="Ask the Auditor..."
            className="flex-1 rounded-lg border border-white/[0.06] bg-white/[0.03] px-3 py-2 font-mono text-xs text-white/80 placeholder-white/20 outline-none focus:border-purple-500/40 transition-colors"
          />
          <button
            onClick={() => void send()}
            disabled={!input.trim() || loading}
            className="rounded-lg bg-purple-500/20 px-3 font-mono text-xs text-purple-400 transition-all hover:bg-purple-500/30 disabled:opacity-30 disabled:cursor-not-allowed"
          >
            ↵
          </button>
        </div>
        <p className="mt-1.5 font-mono text-[9px] text-white/15">Ask about what Cortex is doing</p>
      </div>
    </div>
  )
}
