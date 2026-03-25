import { useState, useRef, useEffect, useCallback } from 'react'
import { motion, AnimatePresence } from 'framer-motion'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { ActivityTimeline } from './ActivityTimeline'
import type { WorkflowState, ChatMessage, ModelTier, ToolCallEvent } from '../lib/types'

// Brain icon for Cortex (replaces Anthropic "A" logo)
function BrainIcon({ size = 18 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
      <path d="M12 2C9.5 2 7.5 4 7.5 6.5c0 .5.1 1 .2 1.4C6.1 8.5 5 10.1 5 12c0 1.5.6 2.8 1.5 3.8-.3.7-.5 1.4-.5 2.2 0 2.2 1.8 4 4 4 .8 0 1.5-.2 2-.6.5.4 1.2.6 2 .6 2.2 0 4-1.8 4-4 0-.8-.2-1.5-.5-2.2.9-1 1.5-2.3 1.5-3.8 0-1.9-1.1-3.5-2.7-4.1.1-.4.2-.9.2-1.4C16.5 4 14.5 2 12 2z"/>
      <path d="M12 2v20" opacity="0.3"/>
      <path d="M8 8.5c1.5 0 2.5 1 4 1s2.5-1 4-1" opacity="0.5"/>
      <path d="M7.5 13c1.5.5 3 .5 4.5.5s3 0 4.5-.5" opacity="0.5"/>
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

// Markdown components with dark theme styling
const markdownComponents = {
  p: ({ children }: any) => <p className="mb-2 last:mb-0">{children}</p>,
  h1: ({ children }: any) => <h1 className="text-sm font-semibold text-white/90 mb-2 mt-3">{children}</h1>,
  h2: ({ children }: any) => <h2 className="text-xs font-semibold text-white/85 mb-1.5 mt-2.5">{children}</h2>,
  h3: ({ children }: any) => <h3 className="text-xs font-medium text-white/80 mb-1 mt-2">{children}</h3>,
  ul: ({ children }: any) => <ul className="list-disc list-inside mb-2 space-y-0.5">{children}</ul>,
  ol: ({ children }: any) => <ol className="list-decimal list-inside mb-2 space-y-0.5">{children}</ol>,
  li: ({ children }: any) => <li className="text-white/70">{children}</li>,
  code: ({ inline, className, children }: any) => {
    if (inline) {
      return <code className="rounded bg-white/[0.08] px-1 py-0.5 font-mono text-[10px] text-cyan-300/80">{children}</code>
    }
    return (
      <pre className="rounded-md bg-white/[0.06] border border-white/[0.06] p-3 my-2 overflow-x-auto">
        <code className="font-mono text-[10px] text-emerald-300/70 leading-relaxed">{children}</code>
      </pre>
    )
  },
  blockquote: ({ children }: any) => (
    <blockquote className="border-l-2 border-anthropic/30 pl-3 my-2 text-white/50 italic">{children}</blockquote>
  ),
  a: ({ href, children }: any) => (
    <a href={href} target="_blank" rel="noopener noreferrer" className="text-cyan-400/80 hover:text-cyan-300 underline decoration-cyan-400/30">{children}</a>
  ),
  table: ({ children }: any) => (
    <div className="overflow-x-auto my-2">
      <table className="w-full border-collapse font-mono text-[10px]">{children}</table>
    </div>
  ),
  th: ({ children }: any) => <th className="border border-white/[0.08] bg-white/[0.04] px-2 py-1 text-left text-white/60">{children}</th>,
  td: ({ children }: any) => <td className="border border-white/[0.06] px-2 py-1 text-white/50">{children}</td>,
  strong: ({ children }: any) => <strong className="font-semibold text-white/85">{children}</strong>,
  em: ({ children }: any) => <em className="italic text-white/60">{children}</em>,
}

// Event bus types (passed from PaneManager)
interface EventBusData {
  events: Array<{type: string, summary: string}>
  connected: boolean
  cortexWorking: boolean
  recentTools: Array<{type: string, summary: string}>
}

interface ChatPaneProps {
  workflowState: WorkflowState
  className?: string
  eventBus?: EventBusData
}

export function ChatPane({ workflowState, className = '', eventBus }: ChatPaneProps) {
  const [messages, setMessages] = useState<ChatMessage[]>([])
  const [input, setInput] = useState('')
  const [loading, setLoading] = useState(false)
  const [streamEvents, setStreamEvents] = useState<Array<{type: string, content: string}>>([])
  const [eventsVisible, setEventsVisible] = useState(false)
  const endRef = useRef<HTMLDivElement>(null)
  const inputRef = useRef<HTMLTextAreaElement>(null)

  // Auto-scroll on new content
  useEffect(() => {
    endRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [messages, workflowState.agentTokens, workflowState.toolCalls])

  // Fade out event strip 2s after turn completes
  useEffect(() => {
    if (eventBus?.cortexWorking) {
      setEventsVisible(true)
    } else if (eventsVisible) {
      const timer = setTimeout(() => setEventsVisible(false), 2000)
      return () => clearTimeout(timer)
    }
  }, [eventBus?.cortexWorking, eventsVisible])

  const isStreaming = workflowState.phase === 'executing' && workflowState.agentTokens.length > 0

  const clearConversation = useCallback(() => {
    setMessages([])
    setStreamEvents([])
  }, [])

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
      const controller = new AbortController()
      const timeoutId = setTimeout(() => controller.abort(), 120000)
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
      let tokenCount = 0
      let costUSD = 0
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
              if (evt.tokens) tokenCount = evt.tokens
              if (evt.cost) costUSD = evt.cost
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
          tokens: tokenCount || undefined,
          cost: costUSD || undefined,
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

  // Use event bus from props (clean) instead of window global (hacky)
  const busEvents = eventBus?.events || []

  return (
    <div className={`flex h-full flex-col rounded-xl glass-panel ${className}`}>
      {/* Header */}
      <div className="flex items-center justify-between border-b border-white/[0.04] px-4 py-3">
        <div className="flex items-center gap-2.5">
          <div className="flex items-center justify-center w-7 h-7 rounded-lg bg-cyan-500/20 text-cyan-400">
            <BrainIcon size={16} />
          </div>
          <div>
            <div className="flex items-center gap-2">
              <span className="font-display text-sm font-semibold text-white/90">Cortex</span>
              <TierBadge tier={workflowState.modelTier} />
            </div>
            <p className="font-mono text-[9px] text-white/20 uppercase tracking-widest">cognitive engine</p>
          </div>
        </div>

        <div className="flex items-center gap-2">
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

          {/* Clear conversation button */}
          {messages.length > 0 && (
            <button
              onClick={clearConversation}
              className="rounded-md border border-white/[0.06] bg-white/[0.03] px-2 py-1 font-mono text-[9px] text-white/30 hover:text-white/60 hover:border-white/[0.12] transition-colors"
              title="Clear conversation"
            >
              Clear
            </button>
          )}
        </div>
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

      {/* Live event bus strip — uses eventBus prop, fades after turn completes */}
      <AnimatePresence>
        {eventsVisible && busEvents.length > 0 && (
          <motion.div
            initial={{ height: 0, opacity: 0 }}
            animate={{ height: 'auto', opacity: 1 }}
            exit={{ height: 0, opacity: 0 }}
            transition={{ duration: 0.3 }}
            className="overflow-hidden border-b border-cyan-500/20 bg-cyan-500/[0.03]"
          >
            <div className="flex items-center gap-2 px-4 py-2 overflow-x-auto">
              <span className="font-mono text-[9px] text-cyan-400/40 uppercase tracking-widest shrink-0">cortex</span>
              {busEvents.slice(-3).map((evt: any, i: number) => (
                <div key={i} className="flex items-center gap-1.5 rounded-md border border-cyan-500/15 bg-cyan-500/[0.04] px-2 py-1">
                  <span className={`font-mono text-[10px] ${
                    evt.type === 'plan_start' || evt.type === 'plan_complete' ? 'text-violet-400' :
                    evt.type === 'verify_start' ? 'text-amber-400' :
                    evt.type === 'verify_complete' ? 'text-emerald-400' :
                    evt.type === 'tool_error' ? 'text-red-400' :
                    evt.type === 'tool_result' ? 'text-emerald-400' :
                    evt.type === 'memory_loaded' ? 'text-blue-400' :
                    'text-cyan-400/60'
                  }`}>
                    {evt.type === 'plan_start' ? '◉' :
                     evt.type === 'plan_complete' ? '◈' :
                     evt.type === 'verify_start' ? '◎' :
                     evt.type === 'verify_complete' ? '✓' :
                     evt.type === 'memory_loaded' ? '⧫' :
                     evt.type === 'tool_called' ? '⟳' :
                     evt.type === 'tool_result' ? '✓' :
                     evt.type === 'tool_error' ? '✗' : '→'}
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
            <BrainIcon size={32} />
            <p className="mt-4 font-mono text-xs text-white/20">
              Cortex ready. Ask anything or give a task.
            </p>
            <div className="mt-4 space-y-1.5">
              {[
                'Audit workflows in Esler CST',
                'Research competitors and write a report',
                'How many contacts are DND?',
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
            <div className="flex items-center justify-between mb-1.5">
              <div className="flex items-center gap-2">
                <span className="font-mono text-[9px] uppercase tracking-wider text-white/30">
                  {msg.role === 'user' ? 'You' : 'Cortex'}
                </span>
                {msg.modelTier && <TierBadge tier={msg.modelTier} />}
              </div>
              {/* Token/cost display per message */}
              {msg.tokens && (
                <span className="font-mono text-[9px] text-white/15">
                  {msg.tokens.toLocaleString()} tok
                  {msg.cost ? ` · $${msg.cost.toFixed(4)}` : ''}
                </span>
              )}
            </div>
            {msg.toolCalls && msg.toolCalls.length > 0 && (
              <div className="flex flex-wrap gap-1 mb-2">
                {msg.toolCalls.map((tool, i) => (
                  <ToolCallChip key={`${tool.name}-${i}`} tool={tool} />
                ))}
              </div>
            )}
            {/* Render markdown for assistant messages, plain text for user */}
            {msg.role === 'assistant' ? (
              <div className="prose-zbot font-mono text-[11px] leading-relaxed text-white/70">
                <ReactMarkdown remarkPlugins={[remarkGfm]} components={markdownComponents}>
                  {msg.content}
                </ReactMarkdown>
              </div>
            ) : (
              <div className="whitespace-pre-wrap font-mono text-[11px] leading-relaxed text-white/70">
                {msg.content}
              </div>
            )}
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
              <span className="font-mono text-[9px] uppercase tracking-wider text-white/30">Cortex</span>
              <TierBadge tier={workflowState.modelTier} />
            </div>
            <div className="prose-zbot font-mono text-[11px] leading-relaxed text-white/70">
              <ReactMarkdown remarkPlugins={[remarkGfm]} components={markdownComponents}>
                {workflowState.agentTokens}
              </ReactMarkdown>
              <span className="inline-block h-3 w-0.5 bg-anthropic cursor-blink ml-0.5 align-text-bottom" />
            </div>
          </motion.div>
        )}

        {/* Live activity indicator during loading */}
        {loading && !isStreaming && (
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
                      evt.type === 'plan_start' ? 'text-violet-400/80' :
                      evt.type === 'plan_complete' ? 'text-violet-300/80' :
                      evt.type === 'verify_start' ? 'text-amber-400/80' :
                      evt.type === 'verify_complete' && evt.content.includes('APPROVED') ? 'text-emerald-400/80' :
                      evt.type === 'verify_complete' ? 'text-amber-400/80' :
                      evt.type === 'memory_loaded' ? 'text-blue-400/60' :
                      evt.type === 'tool_called' ? 'text-cyan-400/60' :
                      evt.type === 'tool_result' ? 'text-emerald-400/60' :
                      evt.type === 'tool_error' ? 'text-red-400/60' :
                      'text-white/30'
                    }`}>
                      {evt.type === 'plan_start' ? '◉' :
                       evt.type === 'plan_complete' ? '◈' :
                       evt.type === 'verify_start' ? '◎' :
                       evt.type === 'verify_complete' && evt.content.includes('APPROVED') ? '✓' :
                       evt.type === 'verify_complete' ? '↻' :
                       evt.type === 'memory_loaded' ? '⧫' :
                       evt.type === 'tool_called' ? '⟳' :
                       evt.type === 'tool_result' ? '✓' :
                       evt.type === 'tool_error' ? '✗' :
                       evt.type === 'turn_start' ? '→' : '·'}
                    </span>
                    <span className={`font-mono text-[10px] ${
                      evt.type === 'verify_complete' && evt.content.includes('APPROVED') ? 'text-emerald-400/60' :
                      evt.type === 'verify_complete' ? 'text-amber-400/60' :
                      'text-white/40'
                    }`}>{evt.content}</span>
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

      {/* Activity Timeline — persistent strip above input */}
      <AnimatePresence>
        {eventBus && (
          <ActivityTimeline events={eventBus.events} cortexWorking={eventBus.cortexWorking} />
        )}
      </AnimatePresence>

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
