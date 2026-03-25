import { useState, useEffect, useRef } from 'react'
import { motion, AnimatePresence } from 'framer-motion'
import type { AgentEvent } from '../hooks/useEventBus'

// ─── Types ──────────────────────────────────────────────────────────────────

interface TimelineEntry {
  id: string
  timestamp: number
  icon: string
  text: string
  type: 'received' | 'thinking' | 'tool' | 'result' | 'complete' | 'error' | 'crawl' | 'review'
}

interface ActivityTimelineProps {
  events: AgentEvent[]
  cortexWorking: boolean
}

// ─── Event → Timeline Entry Mapping ─────────────────────────────────────────

function eventToEntry(event: AgentEvent): TimelineEntry | null {
  const ts = event.timestamp ? new Date(event.timestamp).getTime() : Date.now()
  const base = { id: event.id, timestamp: ts }

  switch (event.type) {
    case 'turn_start':
      return {
        ...base,
        icon: '⏳',
        text: `Received: "${(event.summary || '').slice(0, 40)}${(event.summary || '').length > 40 ? '...' : ''}"`,
        type: 'received',
      }
    case 'plan_start':
    case 'thinking':
      return {
        ...base,
        icon: '🧠',
        text: event.summary || 'Thinking...',
        type: 'thinking',
      }
    case 'plan_complete':
      return {
        ...base,
        icon: '🧠',
        text: `Plan ready (${event.detail?.steps || '?'} steps)`,
        type: 'thinking',
      }
    case 'memory_loaded':
      return {
        ...base,
        icon: '🧠',
        text: event.summary || 'Memory loaded',
        type: 'thinking',
      }
    case 'tool_called':
      return {
        ...base,
        icon: '🔧',
        text: event.summary || 'Calling tool...',
        type: 'tool',
      }
    case 'tool_result':
      return {
        ...base,
        icon: '📄',
        text: event.summary || 'Got results',
        type: 'result',
      }
    case 'tool_error':
      return {
        ...base,
        icon: '❌',
        text: event.summary || 'Tool error',
        type: 'error',
      }
    case 'verify_start':
      return {
        ...base,
        icon: '🔍',
        text: 'Verifying response...',
        type: 'review',
      }
    case 'verify_complete':
      return {
        ...base,
        icon: '🔍',
        text: event.summary || 'Verification complete',
        type: 'review',
      }
    case 'review_finding':
      return {
        ...base,
        icon: '🔍',
        text: event.summary || 'Reviewer finding',
        type: 'review',
      }
    case 'crawl_action':
      return {
        ...base,
        icon: '🌐',
        text: event.summary || 'Browser action',
        type: 'crawl',
      }
    case 'crawl_screenshot':
      return {
        ...base,
        icon: '🌐',
        text: 'Screenshot captured',
        type: 'crawl',
      }
    case 'file_read':
      return {
        ...base,
        icon: '📄',
        text: event.summary || 'Reading file...',
        type: 'result',
      }
    case 'file_write':
      return {
        ...base,
        icon: '✏️',
        text: event.summary || 'Writing file...',
        type: 'result',
      }
    case 'turn_complete': {
      const cost = event.detail?.cost_usd
      const tokens = event.detail?.total_tokens
      const parts: string[] = ['Done']
      if (cost != null) parts.push(`$${Number(cost).toFixed(3)}`)
      if (tokens != null) parts.push(`${Number(tokens).toLocaleString()} tok`)
      return {
        ...base,
        icon: '✅',
        text: parts.join(', '),
        type: 'complete',
      }
    }
    case 'error':
      return {
        ...base,
        icon: '❌',
        text: event.summary || 'Error',
        type: 'error',
      }
    default:
      return null
  }
}

function formatTime(ts: number): string {
  const d = new Date(ts)
  return d.toLocaleTimeString('en-US', { hour12: false, hour: '2-digit', minute: '2-digit', second: '2-digit' })
}

// ─── Color per entry type ───────────────────────────────────────────────────

const typeColors: Record<TimelineEntry['type'], string> = {
  received: 'text-white/50',
  thinking: 'text-cyan-400/70',
  tool: 'text-anthropic/80',
  result: 'text-emerald-400/70',
  complete: 'text-emerald-400/80',
  error: 'text-red-400/80',
  crawl: 'text-green-400/70',
  review: 'text-auditor/70',
}

// ─── Component ──────────────────────────────────────────────────────────────

export function ActivityTimeline({ events, cortexWorking }: ActivityTimelineProps) {
  const [entries, setEntries] = useState<TimelineEntry[]>([])
  const scrollRef = useRef<HTMLDivElement>(null)
  const processedRef = useRef<Set<string>>(new Set())

  // Convert new events into timeline entries
  useEffect(() => {
    const newEntries: TimelineEntry[] = []

    for (const evt of events) {
      if (processedRef.current.has(evt.id)) continue
      processedRef.current.add(evt.id)

      // Clear entries on new turn
      if (evt.type === 'turn_start') {
        processedRef.current.clear()
        processedRef.current.add(evt.id)
        setEntries([])
      }

      const entry = eventToEntry(evt)
      if (entry) newEntries.push(entry)
    }

    if (newEntries.length > 0) {
      setEntries(prev => [...prev, ...newEntries].slice(-50))
    }
  }, [events])

  // Auto-scroll to newest entry
  useEffect(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollLeft = scrollRef.current.scrollWidth
    }
  }, [entries])

  // Don't render if no entries and not working
  if (entries.length === 0 && !cortexWorking) return null

  return (
    <motion.div
      initial={{ height: 0, opacity: 0 }}
      animate={{ height: 'auto', opacity: 1 }}
      exit={{ height: 0, opacity: 0 }}
      transition={{ duration: 0.2 }}
      className={`border-t border-white/[0.04] ${
        cortexWorking ? 'bg-cyan-500/[0.03]' : 'bg-transparent'
      } transition-colors duration-500`}
    >
      <div
        ref={scrollRef}
        className="flex items-center gap-3 px-3 py-1.5 overflow-x-auto scrollbar-none"
        style={{ height: '34px' }}
      >
        {/* Activity label */}
        <span className="font-mono text-[8px] text-white/15 uppercase tracking-widest shrink-0 select-none">
          activity
        </span>

        <AnimatePresence mode="popLayout">
          {entries.map((entry) => (
            <motion.div
              key={entry.id}
              initial={{ opacity: 0, x: 12, scale: 0.95 }}
              animate={{ opacity: 1, x: 0, scale: 1 }}
              exit={{ opacity: 0, scale: 0.9 }}
              transition={{ duration: 0.15 }}
              className="flex items-center gap-1.5 shrink-0"
            >
              <span className="font-mono text-[9px] text-white/15">
                {formatTime(entry.timestamp)}
              </span>
              <span className="text-[10px]">{entry.icon}</span>
              <span className={`font-mono text-[10px] ${typeColors[entry.type]} whitespace-nowrap max-w-[200px] truncate`}>
                {entry.text}
              </span>
            </motion.div>
          ))}
        </AnimatePresence>

        {/* Pulsing dot when working but no recent entries */}
        {cortexWorking && entries.length === 0 && (
          <motion.div
            className="flex items-center gap-1.5"
            animate={{ opacity: [0.4, 1, 0.4] }}
            transition={{ duration: 1.5, repeat: Infinity }}
          >
            <span className="h-1.5 w-1.5 rounded-full bg-cyan-400" />
            <span className="font-mono text-[10px] text-cyan-400/60">Working...</span>
          </motion.div>
        )}
      </div>
    </motion.div>
  )
}
