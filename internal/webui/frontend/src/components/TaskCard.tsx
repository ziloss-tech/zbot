import { useRef, useEffect } from 'react'
import { motion, AnimatePresence } from 'framer-motion'
import { StatusBadge } from './StatusBadge'
import { CriticBadge } from './CriticBadge'
import type { TaskDetail } from '../lib/types'

interface TaskCardProps {
  task: TaskDetail
  index: number
  onViewFile?: (path: string) => void
}

const statusBorder: Record<string, string> = {
  pending: 'border-surface-600',
  running: 'border-executor/40',
  done: 'border-green-500/30',
  failed: 'border-red-500/30',
  canceled: 'border-gray-500/30',
}

/** Extract file paths from output that match ~/zbot-workspace/ pattern. */
function extractFilePaths(output: string): string[] {
  const pattern = /~\/zbot-workspace\/[\w./-]+/g
  const matches = output.match(pattern)
  return matches ? [...new Set(matches)] : []
}

export function TaskCard({ task, index, onViewFile }: TaskCardProps) {
  const outputRef = useRef<HTMLDivElement>(null)

  // Auto-scroll output as it streams.
  useEffect(() => {
    if (outputRef.current && task.status === 'running') {
      outputRef.current.scrollTop = outputRef.current.scrollHeight
    }
  }, [task.output, task.status])

  const filePaths = task.output ? extractFilePaths(task.output) : []

  return (
    <motion.div
      initial={{ opacity: 0, y: 20, scale: 0.95 }}
      animate={{ opacity: 1, y: 0, scale: 1 }}
      transition={{ delay: index * 0.15, type: 'spring', stiffness: 200 }}
      className={`rounded-lg border bg-surface-700 transition-colors ${statusBorder[task.status] ?? 'border-surface-600'}`}
    >
      {/* Card header */}
      <div className="flex items-center justify-between border-b border-surface-600 px-3 py-2">
        <div className="flex items-center gap-2">
          <span className="rounded bg-surface-800 px-1.5 py-0.5 font-mono text-[10px] text-executor-glow">
            {task.id}
          </span>
          <span className="font-mono text-xs font-bold text-gray-200">
            {task.name}
          </span>
        </div>
        <div className="flex items-center gap-2">
          <AnimatePresence>
            {task.criticStatus && (
              <CriticBadge status={task.criticStatus} issues={task.criticResult?.issues} />
            )}
          </AnimatePresence>
          <StatusBadge status={task.status} />
        </div>
      </div>

      {/* Card body */}
      <div className="p-3">
        {task.status === 'pending' && task.depends_on.length > 0 && (
          <p className="font-mono text-xs text-gray-500">
            Depends on {task.depends_on.join(', ')}
          </p>
        )}

        {/* Retrying badge */}
        {task.criticStatus === 'retrying' && (
          <motion.div
            initial={{ opacity: 0, height: 0 }}
            animate={{ opacity: 1, height: 'auto' }}
            className="mb-2 rounded bg-executor/10 px-2 py-1"
          >
            <span className="font-mono text-[10px] text-executor">
              ⟳ GPT-4o requested retry — corrected instruction applied
            </span>
          </motion.div>
        )}

        {(task.status === 'running' || task.status === 'done') && task.output && (
          <div
            ref={outputRef}
            className="max-h-32 overflow-y-auto font-mono text-xs leading-relaxed text-gray-300 whitespace-pre-wrap"
          >
            {task.output}
            {task.status === 'running' && (
              <motion.span
                className="inline-block h-3 w-1 bg-executor"
                animate={{ opacity: [1, 0] }}
                transition={{ duration: 0.8, repeat: Infinity }}
              />
            )}
          </div>
        )}

        {task.status === 'failed' && task.error && (
          <motion.div
            initial={{ x: 0 }}
            animate={{ x: [0, -3, 3, -3, 0] }}
            transition={{ duration: 0.4 }}
            className="rounded bg-red-500/10 p-2 font-mono text-xs text-red-400"
          >
            {task.error}
          </motion.div>
        )}

        {/* Critic issues (partial/fail) */}
        {task.criticResult && task.criticResult.issues.length > 0 && task.criticStatus !== 'retrying' && (
          <div className="mt-2 space-y-1">
            {task.criticResult.issues.map((issue, i) => (
              <div
                key={`${task.id}-issue-${i.toString()}`}
                className={`rounded px-2 py-1 font-mono text-[10px] ${
                  issue.severity === 'critical'
                    ? 'bg-red-500/10 text-red-400'
                    : issue.severity === 'major'
                      ? 'bg-yellow-500/10 text-yellow-400'
                      : 'bg-gray-500/10 text-gray-400'
                }`}
              >
                <span className="font-bold uppercase">{issue.severity}:</span>{' '}
                {issue.description}
              </div>
            ))}
          </div>
        )}

        {/* File preview buttons */}
        {filePaths.length > 0 && onViewFile && (
          <div className="mt-2 flex flex-wrap gap-1">
            {filePaths.map((fp) => (
              <button
                key={fp}
                onClick={() => onViewFile(fp)}
                className="rounded bg-planner/10 px-2 py-0.5 font-mono text-[10px] text-planner transition-colors hover:bg-planner/20"
              >
                📄 {fp.split('/').pop()}
              </button>
            ))}
          </div>
        )}
      </div>
    </motion.div>
  )
}
