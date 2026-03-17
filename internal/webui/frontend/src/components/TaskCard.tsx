import { useRef, useEffect } from 'react'
import { motion, AnimatePresence } from 'framer-motion'
import { StatusBadge } from './StatusBadge'
import type { TaskDetail, CriticIssue } from '../lib/types'

// v2: Inline critic badge (CriticBadge.tsx deleted — critic is deprecated)
function CriticBadge({ status, issues }: { status: string; issues?: CriticIssue[] }) {
  const configs: Record<string, { label: string; color: string; icon: string }> = {
    reviewing: { label: 'Reviewing...', color: 'bg-yellow-500/20 text-yellow-400', icon: '🔍' },
    passed:    { label: 'Passed', color: 'bg-green-500/20 text-green-400', icon: '✓' },
    failed:    { label: 'Retry', color: 'bg-red-500/20 text-red-400', icon: '✗' },
    partial:   { label: 'Partial', color: 'bg-yellow-500/20 text-yellow-400', icon: '⚠' },
    retrying:  { label: 'Retrying', color: 'bg-anthropic/20 text-anthropic', icon: '⟳' },
  }
  const cfg = configs[status]
  if (!cfg) return null
  return (
    <span className={`flex items-center gap-1 rounded-full px-2 py-0.5 font-mono text-[10px] ${cfg.color}`}>
      <span>{cfg.icon}</span>
      <span>{cfg.label}</span>
      {issues && issues.length > 0 && status !== 'reviewing' && status !== 'retrying' && (
        <span className="rounded bg-black/20 px-1">{issues.length}</span>
      )}
    </span>
  )
}

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

  // Sprint 13: Prefer output_files from API (tracked by orchestrator), fallback to regex extraction.
  const apiFiles = task.outputFiles && task.outputFiles.length > 0 ? task.outputFiles : []
  const regexFiles = task.output ? extractFilePaths(task.output) : []
  const filePaths = apiFiles.length > 0 ? apiFiles : regexFiles

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
              ⟳ Self-critique triggered retry — corrected instruction applied
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
            {filePaths.map((fp) => {
              const fileName = fp.split('/').pop() || fp
              const ext = fileName.split('.').pop()?.toLowerCase() || ''
              const icon = ext === 'csv' ? '📊' : ext === 'json' ? '{}' : ext === 'pdf' ? '📕' : '📄'
              return (
                <button
                  key={fp}
                  onClick={() => onViewFile(fp)}
                  className="rounded bg-violet-500/10 px-2 py-0.5 font-mono text-[10px] text-violet-400 transition-colors hover:bg-violet-500/20"
                >
                  {icon} {fileName}
                </button>
              )
            })}
          </div>
        )}
      </div>
    </motion.div>
  )
}
