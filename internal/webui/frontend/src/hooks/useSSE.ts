import { useEffect, useRef, useCallback } from 'react'
import type { SSEEvent } from '../lib/types'

interface UseSSEOptions {
  workflowID: string | null
  onEvent: (event: SSEEvent) => void
}

/**
 * useSSE connects to the SSE stream for a workflow.
 * Auto-reconnects with exponential backoff (1s, 2s, 4s, max 30s).
 */
export function useSSE({ workflowID, onEvent }: UseSSEOptions): { connected: boolean } {
  const eventSourceRef = useRef<EventSource | null>(null)
  const retryDelayRef = useRef(1000)
  const connectedRef = useRef(false)
  const onEventRef = useRef(onEvent)
  onEventRef.current = onEvent

  const connect = useCallback((wfID: string) => {
    if (eventSourceRef.current) {
      eventSourceRef.current.close()
    }

    const es = new EventSource(`/api/stream/${wfID}`)
    eventSourceRef.current = es

    es.onopen = () => {
      connectedRef.current = true
      retryDelayRef.current = 1000
    }

    es.onmessage = (msg) => {
      try {
        const evt = JSON.parse(msg.data) as SSEEvent
        onEventRef.current(evt)
      } catch {
        // ignore malformed events
      }
    }

    es.onerror = () => {
      connectedRef.current = false
      es.close()

      // Exponential backoff reconnect.
      const delay = retryDelayRef.current
      retryDelayRef.current = Math.min(delay * 2, 30000)

      setTimeout(() => {
        if (workflowID === wfID) {
          connect(wfID)
        }
      }, delay)
    }
  }, [workflowID])

  useEffect(() => {
    if (!workflowID) return

    connect(workflowID)

    return () => {
      eventSourceRef.current?.close()
      eventSourceRef.current = null
      connectedRef.current = false
    }
  }, [workflowID, connect])

  return { connected: connectedRef.current }
}
