import { useState, useEffect } from 'react'
import { motion } from 'framer-motion'
import { fetchWorkflows } from '../lib/api'
import type { WorkflowListItem } from '../lib/types'

interface WorkflowHistoryProps {
  activeID: string
  onSelect: (id: string) => void
}

export function WorkflowHistory({ activeID, onSelect }: WorkflowHistoryProps) {
  const [workflows, setWorkflows] = useState<WorkflowListItem[]>([])

  useEffect(() => {
    const load = async () => {
      try {
        const data = await fetchWorkflows()
        setWorkflows(data)
      } catch {
        // silently fail — history is optional
      }
    }
    void load()
    const interval = setInterval(() => { void load() }, 10000)
    return () => clearInterval(interval)
  }, [])

  if (workflows.length === 0) return null

  return (
    <div className="flex items-center gap-2 overflow-x-auto px-4 py-2">
      <span className="shrink-0 font-mono text-xs text-gray-500">Recent:</span>
      {workflows.map((wf) => {
        const isActive = wf.id === activeID
        const statusIcon = wf.status === 'running' ? '⟳'
          : wf.done_count === wf.task_count && wf.task_count > 0 ? '✓'
          : wf.status === 'failed' ? '✗'
          : '◌'

        return (
          <motion.button
            key={wf.id}
            onClick={() => onSelect(wf.id)}
            className={`shrink-0 rounded-full px-3 py-1 font-mono text-xs transition-colors ${
              isActive
                ? 'bg-planner/20 text-planner-glow ring-1 ring-planner/40'
                : 'bg-surface-700 text-gray-400 hover:bg-surface-600'
            }`}
            whileHover={{ scale: 1.05 }}
            whileTap={{ scale: 0.95 }}
            animate={wf.status === 'running' ? { opacity: [1, 0.7, 1] } : undefined}
            transition={wf.status === 'running' ? { duration: 2, repeat: Infinity } : undefined}
          >
            {statusIcon} {wf.id.slice(0, 8)}
          </motion.button>
        )
      })}
    </div>
  )
}
