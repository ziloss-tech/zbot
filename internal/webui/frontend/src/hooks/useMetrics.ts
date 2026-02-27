import { useState, useEffect } from 'react'
import type { Metrics } from '../lib/types'
import { fetchMetrics } from '../lib/api'

const defaultMetrics: Metrics = {
  active_workflows: 0,
  total_tasks: 0,
  done_tasks: 0,
  tokens_today: 0,
  cost_today: '$0.00',
}

export function useMetrics(intervalMs = 10000): Metrics {
  const [metrics, setMetrics] = useState<Metrics>(defaultMetrics)

  useEffect(() => {
    let mounted = true

    const load = async () => {
      try {
        const data = await fetchMetrics()
        if (mounted) setMetrics(data)
      } catch {
        // silently fail — metrics are informational
      }
    }

    void load()
    const timer = setInterval(() => { void load() }, intervalMs)

    return () => {
      mounted = false
      clearInterval(timer)
    }
  }, [intervalMs])

  return metrics
}
