import { useEffect, useState, useRef, useCallback } from 'react'

// AgentEvent mirrors the Go AgentEvent struct.
export interface AgentEvent {
  id: string
  session_id: string
  type: string // turn_start, tool_called, tool_result, tool_error, turn_complete, etc.
  summary: string
  detail?: Record<string, any>
  timestamp: string
}

interface UseEventBusOptions {
  sessionID?: string
  onEvent?: (event: AgentEvent) => void
}

export function useEventBus({ sessionID = 'web-chat', onEvent }: UseEventBusOptions = {}) {
  const [events, setEvents] = useState<AgentEvent[]>([])
  const [connected, setConnected] = useState(false)
  const [cortexWorking, setCortexWorking] = useState(false)
  const eventSourceRef = useRef<EventSource | null>(null)
  const onEventRef = useRef(onEvent)
  onEventRef.current = onEvent

  const connect = useCallback(() => {
    if (eventSourceRef.current) {
      eventSourceRef.current.close()
    }

    const es = new EventSource(`/api/events/${sessionID}`)
    eventSourceRef.current = es

    es.onopen = () => {
      setConnected(true)
    }

    es.onmessage = (msg) => {
      try {
        const event: AgentEvent = JSON.parse(msg.data)
        setEvents(prev => [...prev.slice(-50), event]) // keep last 50

        // Track cortex working state
        if (event.type === 'turn_start') setCortexWorking(true)
        if (event.type === 'turn_complete' || event.type === 'error') setCortexWorking(false)

        // Notify callback
        onEventRef.current?.(event)
      } catch {
        // ignore malformed events
      }
    }

    es.onerror = () => {
      setConnected(false)
      es.close()
      // Reconnect after 3 seconds
      setTimeout(connect, 3000)
    }
  }, [sessionID])

  useEffect(() => {
    connect()
    return () => {
      eventSourceRef.current?.close()
    }
  }, [connect])

  const clearEvents = useCallback(() => setEvents([]), [])

  return {
    events,
    connected,
    cortexWorking,
    clearEvents,
    recentTools: events
      .filter(e => e.type === 'tool_called' || e.type === 'tool_result' || e.type === 'tool_error')
      .slice(-5),
  }
}
