import { useState, useEffect, useCallback } from 'react'
import { motion, AnimatePresence } from 'framer-motion'
import type { AgentEvent } from '../hooks/useEventBus'

interface WorkspaceFile {
  name: string
  path: string
  size: number
  size_human: string
  extension: string
  updated_at: string
}

interface FileTreePaneProps {
  className?: string
  onClose?: () => void
  onSelectFile?: (path: string) => void
}

export function FileTreePane({ className = '', onClose, onSelectFile }: FileTreePaneProps) {
  const [files, setFiles] = useState<WorkspaceFile[]>([])
  const [activeFiles, setActiveFiles] = useState<Map<string, 'read' | 'write'>>(new Map())
  const [selectedPath, setSelectedPath] = useState<string | null>(null)
  const processedRef = new Set<string>()

  // Fetch workspace files
  const fetchFiles = useCallback(async () => {
    try {
      const res = await fetch('/api/workspace?sort=newest&limit=50')
      if (!res.ok) return
      const data = await res.json()
      setFiles(data.files || [])
    } catch { /* ignore */ }
  }, [])

  useEffect(() => { void fetchFiles() }, [fetchFiles])

  // Poll event bus for file_read/file_write events
  useEffect(() => {
    const poll = setInterval(() => {
      const events: AgentEvent[] = (window as any).__zbotEvents || []
      const newActive = new Map(activeFiles)
      let changed = false

      for (const evt of events) {
        if (processedRef.has(evt.id)) continue
        processedRef.add(evt.id)

        if (evt.type === 'file_read' && evt.detail?.path) {
          newActive.set(String(evt.detail.path), 'read')
          changed = true
        } else if (evt.type === 'file_write' && evt.detail?.path) {
          newActive.set(String(evt.detail.path), 'write')
          changed = true
          // Refresh file list on writes
          void fetchFiles()
        } else if (evt.type === 'turn_complete') {
          // Clear highlights when turn completes
          if (newActive.size > 0) {
            newActive.clear()
            changed = true
          }
        }
      }

      if (changed) setActiveFiles(newActive)
    }, 500)
    return () => clearInterval(poll)
  }, [activeFiles, fetchFiles])

  const handleSelect = (path: string) => {
    setSelectedPath(path)
    onSelectFile?.(path)
  }

  return (
    <div className={`flex h-full flex-col rounded-xl glass-panel ${className}`}>
      {/* Header */}
      <div className="flex items-center justify-between border-b border-white/[0.04] px-4 py-3">
        <div className="flex items-center gap-2.5">
          <div className="flex items-center justify-center w-7 h-7 rounded-lg bg-cyan-500/20 text-cyan-400">
            <svg width={14} height={14} viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.3">
              <path d="M2 3h5l1.5 1.5H14v9H2V3z" strokeLinejoin="round" />
            </svg>
          </div>
          <div>
            <span className="font-display text-sm font-semibold text-white/90">Files</span>
            <p className="font-mono text-[9px] text-white/20 uppercase tracking-widest">workspace</p>
          </div>
        </div>
        {onClose && (
          <button onClick={onClose} className="rounded-md p-1 text-white/20 hover:text-white/50 hover:bg-white/[0.04] transition-colors">
            <svg viewBox="0 0 16 16" className="w-3.5 h-3.5" fill="none" stroke="currentColor" strokeWidth="1.5">
              <path d="M4 4l8 8M12 4l-8 8" strokeLinecap="round" />
            </svg>
          </button>
        )}
      </div>

      {/* File list */}
      <div className="flex-1 overflow-y-auto p-2">
        <AnimatePresence>
          {files.map(file => {
            const activity = activeFiles.get(file.path)
            const isSelected = selectedPath === file.path
            return (
              <motion.button
                key={file.path}
                initial={{ opacity: 0 }}
                animate={{ opacity: 1 }}
                onClick={() => handleSelect(file.path)}
                className={`w-full flex items-center gap-2 rounded-md px-2.5 py-1.5 text-left transition-colors ${
                  isSelected ? 'bg-white/[0.06] border border-white/[0.08]' :
                  activity === 'write' ? 'bg-amber-500/[0.08] border border-amber-500/20' :
                  activity === 'read' ? 'bg-cyan-500/[0.06] border border-cyan-500/15' :
                  'border border-transparent hover:bg-white/[0.03]'
                }`}
              >
                <span className={`font-mono text-[10px] ${
                  activity === 'write' ? 'text-amber-400' :
                  activity === 'read' ? 'text-cyan-400' :
                  'text-white/30'
                }`}>
                  {file.extension === 'py' ? '🐍' :
                   file.extension === 'go' ? '⚙️' :
                   file.extension === 'md' ? '📝' :
                   file.extension === 'json' ? '{}' :
                   file.extension === 'ts' || file.extension === 'tsx' ? 'TS' :
                   '📄'}
                </span>
                <span className="font-mono text-[10px] text-white/60 truncate flex-1">{file.name}</span>
                <span className="font-mono text-[9px] text-white/20 shrink-0">{file.size_human}</span>
              </motion.button>
            )
          })}
        </AnimatePresence>

        {files.length === 0 && (
          <div className="flex flex-col items-center justify-center py-8 text-center">
            <p className="font-mono text-[10px] text-white/20">No files in workspace</p>
          </div>
        )}
      </div>
    </div>
  )
}
