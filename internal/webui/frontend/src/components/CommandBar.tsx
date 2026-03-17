import { useState, useRef, useEffect } from 'react'
import { motion, AnimatePresence } from 'framer-motion'
import { createSchedule } from '../lib/api'

interface CommandBarProps {
  onSubmit: (goal: string) => void
  onChat: (message: string) => void
  onResearch?: () => void
  isPlanning: boolean
  isChatting: boolean
}

function parseScheduleInput(raw: string): { schedule: string; goal: string } | null {
  const patterns = [
    /^(every\s+(?:morning|evening|night|day|week|month|hour|minute|weekday|(?:monday|tuesday|wednesday|thursday|friday|saturday|sunday))[\w\s]*?(?:at\s+\d{1,2}(?::\d{2})?\s*(?:am|pm)?)?)\s+(.+)/i,
    /^(every\s+\d+\s+(?:minutes?|hours?|days?))\s+(.+)/i,
    /^(daily\s+at\s+\d{1,2}(?::\d{2})?\s*(?:am|pm)?)\s+(.+)/i,
  ]
  for (const p of patterns) {
    const m = raw.match(p)
    if (m) return { schedule: m[1].trim(), goal: m[2].trim() }
  }
  const v = raw.match(/^(every\s+[\w\s]+?)\s+(research|check|analyze|find|write|send|review|monitor|fetch)\s+(.+)/i)
  if (v) return { schedule: v[1].trim(), goal: `${v[2]} ${v[3]}`.trim() }
  return null
}

// Mode detection
function detectMode(val: string): 'plan' | 'research' | 'schedule' | 'chat' {
  const t = val.trim().toLowerCase()
  if (t.startsWith('plan:')) return 'plan'
  if (t.startsWith('research:')) return 'research'
  if (t.startsWith('schedule:')) return 'schedule'
  return 'chat'
}

const modeConfig = {
  plan:     { accent: 'text-anthropic',  border: 'border-anthropic/30',  glow: 'shadow-anthropic', label: 'plan',     icon: '▸', placeholder: 'Describe what you want Claude to build...' },
  research: { accent: 'text-gemini',    border: 'border-gemini/30',    glow: 'shadow-gemini',   label: 'research', icon: '◎', placeholder: 'Deep research topic — pulls sources, synthesizes, reports...' },
  schedule: { accent: 'text-observer',  border: 'border-observer/30',  glow: 'shadow-observer', label: 'schedule', icon: '◷', placeholder: 'every morning at 9am check AI news...' },
  chat:     { accent: 'text-white/40',  border: 'border-white/[0.08]', glow: '',                label: '',         icon: '',  placeholder: '⌘K to focus · Type a goal or use plan: · research: · schedule:' },
}

export function CommandBar({ onSubmit, onChat, onResearch, isPlanning, isChatting }: CommandBarProps) {
  const [value, setValue] = useState('')
  const inputRef = useRef<HTMLInputElement>(null)
  const [scheduleModal, setScheduleModal] = useState<{ schedule: string; goal: string } | null>(null)
  const [scheduleName, setScheduleName] = useState('')
  const [isScheduling, setIsScheduling] = useState(false)
  const [scheduleError, setScheduleError] = useState('')
  const [focused, setFocused] = useState(false)

  const mode = detectMode(value)
  const cfg = modeConfig[mode]
  const isBusy = isPlanning || isChatting || isScheduling

  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key === 'k') {
        e.preventDefault()
        inputRef.current?.focus()
      }
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [])

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    const trimmed = value.trim()
    if (!trimmed || isBusy) return

    if (mode === 'research') { onResearch?.(); setValue(''); return }

    if (mode === 'schedule') {
      const raw = trimmed.replace(/^schedule:\s*/i, '')
      const parsed = parseScheduleInput(raw)
      setScheduleModal(parsed ?? { schedule: '', goal: raw })
      setScheduleName('')
      setScheduleError('')
      return
    }

    if (mode === 'plan') {
      const goal = trimmed.replace(/^plan:\s*/i, '')
      if (goal) { onSubmit(goal); setValue('') }
    } else {
      // Default: treat as a plan (natural language goal)
      onSubmit(trimmed)
      setValue('')
    }
  }

  const handleScheduleConfirm = async () => {
    if (!scheduleModal) return
    setIsScheduling(true)
    setScheduleError('')
    try {
      await createSchedule(scheduleName || scheduleModal.goal.slice(0, 60), scheduleModal.goal, scheduleModal.schedule)
      setScheduleModal(null)
      setValue('')
    } catch (err) {
      setScheduleError(err instanceof Error ? err.message : 'Failed to create schedule')
    } finally {
      setIsScheduling(false)
    }
  }

  return (
    <>
      <form onSubmit={handleSubmit}>
        <div className={`relative flex items-center gap-3 rounded-xl border px-4 py-2.5 transition-all duration-200 bg-surface-800/80 backdrop-blur-sm ${
          focused ? `${cfg.border} ${cfg.glow}` : 'border-white/[0.06]'
        }`}>
          {/* Mode tag */}
          <AnimatePresence mode="wait">
            {mode !== 'chat' && (
              <motion.div
                key={mode}
                initial={{ opacity: 0, x: -8, width: 0 }}
                animate={{ opacity: 1, x: 0, width: 'auto' }}
                exit={{ opacity: 0, x: -8, width: 0 }}
                className={`flex items-center gap-1.5 shrink-0 overflow-hidden rounded-md border border-white/[0.06] bg-white/[0.04] px-2 py-1 font-mono text-[10px] font-semibold uppercase tracking-widest ${cfg.accent}`}
              >
                <span>{cfg.icon}</span>
                <span>{cfg.label}</span>
              </motion.div>
            )}
          </AnimatePresence>

          <input
            ref={inputRef}
            type="text"
            value={value}
            onChange={(e) => setValue(e.target.value)}
            onFocus={() => setFocused(true)}
            onBlur={() => setFocused(false)}
            placeholder={cfg.placeholder}
            disabled={isBusy}
            className="flex-1 min-w-0 bg-transparent font-mono text-sm text-white/80 placeholder-white/15 outline-none caret-white/50 disabled:opacity-40"
          />

          {/* Busy progress bar */}
          {isPlanning && (
            <motion.div
              className="absolute bottom-0 left-0 h-px rounded-full bg-gradient-to-r from-anthropic/40 via-anthropic to-anthropic/40"
              initial={{ width: '0%', opacity: 0 }}
              animate={{ width: '100%', opacity: 1 }}
              transition={{ duration: 9, ease: 'linear' }}
            />
          )}

          {/* Submit button — only show when there's input */}
          <AnimatePresence>
            {value.trim() && !isBusy && (
              <motion.button
                type="submit"
                initial={{ opacity: 0, scale: 0.8 }}
                animate={{ opacity: 1, scale: 1 }}
                exit={{ opacity: 0, scale: 0.8 }}
                className={`shrink-0 rounded-lg px-3 py-1.5 font-mono text-[11px] font-bold text-white transition-colors ${
                  mode === 'plan'     ? 'bg-anthropic/80 hover:bg-anthropic' :
                  mode === 'research' ? 'bg-gemini/60 hover:bg-gemini/80' :
                  mode === 'schedule' ? 'bg-observer/70 hover:bg-observer/90' :
                  'bg-white/[0.08] hover:bg-white/[0.12]'
                }`}
                whileTap={{ scale: 0.95 }}
              >
                ↵
              </motion.button>
            )}
            {isBusy && (
              <motion.div
                initial={{ opacity: 0 }}
                animate={{ opacity: 1 }}
                exit={{ opacity: 0 }}
                className="shrink-0 font-mono text-[10px] text-white/25"
              >
                working
              </motion.div>
            )}
          </AnimatePresence>
        </div>
      </form>

      {/* Schedule modal */}
      <AnimatePresence>
        {scheduleModal && (
          <motion.div
            initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }}
            className="fixed inset-0 z-50 flex items-center justify-center bg-black/70 backdrop-blur-sm"
            onClick={() => setScheduleModal(null)}
          >
            <motion.div
              initial={{ scale: 0.92, opacity: 0, y: 12 }}
              animate={{ scale: 1, opacity: 1, y: 0 }}
              exit={{ scale: 0.92, opacity: 0, y: 12 }}
              className="glass-panel mx-4 w-full max-w-md rounded-2xl p-6 shadow-2xl"
              onClick={(e) => e.stopPropagation()}
            >
              <h3 className="mb-5 flex items-center gap-2.5 font-display text-sm font-semibold text-white/80">
                <span className="flex h-7 w-7 items-center justify-center rounded-lg bg-observer/20 text-observer text-base">◷</span>
                Create Schedule
              </h3>

              <div className="space-y-3">
                <Field label="Name (optional)">
                  <input type="text" value={scheduleName} onChange={(e) => setScheduleName(e.target.value)}
                    placeholder="Morning AI Briefing"
                    className="w-full rounded-lg border border-white/[0.06] bg-white/[0.03] px-3 py-2 font-mono text-xs text-white/70 placeholder-white/20 outline-none focus:border-observer/40" />
                </Field>
                <Field label="Schedule">
                  <input type="text" value={scheduleModal.schedule} onChange={(e) => setScheduleModal({ ...scheduleModal, schedule: e.target.value })}
                    placeholder="every morning at 9am"
                    className="w-full rounded-lg border border-white/[0.06] bg-white/[0.03] px-3 py-2 font-mono text-xs text-observer/70 placeholder-white/20 outline-none focus:border-observer/40" />
                </Field>
                <Field label="Goal">
                  <textarea value={scheduleModal.goal} onChange={(e) => setScheduleModal({ ...scheduleModal, goal: e.target.value })}
                    rows={3}
                    className="w-full resize-none rounded-lg border border-white/[0.06] bg-white/[0.03] px-3 py-2 font-mono text-xs text-white/70 placeholder-white/20 outline-none focus:border-observer/40" />
                </Field>
              </div>

              {scheduleError && (
                <div className="mt-3 rounded-lg bg-red-500/10 border border-red-500/20 px-3 py-2 font-mono text-xs text-red-400">
                  {scheduleError}
                </div>
              )}

              <div className="mt-5 flex justify-end gap-2">
                <button onClick={() => setScheduleModal(null)}
                  className="rounded-lg px-4 py-2 font-mono text-xs text-white/30 hover:text-white/60 hover:bg-white/[0.04] transition-all">
                  Cancel
                </button>
                <button
                  onClick={() => void handleScheduleConfirm()}
                  disabled={!scheduleModal.schedule || !scheduleModal.goal || isScheduling}
                  className="rounded-lg bg-observer/80 hover:bg-observer px-4 py-2 font-mono text-xs font-bold text-white transition-all disabled:opacity-30">
                  {isScheduling ? 'Creating...' : 'Confirm'}
                </button>
              </div>
            </motion.div>
          </motion.div>
        )}
      </AnimatePresence>
    </>
  )
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <label className="mb-1.5 block font-mono text-[9px] uppercase tracking-widest text-white/25">{label}</label>
      {children}
    </div>
  )
}
