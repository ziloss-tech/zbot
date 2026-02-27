import { motion } from 'framer-motion'
import { StatusBadge } from './StatusBadge'
import { TaskCard } from './TaskCard'
import type { TaskDetail, WorkflowPhase } from '../lib/types'

interface ExecutorPanelProps {
  tasks: TaskDetail[]
  phase: WorkflowPhase
  onViewFile?: (path: string) => void
}

export function ExecutorPanel({ tasks, phase, onViewFile }: ExecutorPanelProps) {
  const isActive = phase === 'executing' || phase === 'handoff'
  const doneCount = tasks.filter((t) => t.status === 'done').length

  return (
    <div className={`flex h-full flex-col rounded-lg border transition-colors ${
      isActive ? 'border-executor/40 shadow-lg shadow-executor/5' : 'border-surface-600'
    } bg-surface-800`}>
      {/* Header */}
      <div className="flex items-center justify-between border-b border-surface-600 px-4 py-3">
        <div className="flex items-center gap-2">
          <span className="text-lg">⚡</span>
          <span className="font-display text-sm font-bold text-executor">Claude</span>
          <StatusBadge status={isActive ? 'running' : phase === 'complete' ? 'done' : 'pending'} />
        </div>
        <span className="font-mono text-xs text-gray-500">EXECUTOR</span>
      </div>

      {/* Content */}
      <div className="flex-1 overflow-y-auto p-4">
        {tasks.length === 0 ? (
          <div className="flex h-full items-center justify-center">
            <p className="font-mono text-sm text-gray-600">
              {phase === 'planning' ? 'Waiting for plan...' : phase === 'handoff' ? 'Receiving tasks...' : 'No tasks yet'}
            </p>
          </div>
        ) : (
          <div className="space-y-3">
            {tasks.map((task, i) => (
              <TaskCard key={task.id} task={task} index={i} onViewFile={onViewFile} />
            ))}
          </div>
        )}
      </div>

      {/* Footer */}
      {tasks.length > 0 && (
        <motion.div
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          className="border-t border-surface-600 px-4 py-2"
        >
          <div className="flex items-center justify-between">
            <span className="font-mono text-xs text-gray-500">
              {doneCount} / {tasks.length} complete
            </span>
            {/* Progress bar */}
            <div className="h-1.5 w-32 overflow-hidden rounded-full bg-surface-600">
              <motion.div
                className="h-full rounded-full bg-gradient-to-r from-executor to-green-400"
                initial={{ width: '0%' }}
                animate={{ width: tasks.length > 0 ? `${(doneCount / tasks.length) * 100}%` : '0%' }}
                transition={{ duration: 0.5 }}
              />
            </div>
          </div>
        </motion.div>
      )}
    </div>
  )
}
