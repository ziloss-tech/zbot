import { useRef, useEffect } from 'react'
import { motion } from 'framer-motion'
import { StatusBadge } from './StatusBadge'
import type { PlannedTask, WorkflowPhase } from '../lib/types'

interface PlannerPanelProps {
  tokens: string
  tasks: PlannedTask[]
  phase: WorkflowPhase
}

export function PlannerPanel({ tokens, tasks, phase }: PlannerPanelProps) {
  const scrollRef = useRef<HTMLDivElement>(null)

  // Auto-scroll as tokens stream in.
  useEffect(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight
    }
  }, [tokens])

  const isActive = phase === 'planning'
  const isDone = phase !== 'idle' && phase !== 'planning'

  return (
    <div className={`flex h-full flex-col rounded-lg border transition-colors ${
      isActive ? 'border-planner/40 shadow-lg shadow-planner/5' : 'border-surface-600'
    } bg-surface-800`}>
      {/* Header */}
      <div className="flex items-center justify-between border-b border-surface-600 px-4 py-3">
        <div className="flex items-center gap-2">
          <span className="text-lg">🧠</span>
          <span className="font-display text-sm font-bold text-planner">GPT-4o</span>
          <StatusBadge status={isActive ? 'planning' : isDone ? 'complete' : 'pending'} />
        </div>
        <span className="font-mono text-xs text-gray-500">PLANNER</span>
      </div>

      {/* Content */}
      <div ref={scrollRef} className="flex-1 overflow-y-auto p-4">
        {phase === 'idle' ? (
          <div className="flex h-full items-center justify-center">
            <p className="font-mono text-sm text-gray-600">
              Waiting for a goal...
            </p>
          </div>
        ) : (
          <div className="space-y-4">
            {/* Streaming tokens */}
            {tokens && (
              <div className="font-mono text-sm leading-relaxed text-gray-300 whitespace-pre-wrap">
                {tokens}
                {isActive && (
                  <motion.span
                    className="inline-block h-4 w-1.5 bg-planner"
                    animate={{ opacity: [1, 0] }}
                    transition={{ duration: 0.8, repeat: Infinity }}
                  />
                )}
              </div>
            )}

            {/* Parsed task list (shown after planning completes) */}
            {tasks.length > 0 && (
              <motion.div
                initial={{ opacity: 0, y: 10 }}
                animate={{ opacity: 1, y: 0 }}
                className="mt-4 space-y-2 border-t border-surface-600 pt-4"
              >
                <p className="font-mono text-xs font-bold text-planner">PLAN:</p>
                {tasks.map((task, i) => (
                  <motion.div
                    key={task.id}
                    initial={{ opacity: 0, x: -10 }}
                    animate={{ opacity: 1, x: 0 }}
                    transition={{ delay: i * 0.1 }}
                    className="flex items-start gap-2 rounded border border-surface-600 bg-surface-700 p-2"
                  >
                    <span className="font-mono text-xs text-planner-glow">
                      {task.id}
                    </span>
                    <div className="flex-1">
                      <p className="font-mono text-xs font-bold text-gray-200">
                        {task.title}
                      </p>
                      {task.parallel && (
                        <span className="font-mono text-[10px] text-green-400">∥ parallel</span>
                      )}
                      {task.depends_on.length > 0 && (
                        <span className="font-mono text-[10px] text-gray-500">
                          {' → after '}{task.depends_on.join(', ')}
                        </span>
                      )}
                    </div>
                  </motion.div>
                ))}
              </motion.div>
            )}
          </div>
        )}
      </div>
    </div>
  )
}
