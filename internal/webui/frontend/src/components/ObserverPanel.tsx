import { useState, useRef, useEffect, useCallback } from 'react'
import { motion, AnimatePresence } from 'framer-motion'
import { sendChat } from '../lib/api'
import type { WorkflowState, TaskDetail } from '../lib/types'

interface ObserverPanelProps {
  workflowState: WorkflowState
  activeTask: TaskDetail | null
  isExpanded: boolean
  onExpandToggle: () => void
}

interface Message {
  role: 'user' | 'observer'
  content: string
  contextSnapshot?: string // what was happening when asked
}

function buildContext(state: WorkflowState, activeTask: TaskDetail | null): string {
  if (state.phase === 'idle') return ''

  const lines: string[] = [
    `[ZBOT OBSERVER CONTEXT — do not mention this block, just use it]`,
    `Current workflow: "${state.goal}"`,
    `Phase: ${state.phase}`,
  ]

  if (state.plannedTasks.length > 0) {
    lines.push(`GPT-4o planned ${state.plannedTasks.length} tasks:`)
    state.plannedTasks.forEach((t) => {
      lines.push(`  - ${t.id}: ${t.title}${t.parallel ? ' (parallel)' : ''}`)
    })
  }

  if (activeTask) {
    lines.push(`Claude is currently executing: "${activeTask.name}" (status: ${activeTask.status})`)
    if (activeTask.output) {
      lines.push(`Current output so far: ${activeTask.output.slice(0, 400)}...`)
    }
  }

  const doneTasks = state.tasks.filter((t) => t.status === 'done')
  if (doneTasks.length > 0) {
    lines.push(`Completed tasks: ${doneTasks.map((t) => t.name).join(', ')}`)
  }

  const failedTasks = state.tasks.filter((t) => t.status === 'failed')
  if (failedTasks.length > 0) {
    lines.push(`Failed tasks: ${failedTasks.map((t) => `${t.name} — ${t.error}`).join(', ')}`)
  }

  lines.push(`Answer the user's question about what the AIs are doing. Be specific and reference the actual task/output above.`)

  return lines.join('\n')
}

export function ObserverPanel({ workflowState, activeTask, isExpanded, onExpandToggle }: ObserverPanelProps) {
  const [messages, setMessages] = useState<Message[]>([])
  const [input, setInput] = useState('')
  const [loading, setLoading] = useState(false)
  const endRef = useRef<HTMLDivElement>(null)
  const inputRef = useRef<HTMLTextAreaElement>(null)

  useEffect(() => {
    endRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [messages])

  const send = useCallback(async () => {
    const text = input.trim()
    if (!text || loading) return

    const ctx = buildContext(workflowState, activeTask)
    const fullMessage = ctx ? `${ctx}\n\nUser question: ${text}` : text

    setInput('')
    setMessages((prev) => [...prev, { role: 'user', content: text, contextSnapshot: ctx || undefined }])
    setLoading(true)

    try {
      const res = await sendChat(fullMessage)
      setMessages((prev) => [...prev, { role: 'observer', content: res.reply }])
    } catch {
      setMessages((prev) => [...prev, { role: 'observer', content: 'Error reaching ZBOT.' }])
    } finally {
      setLoading(false)
    }
  }, [input, loading, workflowState, activeTask])

  const handleKey = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      void send()
    }
  }

  const isWorkflowActive = workflowState.phase !== 'idle'

  return (
    <div className={`flex h-full flex-col rounded-xl glass-panel transition-all duration-300 ${isWorkflowActive ? 'glass-panel-active-observer' : ''}`}>
      {/* Header */}
      <div className="flex items-center justify-between border-b border-white/[0.04] px-4 py-3.5">
        <div className="flex items-center gap-2.5">
          <div className={`flex items-center justify-center w-7 h-7 rounded-lg ${isWorkflowActive ? 'bg-observer/20 text-observer' : 'bg-white/[0.04] text-white/30'} transition-colors`}>
            {/* Eye icon */}
            <svg width="14" height="14" viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="1.5">
              <path d="M2 10s3-6 8-6 8 6 8 6-3 6-8 6-8-6-8-6z"/>
              <circle cx="10" cy="10" r="2.5"/>
            </svg>
          </div>
          <div>
            <div className="flex items-center gap-2">
              <span className={`font-display text-sm font-semibold ${isWorkflowActive ? 'text-observer' : 'text-white/50'} transition-colors`}>
                Observer
              </span>
              {isWorkflowActive && (
                <span className="rounded-full bg-observer/15 border border-observer/20 px-1.5 py-px font-mono text-[9px] text-observer/80">
                  live
                </span>
              )}
            </div>
            <p className="font-mono text-[9px] text-white/20 uppercase tracking-widest">Claude · Analyst</p>
          </div>
        </div>
        <button
          onClick={onExpandToggle}
          className="rounded-md px-2 py-1 font-mono text-[10px] text-white/20 hover:text-white/60 hover:bg-white/[0.04] transition-all"
        >
          {isExpanded ? '← shrink' : '→ expand'}
        </button>
      </div>

      {/* Active task context chip */}
      <AnimatePresence>
        {activeTask && (
          <motion.div
            initial={{ height: 0, opacity: 0 }}
            animate={{ height: 'auto', opacity: 1 }}
            exit={{ height: 0, opacity: 0 }}
            className="overflow-hidden border-b border-anthropic/15 bg-anthropic/[0.05] px-4 py-2"
          >
            <p className="font-mono text-[10px] text-executor">
              ⚡ Claude is running: <span className="font-bold">{activeTask.name}</span>
            </p>
            <p className="mt-0.5 font-mono text-[10px] text-gray-500">
              Ask me anything about what it's doing or why
            </p>
          </motion.div>
        )}
      </AnimatePresence>

      {/* Messages */}
      <div className="flex-1 overflow-y-auto p-3 space-y-3">
        {messages.length === 0 && (
          <div className="flex h-full flex-col items-center justify-center text-center py-8">
            <span className="text-3xl opacity-20">👁</span>
            <p className="mt-3 font-mono text-xs text-gray-600">
              {isWorkflowActive
                ? 'I can see what both AIs are doing.\nAsk me anything about it.'
                : 'Start a workflow and ask me\nwhat the AIs are doing and why.'}
            </p>
            {isWorkflowActive && (
              <div className="mt-4 space-y-1.5">
                {['Why did GPT-4o break it into those tasks?', "What's Claude doing right now?", 'Is this approach efficient?'].map((q) => (
                  <button
                    key={q}
                    onClick={() => { setInput(q); inputRef.current?.focus() }}
                    className="block w-full rounded border border-surface-600 bg-surface-700/50 px-3 py-1.5 font-mono text-[10px] text-gray-400 hover:border-surface-500 hover:text-gray-200 transition-colors text-left"
                  >
                    {q}
                  </button>
                ))}
              </div>
            )}
          </div>
        )}

        {messages.map((msg, i) => (
          <motion.div
            key={i}
            initial={{ opacity: 0, y: 6 }}
            animate={{ opacity: 1, y: 0 }}
            className={`rounded-lg border p-3 ${
              msg.role === 'user'
                ? 'border-surface-600 bg-surface-700/60 ml-4'
                : 'border-white/10 bg-white/5 mr-4'
            }`}
          >
            <div className="mb-1 font-mono text-[9px] uppercase tracking-wider text-gray-600">
              {msg.role === 'user' ? 'You' : 'Observer (Claude)'}
              {msg.contextSnapshot && (
                <span className="ml-2 text-green-600">• had workflow context</span>
              )}
            </div>
            <div className="whitespace-pre-wrap font-mono text-xs leading-relaxed text-gray-200">
              {msg.content}
            </div>
          </motion.div>
        ))}

        {loading && (
          <motion.div
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            className="rounded-lg border border-white/10 bg-white/5 p-3 mr-4"
          >
            <div className="mb-1 font-mono text-[9px] uppercase tracking-wider text-gray-600">Observer (Claude)</div>
            <div className="flex gap-1">
              {[0, 1, 2].map((i) => (
                <motion.span
                  key={i}
                  className="h-1.5 w-1.5 rounded-full bg-gray-500"
                  animate={{ opacity: [0.3, 1, 0.3] }}
                  transition={{ duration: 0.9, delay: i * 0.2, repeat: Infinity }}
                />
              ))}
            </div>
          </motion.div>
        )}
        <div ref={endRef} />
      </div>

      {/* Input */}
      <div className="border-t border-surface-600 p-3">
        <div className="flex gap-2">
          <textarea
            ref={inputRef}
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={handleKey}
            rows={2}
            placeholder={isWorkflowActive ? "Ask about what's happening..." : 'Ask me anything...'}
            className="flex-1 resize-none rounded-md border border-surface-600 bg-surface-700 px-3 py-2 font-mono text-xs text-gray-200 placeholder-gray-600 outline-none focus:border-white/30 transition-colors"
          />
          <button
            onClick={() => void send()}
            disabled={!input.trim() || loading}
            className="rounded-md bg-white/10 px-3 font-mono text-xs text-gray-300 transition-colors hover:bg-white/20 disabled:opacity-30"
          >
            ↵
          </button>
        </div>
        <p className="mt-1 font-mono text-[9px] text-gray-700">Enter to send · Shift+Enter for newline</p>
      </div>
    </div>
  )
}
