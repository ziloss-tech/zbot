import { useRef, useEffect } from 'react'
import { motion } from 'framer-motion'
import { StatusBadge } from './StatusBadge'
import type { PlannedTask, WorkflowPhase } from '../lib/types'

// OpenAI official brand mark — simplified SVG
function OpenAILogo({ size = 18 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 41 41" fill="currentColor">
      <path d="M37.532 16.87a9.963 9.963 0 00-.856-8.184 10.078 10.078 0 00-10.855-4.835 9.964 9.964 0 00-7.504-3.337 10.079 10.079 0 00-9.614 6.977 9.967 9.967 0 00-6.664 4.834 10.08 10.08 0 001.24 11.817 9.965 9.965 0 00.856 8.185 10.079 10.079 0 0010.855 4.835 9.965 9.965 0 007.504 3.336 10.078 10.078 0 009.617-6.981 9.967 9.967 0 006.663-4.834 10.079 10.079 0 00-1.243-11.813zM22.498 37.886a7.474 7.474 0 01-4.799-1.735c.061-.033.168-.091.237-.134l7.964-4.6a1.294 1.294 0 00.655-1.134V19.054l3.366 1.944a.12.12 0 01.066.092v9.299a7.505 7.505 0 01-7.49 7.496zM6.392 31.006a7.471 7.471 0 01-.894-5.023c.06.036.162.099.237.141l7.964 4.6a1.297 1.297 0 001.308 0l9.724-5.614v3.888a.12.12 0 01-.048.103l-8.051 4.649a7.504 7.504 0 01-10.24-2.744zM4.297 13.62A7.469 7.469 0 018.2 10.333c0 .068-.004.19-.004.274v9.201a1.294 1.294 0 00.654 1.132l9.723 5.614-3.366 1.944a.12.12 0 01-.114.012L7.044 23.86a7.504 7.504 0 01-2.747-10.24zm27.658 6.437l-9.724-5.615 3.367-1.943a.121.121 0 01.114-.012l8.048 4.648a7.498 7.498 0 01-1.158 13.528v-9.476a1.293 1.293 0 00-.647-1.13zm3.35-5.043c-.059-.037-.162-.099-.236-.141l-7.965-4.6a1.298 1.298 0 00-1.308 0l-9.723 5.614v-3.888a.12.12 0 01.048-.103l8.05-4.645a7.497 7.497 0 0111.135 7.763zm-21.063 6.929l-3.367-1.944a.12.12 0 01-.065-.092v-9.299a7.497 7.497 0 0112.293-5.756 6.94 6.94 0 00-.236.134l-7.965 4.6a1.294 1.294 0 00-.654 1.132l-.006 11.225zm1.829-3.943l4.33-2.501 4.332 2.5v4.999l-4.331 2.5-4.331-2.5V18z"/>
    </svg>
  )
}

interface PlannerPanelProps {
  tokens: string
  tasks: PlannedTask[]
  phase: WorkflowPhase
}

export function PlannerPanel({ tokens, tasks, phase }: PlannerPanelProps) {
  const scrollRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight
    }
  }, [tokens])

  const isActive = phase === 'planning'
  const isDone = phase !== 'idle' && phase !== 'planning'

  return (
    <div className={`flex h-full flex-col rounded-xl glass-panel transition-all duration-300 ${
      isActive ? 'glass-panel-active-openai' : ''
    }`}>
      {/* Header */}
      <div className="flex items-center justify-between border-b border-white/[0.04] px-4 py-3.5">
        <div className="flex items-center gap-2.5">
          <div className={`flex items-center justify-center w-7 h-7 rounded-lg ${
            isActive ? 'bg-openai/20 text-openai' : 'bg-white/[0.04] text-white/30'
          } transition-colors`}>
            <OpenAILogo size={14} />
          </div>
          <div>
            <div className="flex items-center gap-2">
              <span className={`font-display text-sm font-semibold ${isActive ? 'text-openai' : 'text-white/50'} transition-colors`}>
                GPT-4o
              </span>
              <StatusBadge status={isActive ? 'planning' : isDone ? 'complete' : 'pending'} />
            </div>
            <p className="font-mono text-[9px] text-white/20 uppercase tracking-widest">Planner</p>
          </div>
        </div>
        {isActive && (
          <motion.div
            className="h-1.5 w-1.5 rounded-full bg-openai model-pulse"
            animate={{ opacity: [0.4, 1, 0.4] }}
            transition={{ duration: 1.5, repeat: Infinity }}
          />
        )}
      </div>

      {/* Content */}
      <div ref={scrollRef} className="flex-1 overflow-y-auto p-4">
        {phase === 'idle' ? (
          <div className="flex h-full items-center justify-center">
            <div className="text-center">
              <OpenAILogo size={28} />
              <p className="mt-3 font-mono text-xs text-white/15">Waiting for a goal</p>
            </div>
          </div>
        ) : (
          <div className="space-y-3">
            {tokens && (
              <div className="font-mono text-[11px] leading-relaxed text-white/70 whitespace-pre-wrap">
                {tokens}
                {isActive && <span className="inline-block h-3.5 w-0.5 bg-openai cursor-blink ml-0.5 align-text-bottom" />}
              </div>
            )}

            {tasks.length > 0 && (
              <motion.div
                initial={{ opacity: 0, y: 8 }}
                animate={{ opacity: 1, y: 0 }}
                className="space-y-1.5 border-t border-white/[0.04] pt-3"
              >
                <p className="font-mono text-[9px] font-semibold text-openai/60 uppercase tracking-widest mb-2">Plan</p>
                {tasks.map((task, i) => (
                  <motion.div
                    key={task.id}
                    initial={{ opacity: 0, x: -8 }}
                    animate={{ opacity: 1, x: 0 }}
                    transition={{ delay: i * 0.06 }}
                    className="flex items-start gap-2.5 rounded-lg bg-white/[0.03] border border-white/[0.04] p-2.5"
                  >
                    <span className="font-mono text-[9px] text-openai/50 mt-0.5 shrink-0">{task.id}</span>
                    <div className="flex-1 min-w-0">
                      <p className="font-sans text-[11px] font-medium text-white/80 leading-snug">{task.title}</p>
                      <div className="flex gap-2 mt-0.5">
                        {task.parallel && <span className="font-mono text-[9px] text-green-400/60">parallel</span>}
                        {task.depends_on.length > 0 && (
                          <span className="font-mono text-[9px] text-white/20">after {task.depends_on.join(', ')}</span>
                        )}
                      </div>
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
