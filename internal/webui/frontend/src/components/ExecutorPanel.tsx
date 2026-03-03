import { useRef, useEffect } from 'react'
import { motion, AnimatePresence } from 'framer-motion'
import { StatusBadge } from './StatusBadge'
import type { TaskDetail, WorkflowPhase } from '../lib/types'

// Anthropic's actual logomark — the "A" glyph
function AnthropicLogo({ size = 18 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="currentColor">
      <path d="M13.827 3.52h-3.654L5.063 20.48h3.332l1.138-3.192h5.062l1.138 3.192h3.332L13.827 3.52zm-3.645 11.025 1.822-5.117 1.822 5.117h-3.644z"/>
    </svg>
  )
}

const statusColors: Record<string, string> = {
  done: 'text-openai',
  running: 'text-anthropic animate-pulse',
  pending: 'text-white/20',
  failed: 'text-red-400',
  skipped: 'text-white/15',
}

const statusDot: Record<string, string> = {
  done: 'bg-openai',
  running: 'bg-anthropic',
  pending: 'bg-white/10',
  failed: 'bg-red-400',
  skipped: 'bg-white/10',
}

interface ExecutorPanelProps {
  tasks: TaskDetail[]
  phase: WorkflowPhase
  onViewFile?: (path: string) => void
}

export function ExecutorPanel({ tasks, phase, onViewFile }: ExecutorPanelProps) {
  const scrollRef = useRef<HTMLDivElement>(null)
  const isActive = phase === 'executing' || phase === 'handoff'
  const doneCount = tasks.filter((t) => t.status === 'done').length
  const activeTask = tasks.find((t) => t.status === 'running')

  useEffect(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight
    }
  }, [tasks, activeTask?.output])

  const progress = tasks.length > 0 ? doneCount / tasks.length : 0

  return (
    <div className={`flex h-full flex-col rounded-xl glass-panel transition-all duration-300 ${
      isActive ? 'glass-panel-active-anthropic' : ''
    }`}>
      {/* Header */}
      <div className="flex items-center justify-between border-b border-white/[0.04] px-4 py-3.5">
        <div className="flex items-center gap-2.5">
          <div className={`flex items-center justify-center w-7 h-7 rounded-lg ${
            isActive ? 'bg-anthropic/20 text-anthropic' : 'bg-white/[0.04] text-white/30'
          } transition-colors`}>
            <AnthropicLogo size={14} />
          </div>
          <div>
            <div className="flex items-center gap-2">
              <span className={`font-display text-sm font-semibold ${isActive ? 'text-anthropic' : 'text-white/50'} transition-colors`}>
                Claude
              </span>
              <StatusBadge status={isActive ? 'running' : phase === 'complete' ? 'done' : 'pending'} />
            </div>
            <p className="font-mono text-[9px] text-white/20 uppercase tracking-widest">Executor</p>
          </div>
        </div>
        {tasks.length > 0 && (
          <div className="flex items-center gap-2">
            <span className="font-mono text-[10px] text-white/30">{doneCount}/{tasks.length}</span>
            <div className="h-1 w-16 rounded-full bg-white/[0.06] overflow-hidden">
              <motion.div
                className="h-full rounded-full bg-anthropic"
                animate={{ width: `${progress * 100}%` }}
                transition={{ type: 'spring', damping: 20 }}
              />
            </div>
          </div>
        )}
      </div>

      {/* Active task banner */}
      <AnimatePresence>
        {activeTask && (
          <motion.div
            initial={{ height: 0, opacity: 0 }}
            animate={{ height: 'auto', opacity: 1 }}
            exit={{ height: 0, opacity: 0 }}
            className="overflow-hidden border-b border-anthropic/20 bg-anthropic/[0.06]"
          >
            <div className="flex items-center gap-2 px-4 py-2">
              <motion.div
                className="h-1.5 w-1.5 rounded-full bg-anthropic shrink-0"
                animate={{ opacity: [1, 0.3, 1] }}
                transition={{ duration: 1.2, repeat: Infinity }}
              />
              <span className="font-mono text-[10px] text-anthropic/80 truncate">{activeTask.title}</span>
            </div>
          </motion.div>
        )}
      </AnimatePresence>

      {/* Task list */}
      <div ref={scrollRef} className="flex-1 overflow-y-auto p-3 space-y-1.5">
        {tasks.length === 0 ? (
          <div className="flex h-full items-center justify-center">
            <div className="text-center">
              <AnthropicLogo size={28} />
              <p className="mt-3 font-mono text-xs text-white/15">Waiting for plan</p>
            </div>
          </div>
        ) : (
          tasks.map((task, i) => (
            <motion.div
              key={task.id}
              initial={{ opacity: 0, y: 6 }}
              animate={{ opacity: 1, y: 0 }}
              transition={{ delay: i * 0.04 }}
              className={`rounded-lg border px-3 py-2.5 transition-all ${
                task.status === 'running'
                  ? 'border-anthropic/25 bg-anthropic/[0.06]'
                  : task.status === 'done'
                  ? 'border-white/[0.04] bg-white/[0.02]'
                  : 'border-white/[0.03] bg-transparent'
              }`}
            >
              <div className="flex items-start gap-2.5">
                <div className={`mt-1 h-1.5 w-1.5 rounded-full shrink-0 ${statusDot[task.status] || 'bg-white/10'}`} />
                <div className="flex-1 min-w-0">
                  <div className="flex items-center justify-between gap-2">
                    <span className={`font-sans text-[11px] font-medium leading-snug ${
                      task.status === 'done' ? 'text-white/50' :
                      task.status === 'running' ? 'text-white/90' : 'text-white/35'
                    }`}>
                      {task.title}
                    </span>
                    {task.duration_ms && (
                      <span className="font-mono text-[9px] text-white/20 shrink-0">
                        {(task.duration_ms / 1000).toFixed(1)}s
                      </span>
                    )}
                  </div>

                  {/* Token stream for running task */}
                  {task.status === 'running' && task.output && (
                    <div className="mt-1.5 font-mono text-[10px] leading-relaxed text-white/50 line-clamp-3">
                      {task.output}
                      <span className="inline-block h-2.5 w-0.5 bg-anthropic cursor-blink ml-0.5 align-text-bottom" />
                    </div>
                  )}

                  {/* Files touched */}
                  {task.status === 'done' && task.files_changed && task.files_changed.length > 0 && (
                    <div className="mt-1.5 flex flex-wrap gap-1">
                      {task.files_changed.slice(0, 3).map((f) => (
                        <button
                          key={f}
                          onClick={() => onViewFile?.(f)}
                          className="font-mono text-[9px] text-openai/50 hover:text-openai transition-colors truncate max-w-[140px]"
                        >
                          {f.split('/').pop()}
                        </button>
                      ))}
                      {task.files_changed.length > 3 && (
                        <span className="font-mono text-[9px] text-white/20">+{task.files_changed.length - 3}</span>
                      )}
                    </div>
                  )}
                </div>
              </div>
            </motion.div>
          ))
        )}
      </div>
    </div>
  )
}
