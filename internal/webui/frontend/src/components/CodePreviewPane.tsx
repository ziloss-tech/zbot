import { useState, useEffect } from 'react'
import { motion } from 'framer-motion'

interface CodePreviewPaneProps {
  filePath: string | null
  className?: string
  onClose?: () => void
}

export function CodePreviewPane({ filePath, className = '', onClose }: CodePreviewPaneProps) {
  const [content, setContent] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!filePath) {
      setContent(null)
      return
    }

    setLoading(true)
    setError(null)

    fetch(`/api/workspace/preview?path=${encodeURIComponent(filePath)}`)
      .then(res => {
        if (!res.ok) throw new Error(`HTTP ${res.status}`)
        return res.text()
      })
      .then(text => {
        setContent(text)
        setLoading(false)
      })
      .catch(err => {
        setError(err.message)
        setLoading(false)
      })
  }, [filePath])

  // Auto-refresh when file_write events target this file
  useEffect(() => {
    if (!filePath) return
    const poll = setInterval(() => {
      const events: any[] = (window as any).__zbotEvents || []
      const hasWrite = events.some(e =>
        e.type === 'file_write' && e.detail?.path && String(e.detail.path).endsWith(filePath)
      )
      if (hasWrite) {
        fetch(`/api/workspace/preview?path=${encodeURIComponent(filePath)}`)
          .then(res => res.ok ? res.text() : '')
          .then(text => { if (text) setContent(text) })
          .catch(() => {})
      }
    }, 1000)
    return () => clearInterval(poll)
  }, [filePath])

  const lines = content?.split('\n') || []
  const ext = filePath?.split('.').pop() || ''

  return (
    <div className={`flex h-full flex-col rounded-xl glass-panel ${className}`}>
      {/* Header */}
      <div className="flex items-center justify-between border-b border-white/[0.04] px-4 py-3">
        <div className="flex items-center gap-2.5">
          <div className="flex items-center justify-center w-7 h-7 rounded-lg bg-amber-500/20 text-amber-400">
            <svg width={14} height={14} viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.3">
              <path d="M4 2h6l3 3v9H4V2z" strokeLinejoin="round" />
              <path d="M10 2v3h3" strokeLinecap="round" />
            </svg>
          </div>
          <div>
            <span className="font-display text-sm font-semibold text-white/90">
              {filePath ? filePath.split('/').pop() : 'Preview'}
            </span>
            {filePath && (
              <p className="font-mono text-[9px] text-white/20 truncate max-w-[200px]">{filePath}</p>
            )}
          </div>
          {ext && (
            <span className="inline-flex items-center rounded-full px-2 py-0.5 font-mono text-[9px] bg-amber-500/15 text-amber-400">
              {ext}
            </span>
          )}
        </div>
        {onClose && (
          <button onClick={onClose} className="rounded-md p-1 text-white/20 hover:text-white/50 hover:bg-white/[0.04] transition-colors">
            <svg viewBox="0 0 16 16" className="w-3.5 h-3.5" fill="none" stroke="currentColor" strokeWidth="1.5">
              <path d="M4 4l8 8M12 4l-8 8" strokeLinecap="round" />
            </svg>
          </button>
        )}
      </div>

      {/* Content */}
      <div className="flex-1 overflow-auto">
        {!filePath && (
          <div className="flex h-full items-center justify-center">
            <p className="font-mono text-[10px] text-white/20">Select a file to preview</p>
          </div>
        )}

        {loading && (
          <div className="flex h-full items-center justify-center">
            <motion.span
              className="font-mono text-[10px] text-amber-400/60"
              animate={{ opacity: [1, 0.4, 1] }}
              transition={{ duration: 1.5, repeat: Infinity }}
            >
              Loading...
            </motion.span>
          </div>
        )}

        {error && (
          <div className="p-4">
            <p className="font-mono text-[10px] text-red-400">Error: {error}</p>
          </div>
        )}

        {content !== null && !loading && (
          <div className="flex text-[11px] font-mono leading-relaxed">
            {/* Line numbers */}
            <div className="select-none border-r border-white/[0.04] bg-white/[0.01] px-3 py-3 text-right text-white/15">
              {lines.map((_, i) => (
                <div key={i}>{i + 1}</div>
              ))}
            </div>
            {/* Code content */}
            <pre className="flex-1 overflow-x-auto px-4 py-3 text-white/60 whitespace-pre">
              {content}
            </pre>
          </div>
        )}
      </div>
    </div>
  )
}
