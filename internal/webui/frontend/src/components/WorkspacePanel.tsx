import { useState, useEffect, useCallback, useRef } from 'react'
import { AnimatePresence, motion } from 'framer-motion'

interface WorkspaceFile {
  name: string
  path: string
  size: number
  size_human: string
  extension: string
  created_at: string
  updated_at: string
  workflow_id?: string
}

interface WorkspaceResponse {
  files: WorkspaceFile[]
  total: number
  workspace_path: string
}

interface WorkspacePanelProps {
  open: boolean
  onClose: () => void
  onPreview: (path: string) => void
}

type FilterTab = 'all' | 'md' | 'csv' | 'json' | 'py' | 'other'
type SortMode = 'newest' | 'oldest' | 'largest'

const FILE_ICONS: Record<string, string> = {
  md: '📄',
  csv: '📊',
  json: '{}',
  py: '💻',
  js: '💻',
  ts: '💻',
  go: '💻',
  sh: '💻',
  pdf: '📕',
  png: '🖼️',
  jpg: '🖼️',
  jpeg: '🖼️',
  html: '🌐',
  txt: '📄',
}

const FILTER_TABS: { label: string; value: FilterTab }[] = [
  { label: 'All', value: 'all' },
  { label: '.md', value: 'md' },
  { label: '.csv', value: 'csv' },
  { label: '.json', value: 'json' },
  { label: '.py', value: 'py' },
  { label: 'Other', value: 'other' },
]

const KNOWN_EXTS = new Set(['md', 'csv', 'json', 'py'])

export function WorkspacePanel({ open, onClose, onPreview }: WorkspacePanelProps) {
  const [files, setFiles] = useState<WorkspaceFile[]>([])
  const [total, setTotal] = useState(0)
  const [wsPath, setWsPath] = useState('')
  const [filter, setFilter] = useState<FilterTab>('all')
  const [sort, setSort] = useState<SortMode>('newest')
  const [search, setSearch] = useState('')
  const [loading, setLoading] = useState(false)
  const searchRef = useRef<HTMLInputElement>(null)
  const pollRef = useRef<ReturnType<typeof setInterval>>()

  const fetchFiles = useCallback(async () => {
    setLoading(true)
    try {
      const params = new URLSearchParams()
      if (filter !== 'all' && filter !== 'other') params.set('ext', filter)
      params.set('sort', sort)
      params.set('limit', '100')
      const res = await fetch(`/api/workspace?${params}`)
      if (!res.ok) throw new Error('fetch failed')
      const data: WorkspaceResponse = await res.json()
      setFiles(data.files)
      setTotal(data.total)
      setWsPath(data.workspace_path)
    } catch {
      // silently fail
    } finally {
      setLoading(false)
    }
  }, [filter, sort])

  // Fetch on open + when filter/sort changes.
  useEffect(() => {
    if (open) {
      fetchFiles()
      setTimeout(() => searchRef.current?.focus(), 200)
    }
  }, [open, fetchFiles])

  // Auto-refresh every 10 seconds.
  useEffect(() => {
    if (open) {
      pollRef.current = setInterval(fetchFiles, 10000)
    }
    return () => {
      if (pollRef.current) clearInterval(pollRef.current)
    }
  }, [open, fetchFiles])

  const handleDelete = async (path: string) => {
    try {
      await fetch(`/api/workspace/file?path=${encodeURIComponent(path)}`, { method: 'DELETE' })
      setFiles((prev) => prev.filter((f) => f.path !== path))
      setTotal((prev) => Math.max(0, prev - 1))
    } catch {
      // silently fail
    }
  }

  const handleDownload = (path: string) => {
    window.open(`/api/workspace/download?path=${encodeURIComponent(path)}`, '_blank')
  }

  const formatAge = (dateStr: string) => {
    const date = new Date(dateStr)
    const now = new Date()
    const diffMs = now.getTime() - date.getTime()
    const diffMin = diffMs / 60000
    if (diffMin < 1) return 'just now'
    if (diffMin < 60) return `${Math.round(diffMin)} min ago`
    const diffHours = diffMin / 60
    if (diffHours < 24) return `${Math.round(diffHours)}h ago`
    const diffDays = Math.round(diffHours / 24)
    if (diffDays === 1) return '1 day ago'
    if (diffDays < 30) return `${diffDays}d ago`
    return date.toLocaleDateString()
  }

  const getIcon = (ext: string) => FILE_ICONS[ext] || '📁'

  // Apply client-side text search and "other" filter.
  const displayed = files.filter((f) => {
    if (filter === 'other' && KNOWN_EXTS.has(f.extension)) return false
    if (search && !f.name.toLowerCase().includes(search.toLowerCase())) return false
    return true
  })

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
            className="fixed right-0 top-0 z-50 flex h-full w-[420px] flex-col border-l border-surface-600 bg-[#0a0b0d]"
          >
            {/* Header */}
            <div className="flex items-center justify-between border-b border-surface-600 px-4 py-3">
              <div className="flex items-center gap-2">
                <span className="text-lg">📁</span>
                <h2 className="font-display text-sm font-bold tracking-tight text-gray-100">
                  Workspace
                </h2>
                <span className="rounded-full bg-violet-500/20 px-2 py-0.5 font-mono text-xs text-violet-400">
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

            {/* Workspace path + search */}
            <div className="border-b border-surface-700 px-4 py-3">
              <div className="mb-2 flex items-center justify-between">
                <span className="font-mono text-[10px] text-gray-600">{wsPath}</span>
                {/* Sort toggle */}
                <select
                  value={sort}
                  onChange={(e) => setSort(e.target.value as SortMode)}
                  className="rounded border border-surface-600 bg-surface-800 px-1.5 py-0.5 font-mono text-[10px] text-gray-400 outline-none"
                >
                  <option value="newest">Newest</option>
                  <option value="oldest">Oldest</option>
                  <option value="largest">Largest</option>
                </select>
              </div>
              <div className="relative">
                <input
                  ref={searchRef}
                  type="text"
                  value={search}
                  onChange={(e) => setSearch(e.target.value)}
                  placeholder="Search files..."
                  className="w-full rounded-md border border-surface-600 bg-surface-800 px-3 py-2 pl-8 font-mono text-xs text-gray-200 placeholder-gray-600 outline-none transition-colors focus:border-violet-500/50"
                />
                <svg
                  className="absolute left-2.5 top-2.5 h-3.5 w-3.5 text-gray-600"
                  fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2"
                >
                  <circle cx="11" cy="11" r="8" />
                  <path d="M21 21l-4.35-4.35" />
                </svg>
                {loading && (
                  <div className="absolute right-2.5 top-2.5 h-3.5 w-3.5 animate-spin rounded-full border-2 border-violet-500/30 border-t-violet-500" />
                )}
              </div>
            </div>

            {/* Filter tabs */}
            <div className="flex gap-1 border-b border-surface-700 px-4 py-2">
              {FILTER_TABS.map((tab) => (
                <button
                  key={tab.value}
                  onClick={() => setFilter(tab.value)}
                  className={`rounded-full px-2.5 py-1 font-mono text-[10px] transition-colors ${
                    filter === tab.value
                      ? 'bg-violet-500/20 text-violet-400'
                      : 'text-gray-500 hover:bg-surface-700 hover:text-gray-400'
                  }`}
                >
                  {tab.label}
                </button>
              ))}
            </div>

            {/* File list */}
            <div className="flex-1 overflow-y-auto px-4 py-2">
              {displayed.length === 0 && !loading && (
                <div className="flex flex-col items-center justify-center py-16 text-center">
                  <span className="text-3xl opacity-40">📁</span>
                  <p className="mt-3 font-mono text-xs text-gray-600">
                    {search
                      ? 'No files match your search'
                      : 'No files yet — ZBOT will save files here as it works'}
                  </p>
                </div>
              )}

              <AnimatePresence mode="popLayout">
                {displayed.map((f) => (
                  <motion.div
                    key={f.path}
                    layout
                    initial={{ opacity: 0, y: 10 }}
                    animate={{ opacity: 1, y: 0 }}
                    exit={{ opacity: 0, x: 50 }}
                    className="group mb-2 rounded-lg border border-surface-700 bg-surface-800/50 p-3 transition-colors hover:border-surface-600"
                  >
                    {/* File info */}
                    <div className="flex items-start gap-2">
                      <span className="mt-0.5 text-sm">{getIcon(f.extension)}</span>
                      <div className="min-w-0 flex-1">
                        <p className="truncate font-mono text-xs font-bold text-gray-200">
                          {f.name}
                        </p>
                        <div className="mt-1 flex items-center gap-2">
                          <span className="font-mono text-[10px] text-gray-500">
                            {f.size_human}
                          </span>
                          <span className="text-[10px] text-gray-700">•</span>
                          <span className="font-mono text-[10px] text-gray-500">
                            {formatAge(f.updated_at)}
                          </span>
                          {f.workflow_id && (
                            <>
                              <span className="text-[10px] text-gray-700">•</span>
                              <span className="rounded bg-surface-700 px-1 font-mono text-[10px] text-gray-500">
                                {f.workflow_id}
                              </span>
                            </>
                          )}
                        </div>
                      </div>
                    </div>

                    {/* Actions */}
                    <div className="mt-2 flex gap-1.5 opacity-0 transition-opacity group-hover:opacity-100">
                      <button
                        onClick={() => onPreview(f.path)}
                        className="rounded bg-violet-500/10 px-2 py-0.5 font-mono text-[10px] text-violet-400 transition-colors hover:bg-violet-500/20"
                      >
                        Preview
                      </button>
                      <button
                        onClick={() => handleDownload(f.path)}
                        className="rounded bg-blue-500/10 px-2 py-0.5 font-mono text-[10px] text-blue-400 transition-colors hover:bg-blue-500/20"
                      >
                        Download
                      </button>
                      <button
                        onClick={() => void handleDelete(f.path)}
                        className="rounded bg-red-500/10 px-2 py-0.5 font-mono text-[10px] text-red-400 transition-colors hover:bg-red-500/20"
                      >
                        Delete
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
