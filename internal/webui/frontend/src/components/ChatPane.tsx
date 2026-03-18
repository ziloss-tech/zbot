import { useState, useRef, useEffect, useCallback } from 'react'
import { motion, AnimatePresence } from 'framer-motion'
import { sendChat } from '../lib/api'
import type { WorkflowState, ChatMessage, ModelTier, ToolCallEvent } from '../lib/types'

// Anthropic logomark
function ClaudeLogo({ size = 18 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="currentColor">
      <path d="M13.827 3.52h-3.654L5.063 20.48h3.332l1.138-3.192h5.062l1.138 3.192h3.332L13.827 3.52zm-3.645 11.025 1.822-5.117 1.822 5.117h-3.644z"/>
    </svg>
  )
}

const tierColors: Record<ModelTier, { bg: string; text: string; label: string }> = {
  haiku:  { bg: 'bg-emerald-500/15', text: 'text-emerald-400', label: 'Haiku' },
  sonnet: { bg: 'bg-anthropic/15',   text: 'text-anthropic',   label: 'Sonnet' },
  opus:   { bg: 'bg-purple-500/15',  text: 'text-purple-400',  label: 'Opus' },
}

function TierBadge({ tier }: { tier: ModelTier }) {
  const cfg = tierColors[tier]
  return (
    <span className={`inline-flex items-center gap-1 rounded-full px-2 py-0.5 font-mono text-[9px] ${cfg.bg} ${cfg.text}`}>
      <span className="h-1 w-1 rounded-full bg-current" />
      {cfg.label}
    </span>
  )
}

function ToolCallChip({ tool }: { tool: ToolCallEvent }) {
  const statusIcon = tool.status === 'running' ? '⟳' : tool.status === 'done' ? '✓' : '✗'
  const statusColor = tool.status === 'running' ? 'text-anthropic' : tool.status === 'done' ? 'text-emerald-400' : 'text-red-400'

  return (
    <div className="flex items-center gap-2 rounded-md border border-white/[0.06] bg-white/[0.03] px-2.5 py-1.5">
      <span className={`font-mono text-[10px] ${statusColor}`}>{statusIcon}</span>
      <span className="font-mono text-[10px] text-white/60">{tool.name}</span>
      {tool.status === 'running' && (
        <motion.span
          className="h-1 w-1 rounded-full bg-anthropic"
          animate={{ opacity: [1, 0.3, 1] }}
          transition={{ duration: 1, repeat: Infinity }}
        />
      )}
    </div>
  )
}

interface ChatPaneProps {
  workflowState: WorkflowState
  className?: string
}

export function ChatPane({ workflowState, className = '' }: ChatPaneProps) {
  const [messages, setMessages] = useState<ChatMessage[]>([])
  const [input, setInput] = useState('')
  const [loading, setLoading] = useState(false)
  const [streamEvents, setStreamEvents] = useState<Array<{type: string, content: string}>>([])
  const endRef = useRef<HTMLDivElement>(null)
  const inputRef = useRef<HTMLTextAreaElement>(null)

  // Auto-scroll on new content
  useEffect(() => {
    endRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [messages, workflowState.agentTokens, workflowState.toolCalls])

  // Show streaming agent output as a live message
  const isStreaming = workflowState.phase === 'executing' && workflowState.agentTokens.length > 0

  const send = useCallback(async () => {
    const text = input.trim()
    if (!text || loading) return

    const userMsg: ChatMessage = {
      id: `user-${Date.now()}`,
      role: 'user',
      content: text,
      timestamp: Date.now(),
    }

    setInput('')
    setMessages((prev) => [...prev, userMsg])
    setLoading(true)

    try {
      // Use streaming endpoint — shows tool calls as they happen,
      // then delivers the final reply.
      const controller = new AbortController()
      const timeoutId = setTimeout(() => controller.abort(), 120000) // 2 min timeout
      const res = await fetch('/api/chat/stream', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ message: text }),
        signal: controller.signal,
      })

      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      if (!res.body) throw new Error('No response body')

      const reader = res.body.getReader()
      const decoder = new TextDecoder()
      let buffer = ''
      let finalReply = ''
      clearTimeout(timeoutId)
      setStreamEvents([])

      while (true) {
        const { done, value } = await reader.read()
        if (done) break

        buffer += decoder.decode(value, { stream: true })
        const lines = buffer.split('\n')
        buffer = lines.pop() || ''

        for (const line of lines) {
          if (!line.startsWith('data: ')) continue
          try {
            const evt = JSON.parse(line.slice(6))
            if (evt.type === 'done') {
              finalReply = evt.content
            } else if (evt.type === 'error') {
              finalReply = 'Error: ' + evt.content
            } else if (evt.type !== 'turn_complete') {
              setStreamEvents(prev => [...prev.slice(-8), { type: evt.type, content: evt.content }])
            }
          } catch { /* ignore parse errors */ }
        }
      }
      setStreamEvents([])

      if (finalReply) {
        const assistantMsg: ChatMessage = {
          id: `assistant-${Date.now()}`,
          role: 'assistant',
          content: finalReply,
          timestamp: Date.now(),
          modelTier: workflowState.modelTier,
        }
        setMessages((prev) => [...prev, assistantMsg])
      }
    } catch {
      const errorMsg: ChatMessage = {
        id: `error-${Date.now()}`,
        role: 'assistant',
        content: 'Error reaching Cortex.',
        timestamp: Date.now(),
      }
      setMessages((prev) => [...prev, errorMsg])
    } finally {
      setLoading(false)
    }
  }, [input, loading, workflowState.modelTier])

  const handleKey = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      void send()
    }
  }

  return (
    <div className={`flex h-full flex-col rounded-xl glass-panel ${className}`}>
      {/* Header */}
      <div className="flex items-center justify-between border-b border-white/[0.04] px-4 py-3">
        <div className="flex items-center gap-2.5">
          <div className="flex items-center justify-center w-7 h-7 rounded-lg bg-anthropic/20 text-anthropic">
            <ClaudeLogo size={14} />
          </div>
          <div>
            <div className="flex items-center gap-2">
              <span className="font-display text-sm font-semibold text-white/90">Cortex</span>
              <TierBadge tier={workflowState.modelTier} />
            </div>
            <p className="font-mono text-[9px] text-white/20 uppercase tracking-widest">Cortex engine</p>
          </div>
        </div>

        {/* Phase indicator */}
        {workflowState.phase !== 'idle' && (
          <div className="flex items-center gap-2">
            {workflowState.phase === 'executing' && (
              <motion.div
                className="h-1.5 w-1.5 rounded-full bg-anthropic"
                animate={{ opacity: [1, 0.3, 1] }}
                transition={{ duration: 1.2, repeat: Infinity }}
              />
            )}
            <span className="font-mono text-[10px] text-white/30 capitalize">{workflowState.phase}</span>
          </div>
        )}
      </div>

      {/* Thinking indicator */}
      <AnimatePresence>
        {workflowState.agentThinking && (
          <motion.div
            initial={{ height: 0, opacity: 0 }}
            animate={{ height: 'auto', opacity: 1 }}
            exit={{ height: 0, opacity: 0 }}
            className="overflow-hidden border-b border-purple-500/20 bg-purple-500/[0.06]"
          >
            <div className="flex items-center gap-2 px-4 py-2">
              <motion.span
                className="font-mono text-[10px] text-purple-400"
                animate={{ opacity: [1, 0.4, 1] }}
                transition={{ duration: 1.5, repeat: Infinity }}
              >
                thinking...
              </motion.span>
              <span className="font-mono text-[10px] text-purple-400/60 truncate">
                {workflowState.agentThinking.slice(-80)}
              </span>
            </div>
          </motion.div>
        )}
      </AnimatePresence>

      {/* Live event bus strip — shows real-time Cortex activity */}
      <AnimatePresence>
        {(window as any).__zbotEvents?.length > 0 && (
          <motion.div
            initial={{ height: 0, opacity: 0 }}
            animate={{ height: 'auto', opacity: 1 }}
            exit={{ height: 0, opacity: 0 }}
            className="overflow-hidden border-b border-cyan-500/20 bg-cyan-500/[0.03]"
          >
            <div className="flex items-center gap-2 px-4 py-2 overflow-x-auto">
              <span className="font-mono text-[9px] text-cyan-400/40 uppercase tracking-widest shrink-0">cortex</span>
              {((window as any).__zbotEvents || []).slice(-3).map((evt: any, i: number) => (
                <div key={i} className="flex items-center gap-1.5 rounded-md border border-cyan-500/15 bg-cyan-500/[0.04] px-2 py-1">
                  <span className={`font-mono text-[10px] ${
                    evt.type === 'tool_error' ? 'text-red-400' :
                    evt.type === 'tool_result' ? 'text-emerald-400' :
                    'text-cyan-400/60'
                  }`}>
                    {evt.type === 'tool_called' ? '⟳' : evt.type === 'tool_result' ? '✓' : evt.type === 'tool_error' ? '✗' : '→'}
                  </span>
                  <span className="font-mono text-[10px] text-white/50">{evt.summary}</span>
                </div>
              ))}
            </div>
          </motion.div>
        )}
      </AnimatePresence>

      {/* Tool calls strip */}
      <AnimatePresence>
        {workflowState.toolCalls.length > 0 && workflowState.toolCalls.some(t => t.status === 'running') && (
          <motion.div
            initial={{ height: 0, opacity: 0 }}
            animate={{ height: 'auto', opacity: 1 }}
            exit={{ height: 0, opacity: 0 }}
            className="overflow-hidden border-b border-white/[0.04] bg-white/[0.02]"
          >
            <div className="flex items-center gap-2 px-4 py-2 overflow-x-auto">
              <span className="font-mono text-[9px] text-white/20 uppercase tracking-widest shrink-0">tools</span>
              {workflowState.toolCalls.slice(-3).map((tool, i) => (
                <ToolCallChip key={`${tool.name}-${i}`} tool={tool} />
              ))}
            </div>
          </motion.div>
        )}
      </AnimatePresence>

      {/* Messages area */}
      <div className="flex-1 overflow-y-auto p-4 space-y-4">
        {messages.length === 0 && !isStreaming && (
          <div className="flex h-full flex-col items-center justify-center text-center py-8">
            <ClaudeLogo size={32} />
            <p className="mt-4 font-mono text-xs text-white/20">
              Cortex ready. Ask anything or give a task.
            </p>
            <div className="mt-4 space-y-1.5">
              {[
                'Deploy the staging branch',
                'Research competitors and write a report',
                'Refactor the auth module',
              ].map((q) => (
                <button
                  key={q}
                  onClick={() => { setInput(q); inputRef.current?.focus() }}
                  className="block w-full rounded-lg border border-white/[0.06] bg-white/[0.03] px-3 py-2 font-mono text-[10px] text-white/40 hover:border-white/[0.12] hover:text-white/70 transition-colors text-left"
                >
                  {q}
                </button>
              ))}
            </div>
          </div>
        )}

        {messages.map((msg) => (
          <motion.div
            key={msg.id}
            initial={{ opacity: 0, y: 6 }}
            animate={{ opacity: 1, y: 0 }}
            className={`rounded-lg border p-3 ${
              msg.role === 'user'
                ? 'border-white/[0.06] bg-white/[0.04] ml-8'
                : 'border-white/[0.04] bg-white/[0.02] mr-4'
            }`}
          >
            <div className="flex items-center gap-2 mb-1.5">
              <span className="font-mono text-[9px] uppercase tracking-wider text-white/30">
                {msg.role === 'user' ? 'You' : 'Cortex'}
              </span>
              {msg.modelTier && <TierBadge tier={msg.modelTier} />}
            </div>
            {msg.toolCalls && msg.toolCalls.length > 0 && (
              <div className="flex flex-wrap gap-1 mb-2">
                {msg.toolCalls.map((tool, i) => (
                  <ToolCallChip key={`${tool.name}-${i}`} tool={tool} />
                ))}
              </div>
            )}
            <div className="whitespace-pre-wrap font-mono text-[11px] leading-relaxed text-white/70">
              {msg.content}
            </div>
          </motion.div>
        ))}

        {/* Live streaming message */}
        {isStreaming && (
          <motion.div
            initial={{ opacity: 0, y: 6 }}
            animate={{ opacity: 1, y: 0 }}
            className="rounded-lg border border-anthropic/20 bg-anthropic/[0.04] p-3 mr-4"
          >
            <div className="flex items-center gap-2 mb-1.5">
              <span className="font-mono text-[9px] uppercase tracking-wider text-white/30">Claude</span>
              <TierBadge tier={workflowState.modelTier} />
            </div>
            <div className="whitespace-pre-wrap font-mono text-[11px] leading-relaxed text-white/70">
              {workflowState.agentTokens}
              <span className="inline-block h-3 w-0.5 bg-anthropic cursor-blink ml-0.5 align-text-bottom" />
            </div>
          </motion.div>
        )}

        {/* Live activity indicator during streaming */}
        {loading && (
          <motion.div
            initial={{ opacity: 0, y: 6 }}
            animate={{ opacity: 1, y: 0 }}
            className="rounded-lg border border-cyan-500/20 bg-cyan-500/[0.03] p-3 mr-4"
          >
            <div className="flex items-center gap-2 mb-2">
              <motion.span
                className="h-2 w-2 rounded-full bg-cyan-400"
                animate={{ opacity: [1, 0.3, 1] }}
                transition={{ duration: 1, repeat: Infinity }}
              />
              <span className="font-mono text-[10px] text-cyan-400/70 uppercase tracking-wider">Cortex working</span>
            </div>
            {streamEvents.length > 0 ? (
              <div className="space-y-1">
                {streamEvents.map((evt, i) => (
                  <motion.div
                    key={i}
                    initial={{ opacity: 0, x: -4 }}
                    animate={{ opacity: 1, x: 0 }}
                    className="flex items-center gap-2"
                  >
                    <span className={`font-mono text-[10px] ${
                      evt.type === 'tool_called' ? 'text-cyan-400/60' :
                      evt.type === 'tool_result' ? 'text-emerald-400/60' :
                      evt.type === 'tool_error' ? 'text-red-400/60' :
                      'text-white/30'
                    }`}>
                      {evt.type === 'tool_called' ? '⟳' :
                       evt.type === 'tool_result' ? '✓' :
                       evt.type === 'tool_error' ? '✗' :
                       evt.type === 'turn_start' ? '→' :
                       evt.type === 'memory_loaded' ? '🧠' : '·'}
                    </span>
                    <span className="font-mono text-[10px] text-white/40">{evt.content}</span>
                  </motion.div>
                ))}
              </div>
            ) : (
              <div className="flex gap-1">
                {[0, 1, 2].map((i) => (
                  <motion.span
                    key={i}
                    className="h-1 w-1 rounded-full bg-cyan-400/40"
                    animate={{ opacity: [0.3, 1, 0.3] }}
                    transition={{ duration: 0.9, delay: i * 0.2, repeat: Infinity }}
                  />
                ))}
              </div>
            )}
          </motion.div>
        )}

        <div ref={endRef} />
      </div>

      {/* Input */}
      <div className="border-t border-white/[0.04] p-3">
        <div className="flex gap-2">
          <textarea
            ref={inputRef}
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={handleKey}
            rows={2}
            placeholder="Message Cortex..."
            className="flex-1 resize-none rounded-lg border border-white/[0.06] bg-white/[0.03] px-3 py-2.5 font-mono text-xs text-white/80 placeholder-white/20 outline-none focus:border-anthropic/40 transition-colors"
          />
          <button
            onClick={() => void send()}
            disabled={!input.trim() || loading}
            className="rounded-lg bg-anthropic/20 px-4 font-mono text-xs text-anthropic transition-all hover:bg-anthropic/30 disabled:opacity-30 disabled:cursor-not-allowed"
          >
            ↵
          </button>
        </div>
        <div className="flex items-center justify-between mt-1.5">
          <p className="font-mono text-[9px] text-white/15">Enter to send · Shift+Enter for newline</p>
          <p className="font-mono text-[9px] text-white/15">/think · /deep · /research</p>
        </div>
      </div>
    </div>
  )
}
