import { useState, useEffect, useCallback, useRef } from 'react'
import { AnimatePresence, motion } from 'framer-motion'

interface Memory {
  id: string
  content: string
  source: string
  tags: string[]
  created_at: string
  score?: number
}

interface MemoriesResponse {
  total: number
  memories: Memory[]
}

interface MemoryPanelProps {
  open: boolean
  onClose: () => void
}

export function MemoryPanel({ open, onClose }: MemoryPanelProps) {
  const [memories, setMemories] = useState<Memory[]>([])
  const [total, setTotal] = useState(0)
  const [query, setQuery] = useState('')
  const [loading, setLoading] = useState(false)
  const debounceRef = useRef<ReturnType<typeof setTimeout>>()
  const inputRef = useRef<HTMLInputElement>(null)

  const fetchMemories = useCallback(async (q: string) => {
    setLoading(true)
    try {
      const params = new URLSearchParams()
      if (q) params.set('q', q)
      params.set('limit', '20')
      const res = await fetch(`/api/memories?${params}`)
      if (!res.ok) throw new Error('fetch failed')
      const data: MemoriesResponse = await res.json()
      setMemories(data.memories)
      setTotal(data.total)
    } catch {
      // silently fail
    } finally {
      setLoading(false)
    }
  }, [])

  // Load memories when panel opens.
  useEffect(() => {
    if (open) {
      fetchMemories(query)
      // Focus search input.
      setTimeout(() => inputRef.current?.focus(), 200)
    }
  }, [open]) // eslint-disable-line react-hooks/exhaustive-deps

  // Debounced search.
  useEffect(() => {
    if (!open) return
    if (debounceRef.current) clearTimeout(debounceRef.current)
    debounceRef.current = setTimeout(() => {
      fetchMemories(query)
    }, 300)
    return () => {
      if (debounceRef.current) clearTimeout(debounceRef.current)
    }
  }, [query, open, fetchMemories])

  const handleDelete = async (id: string) => {
    try {
      await fetch(`/api/memory/${id}`, { method: 'DELETE' })
      setMemories((prev) => prev.filter((m) => m.id !== id))
      setTotal((prev) => Math.max(0, prev - 1))
    } catch {
      // silently fail
    }
  }

  const formatAge = (dateStr: string) => {
    const date = new Date(dateStr)
    const now = new Date()
    const diffMs = now.getTime() - date.getTime()
    const diffHours = diffMs / (1000 * 60 * 60)
    if (diffHours < 1) return `${Math.round(diffMs / 60000)}m ago`
    if (diffHours < 24) return `${Math.round(diffHours)}h ago`
    const diffDays = Math.round(diffHours / 24)
    if (diffDays === 1) return '1 day ago'
    if (diffDays < 30) return `${diffDays}d ago`
    return date.toLocaleDateString()
  }

  const categoryColor = (tags: string[]) => {
    const tag = tags[0] || ''
    switch (tag) {
      case 'preference': return 'bg-purple-500/20 text-purple-400'
      case 'business': return 'bg-blue-500/20 text-blue-400'
      case 'technical': return 'bg-green-500/20 text-green-400'
      case 'personal': return 'bg-pink-500/20 text-pink-400'
      case 'workflow_insight': return 'bg-amber-500/20 text-amber-400'
      default: return 'bg-gray-500/20 text-gray-400'
    }
  }

  return (
    <AnimatePresence>
      {open && (
        <>
          {/* Backdrop */}
          <motion.div
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            className="fixed inset-0 z-40 bg-black/40"
            onClick={onClose}
          />

          {/* Panel */}
          <motion.div
            initial={{ x: '100%' }}
            animate={{ x: 0 }}
            exit={{ x: '100%' }}
            transition={{ type: 'spring', damping: 25, stiffness: 300 }}
            className="fixed right-0 top-0 z-50 flex h-full w-96 flex-col border-l border-surface-600 bg-[#0a0b0d]"
          >
            {/* Header */}
            <div className="flex items-center justify-between border-b border-surface-600 px-4 py-3">
              <div className="flex items-center gap-2">
                <span className="text-lg">🧠</span>
                <h2 className="font-display text-sm font-bold tracking-tight text-gray-100">
                  Memory
                </h2>
                <span className="rounded-full bg-amber-500/20 px-2 py-0.5 font-mono text-xs text-amber-400">
                  {total}
                </span>
              </div>
              <button
                onClick={onClose}
                className="rounded p-1 text-gray-500 transition-colors hover:bg-surface-700 hover:text-gray-300"
              >
                <svg width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="2">
                  <path d="M4 4l8 8M12 4l-8 8" />
                </svg>
              </button>
            </div>

            {/* Search */}
            <div className="border-b border-surface-700 px-4 py-3">
              <div className="relative">
                <input
                  ref={inputRef}
                  type="text"
                  value={query}
                  onChange={(e) => setQuery(e.target.value)}
                  placeholder="Search memories..."
                  className="w-full rounded-md border border-surface-600 bg-surface-800 px-3 py-2 pl-8 font-mono text-xs text-gray-200 placeholder-gray-600 outline-none transition-colors focus:border-amber-500/50"
                />
                <svg
                  className="absolute left-2.5 top-2.5 h-3.5 w-3.5 text-gray-600"
                  fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2"
                >
                  <circle cx="11" cy="11" r="8" />
                  <path d="M21 21l-4.35-4.35" />
                </svg>
                {loading && (
                  <div className="absolute right-2.5 top-2.5 h-3.5 w-3.5 animate-spin rounded-full border-2 border-amber-500/30 border-t-amber-500" />
                )}
              </div>
            </div>

            {/* Memory list */}
            <div className="flex-1 overflow-y-auto px-4 py-2">
              {memories.length === 0 && !loading && (
                <div className="flex flex-col items-center justify-center py-16 text-center">
                  <span className="text-3xl opacity-40">🧠</span>
                  <p className="mt-3 font-mono text-xs text-gray-600">
                    {query
                      ? 'No memories match your search'
                      : 'No memories yet — ZBOT will start remembering things as you work'}
                  </p>
                </div>
              )}

              <AnimatePresence mode="popLayout">
                {memories.map((m) => (
                  <motion.div
                    key={m.id}
                    layout
                    initial={{ opacity: 0, y: 10 }}
                    animate={{ opacity: 1, y: 0 }}
                    exit={{ opacity: 0, x: 50 }}
                    className="group mb-2 rounded-lg border border-surface-700 bg-surface-800/50 p-3 transition-colors hover:border-surface-600"
                  >
                    {/* Content */}
                    <p className="font-mono text-xs leading-relaxed text-gray-300">
                      {m.content.length > 300 ? m.content.slice(0, 300) + '...' : m.content}
                    </p>

                    {/* Footer */}
                    <div className="mt-2 flex items-center justify-between">
                      <div className="flex items-center gap-2">
                        {m.tags.length > 0 && (
                          <span className={`rounded-full px-1.5 py-0.5 font-mono text-[10px] ${categoryColor(m.tags)}`}>
                            {m.tags[0]}
                          </span>
                        )}
                        <span className="font-mono text-[10px] text-gray-600">
                          {formatAge(m.created_at)}
                        </span>
                        {m.score != null && m.score > 0 && (
                          <span className="font-mono text-[10px] text-gray-700">
                            {(m.score * 100).toFixed(0)}%
                          </span>
                        )}
                      </div>

                      <button
                        onClick={() => void handleDelete(m.id)}
                        className="rounded p-1 text-gray-700 opacity-0 transition-all hover:bg-red-500/10 hover:text-red-400 group-hover:opacity-100"
                        title="Delete memory"
                      >
                        <svg width="12" height="12" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="2">
                          <path d="M2 4h12M5.33 4V2.67a1.33 1.33 0 011.34-1.34h2.66a1.33 1.33 0 011.34 1.34V4M6.67 7.33v4M9.33 7.33v4" />
                          <path d="M3.33 4l.67 9.33a1.33 1.33 0 001.33 1.34h5.34a1.33 1.33 0 001.33-1.34L12.67 4" />
                        </svg>
                      </button>
                    </div>
                  </motion.div>
                ))}
              </AnimatePresence>
            </div>
          </motion.div>
        </>
      )}
    </AnimatePresence>
  )
}
