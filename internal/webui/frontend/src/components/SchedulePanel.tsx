import { useState, useEffect, useCallback, useRef } from 'react'
import { AnimatePresence, motion } from 'framer-motion'
import {
  fetchSchedules,
  createSchedule,
  pauseSchedule,
  resumeSchedule,
  deleteSchedule,
  runScheduleNow,
} from '../lib/api'
import type { ScheduledJob } from '../lib/types'

interface SchedulePanelProps {
  open: boolean
  onClose: () => void
}

const STATUS_DOT: Record<string, string> = {
  active: 'bg-green-400',
  running: 'bg-yellow-400 animate-pulse',
  paused: 'bg-gray-500',
}

const STATUS_ICON: Record<string, string> = {
  active: '●',
  running: '●',
  paused: '⏸',
}

export function SchedulePanel({ open, onClose }: SchedulePanelProps) {
  const [jobs, setJobs] = useState<ScheduledJob[]>([])
  const [loading, setLoading] = useState(false)
  const [showNew, setShowNew] = useState(false)
  const [newName, setNewName] = useState('')
  const [newGoal, setNewGoal] = useState('')
  const [newSchedule, setNewSchedule] = useState('')
  const [creating, setCreating] = useState(false)
  const [error, setError] = useState('')
  const pollRef = useRef<ReturnType<typeof setInterval>>()

  const loadJobs = useCallback(async () => {
    setLoading(true)
    try {
      const data = await fetchSchedules()
      setJobs(data)
    } catch {
      // silently fail
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    if (open) {
      loadJobs()
    }
  }, [open, loadJobs])

  // Auto-refresh every 30 seconds.
  useEffect(() => {
    if (open) {
      pollRef.current = setInterval(() => void loadJobs(), 30000)
    }
    return () => {
      if (pollRef.current) clearInterval(pollRef.current)
    }
  }, [open, loadJobs])

  const handleCreate = async () => {
    if (!newGoal || !newSchedule) return
    setCreating(true)
    setError('')
    try {
      await createSchedule(newName || newGoal.slice(0, 60), newGoal, newSchedule)
      setShowNew(false)
      setNewName('')
      setNewGoal('')
      setNewSchedule('')
      await loadJobs()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create')
    } finally {
      setCreating(false)
    }
  }

  const handlePause = async (id: string) => {
    try {
      await pauseSchedule(id)
      await loadJobs()
    } catch { /* ignore */ }
  }

  const handleResume = async (id: string) => {
    try {
      await resumeSchedule(id)
      await loadJobs()
    } catch { /* ignore */ }
  }

  const handleDelete = async (id: string) => {
    try {
      await deleteSchedule(id)
      setJobs((prev) => prev.filter((j) => j.id !== id))
    } catch { /* ignore */ }
  }

  const handleRunNow = async (id: string) => {
    try {
      await runScheduleNow(id)
      // Refresh to show running status.
      setTimeout(() => void loadJobs(), 1000)
    } catch { /* ignore */ }
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

  const formatNextRun = (dateStr: string) => {
    const date = new Date(dateStr)
    const now = new Date()
    const diffMs = date.getTime() - now.getTime()
    if (diffMs < 0) return 'overdue'
    const diffMin = diffMs / 60000
    if (diffMin < 60) return `in ${Math.round(diffMin)} min`
    const diffHours = diffMin / 60
    if (diffHours < 24) return `in ${Math.round(diffHours)}h`
    return date.toLocaleString(undefined, { weekday: 'short', month: 'short', day: 'numeric', hour: 'numeric', minute: '2-digit' })
  }

  const activeCount = jobs.filter((j) => j.status === 'active' || j.status === 'running').length
  const pausedCount = jobs.filter((j) => j.status === 'paused').length

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
                <span className="text-lg">⏰</span>
                <h2 className="font-display text-sm font-bold tracking-tight text-gray-100">
                  Scheduled Tasks
                </h2>
              </div>
              <div className="flex items-center gap-2">
                <button
                  onClick={() => setShowNew(!showNew)}
                  className="rounded bg-violet-500/20 px-2 py-1 font-mono text-xs text-violet-400 transition-colors hover:bg-violet-500/30"
                >
                  + New
                </button>
                <button
                  onClick={onClose}
                  className="rounded p-1 text-gray-500 transition-colors hover:bg-surface-700 hover:text-gray-300"
                >
                  <svg width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="2">
                    <path d="M4 4l8 8M12 4l-8 8" />
                  </svg>
                </button>
              </div>
            </div>

            {/* Summary bar */}
            <div className="border-b border-surface-700 px-4 py-2">
              <span className="font-mono text-[10px] text-gray-500">
                {activeCount} active{pausedCount > 0 ? ` • ${pausedCount} paused` : ''}
              </span>
              {loading && (
                <span className="ml-2 inline-block h-3 w-3 animate-spin rounded-full border-2 border-violet-500/30 border-t-violet-500" />
              )}
            </div>

            {/* New job form */}
            <AnimatePresence>
              {showNew && (
                <motion.div
                  initial={{ height: 0, opacity: 0 }}
                  animate={{ height: 'auto', opacity: 1 }}
                  exit={{ height: 0, opacity: 0 }}
                  className="overflow-hidden border-b border-surface-700"
                >
                  <div className="space-y-2 px-4 py-3">
                    <input
                      type="text"
                      value={newName}
                      onChange={(e) => setNewName(e.target.value)}
                      placeholder="Name (optional)"
                      className="w-full rounded border border-surface-600 bg-surface-800 px-3 py-2 font-mono text-xs text-gray-200 placeholder-gray-600 outline-none focus:border-violet-500/50"
                    />
                    <textarea
                      value={newGoal}
                      onChange={(e) => setNewGoal(e.target.value)}
                      placeholder="Goal: what should ZBOT do?"
                      rows={2}
                      className="w-full resize-none rounded border border-surface-600 bg-surface-800 px-3 py-2 font-mono text-xs text-gray-200 placeholder-gray-600 outline-none focus:border-violet-500/50"
                    />
                    <input
                      type="text"
                      value={newSchedule}
                      onChange={(e) => setNewSchedule(e.target.value)}
                      placeholder="Schedule: every morning at 8am, every Monday at 9am..."
                      className="w-full rounded border border-surface-600 bg-surface-800 px-3 py-2 font-mono text-xs text-violet-400 placeholder-gray-600 outline-none focus:border-violet-500/50"
                    />
                    {error && (
                      <div className="rounded bg-red-500/10 px-2 py-1 font-mono text-[10px] text-red-400">
                        {error}
                      </div>
                    )}
                    <button
                      onClick={() => void handleCreate()}
                      disabled={!newGoal || !newSchedule || creating}
                      className="w-full rounded bg-violet-600 py-2 font-mono text-xs font-bold text-white transition-opacity disabled:opacity-40 hover:bg-violet-500"
                    >
                      {creating ? 'Creating...' : 'Create Schedule'}
                    </button>
                  </div>
                </motion.div>
              )}
            </AnimatePresence>

            {/* Job list */}
            <div className="flex-1 overflow-y-auto px-4 py-2">
              {jobs.length === 0 && !loading && (
                <div className="flex flex-col items-center justify-center py-16 text-center">
                  <span className="text-3xl opacity-40">⏰</span>
                  <p className="mt-3 font-mono text-xs text-gray-600">
                    No scheduled tasks yet
                  </p>
                  <p className="mt-1 font-mono text-[10px] text-gray-700">
                    Type &quot;schedule: every day at 9am ...&quot; in the command bar
                  </p>
                </div>
              )}

              <AnimatePresence mode="popLayout">
                {jobs.map((job) => (
                  <motion.div
                    key={job.id}
                    layout
                    initial={{ opacity: 0, y: 10 }}
                    animate={{ opacity: 1, y: 0 }}
                    exit={{ opacity: 0, x: 50 }}
                    className="group mb-3 rounded-lg border border-surface-700 bg-surface-800/50 p-3 transition-colors hover:border-surface-600"
                  >
                    {/* Job header */}
                    <div className="flex items-start gap-2">
                      <span className={`mt-1.5 h-2 w-2 rounded-full ${STATUS_DOT[job.status] || 'bg-gray-500'}`} />
                      <div className="min-w-0 flex-1">
                        <div className="flex items-center gap-2">
                          <p className="truncate font-mono text-xs font-bold text-gray-200">
                            {STATUS_ICON[job.status] === '⏸' ? '⏸ ' : ''}{job.name}
                          </p>
                        </div>
                        <p className="mt-0.5 truncate font-mono text-[10px] text-gray-500">
                          &quot;{job.goal.length > 80 ? job.goal.slice(0, 80) + '...' : job.goal}&quot;
                        </p>
                        <p className="mt-1 font-mono text-[10px] text-violet-400">
                          {job.natural_schedule || job.cron_expr}
                        </p>

                        {/* Timing info */}
                        <div className="mt-1.5 flex flex-wrap items-center gap-2">
                          {job.status !== 'paused' && (
                            <span className="font-mono text-[10px] text-gray-500">
                              Next: {formatNextRun(job.next_run)}
                            </span>
                          )}
                          {job.last_run ? (
                            <>
                              <span className="text-[10px] text-gray-700">•</span>
                              <span className="font-mono text-[10px] text-gray-500">
                                Last: {formatAge(job.last_run)} {job.status !== 'running' ? '✅' : '⏳'}
                              </span>
                            </>
                          ) : (
                            <>
                              <span className="text-[10px] text-gray-700">•</span>
                              <span className="font-mono text-[10px] text-gray-600">Never run yet</span>
                            </>
                          )}
                          {job.run_count > 0 && (
                            <>
                              <span className="text-[10px] text-gray-700">•</span>
                              <span className="font-mono text-[10px] text-gray-500">
                                {job.run_count} runs
                              </span>
                            </>
                          )}
                        </div>
                      </div>
                    </div>

                    {/* Actions */}
                    <div className="mt-2 flex gap-1.5 opacity-0 transition-opacity group-hover:opacity-100">
                      {job.status !== 'running' && (
                        <button
                          onClick={() => void handleRunNow(job.id)}
                          className="rounded bg-green-500/10 px-2 py-0.5 font-mono text-[10px] text-green-400 transition-colors hover:bg-green-500/20"
                        >
                          Run Now
                        </button>
                      )}
                      {job.status === 'active' && (
                        <button
                          onClick={() => void handlePause(job.id)}
                          className="rounded bg-yellow-500/10 px-2 py-0.5 font-mono text-[10px] text-yellow-400 transition-colors hover:bg-yellow-500/20"
                        >
                          Pause
                        </button>
                      )}
                      {job.status === 'paused' && (
                        <button
                          onClick={() => void handleResume(job.id)}
                          className="rounded bg-blue-500/10 px-2 py-0.5 font-mono text-[10px] text-blue-400 transition-colors hover:bg-blue-500/20"
                        >
                          Resume
                        </button>
                      )}
                      <button
                        onClick={() => void handleDelete(job.id)}
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
