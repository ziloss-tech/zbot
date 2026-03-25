import { useState, useEffect, useRef } from 'react'
import type { AgentEvent } from './useEventBus'

// ─── Cortex Status Types ──────────────────────────────────────────────────────

export type CortexStatus = 'idle' | 'thinking' | 'tool_calling' | 'streaming' | 'error'

export interface CortexStatusState {
  status: CortexStatus
  currentTool: string | null
  elapsedMs: number
  turnStartTime: number | null
}

/**
 * useCortexStatus derives a high-level cortex status from event bus events.
 * Provides status, current tool name, and a live elapsed-time counter.
 */
export function useCortexStatus(events: AgentEvent[], cortexWorking: boolean): CortexStatusState {
  const [status, setStatus] = useState<CortexStatus>('idle')
  const [currentTool, setCurrentTool] = useState<string | null>(null)
  const [turnStartTime, setTurnStartTime] = useState<number | null>(null)
  const [elapsedMs, setElapsedMs] = useState(0)
  const timerRef = useRef<ReturnType<typeof setInterval> | null>(null)

  // Derive status from the latest events
  useEffect(() => {
    if (events.length === 0) return

    const latest = events[events.length - 1]
    if (!latest) return

    switch (latest.type) {
      case 'turn_start':
        setStatus('thinking')
        setCurrentTool(null)
        setTurnStartTime(Date.now())
        break
      case 'plan_start':
      case 'thinking':
      case 'plan_complete':
      case 'memory_loaded':
        setStatus('thinking')
        setCurrentTool(null)
        break
      case 'tool_called':
        setStatus('tool_calling')
        setCurrentTool(latest.summary?.split('(')[0]?.trim() || latest.summary || 'tool')
        break
      case 'tool_result':
      case 'tool_error':
        // After tool result, back to thinking (agent processes result)
        setStatus('thinking')
        setCurrentTool(null)
        break
      case 'verify_start':
        setStatus('thinking')
        setCurrentTool(null)
        break
      case 'turn_complete':
        setStatus('idle')
        setCurrentTool(null)
        setTurnStartTime(null)
        setElapsedMs(0)
        break
      case 'error':
        setStatus('error')
        setCurrentTool(null)
        break
    }
  }, [events])

  // If cortexWorking goes false externally, force idle
  useEffect(() => {
    if (!cortexWorking && status !== 'idle' && status !== 'error') {
      setStatus('idle')
      setCurrentTool(null)
      setTurnStartTime(null)
      setElapsedMs(0)
    }
  }, [cortexWorking, status])

  // Auto-clear error after 10 seconds
  useEffect(() => {
    if (status === 'error') {
      const t = setTimeout(() => {
        setStatus('idle')
        setTurnStartTime(null)
        setElapsedMs(0)
      }, 10000)
      return () => clearTimeout(t)
    }
  }, [status])

  // Live elapsed time counter
  useEffect(() => {
    if (turnStartTime) {
      timerRef.current = setInterval(() => {
        setElapsedMs(Date.now() - turnStartTime)
      }, 100)
    } else {
      if (timerRef.current) {
        clearInterval(timerRef.current)
        timerRef.current = null
      }
    }
    return () => {
      if (timerRef.current) clearInterval(timerRef.current)
    }
  }, [turnStartTime])

  return { status, currentTool, elapsedMs, turnStartTime }
}
