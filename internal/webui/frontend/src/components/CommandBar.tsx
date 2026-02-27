import { useState, useRef, useEffect } from 'react'
import { motion } from 'framer-motion'

interface CommandBarProps {
  onSubmit: (goal: string) => void
  onChat: (message: string) => void
  isPlanning: boolean
  isChatting: boolean
}

export function CommandBar({ onSubmit, onChat, isPlanning, isChatting }: CommandBarProps) {
  const [value, setValue] = useState('')
  const inputRef = useRef<HTMLInputElement>(null)

  // Cmd+K focuses the command bar.
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

  const isPlanMessage = value.trim().toLowerCase().startsWith('plan:')
  const isBusy = isPlanning || isChatting

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    const trimmed = value.trim()
    if (!trimmed || isBusy) return

    if (isPlanMessage) {
      // Strip "plan:" prefix and submit as a plan.
      const goal = trimmed.replace(/^plan:\s*/i, '')
      if (goal) onSubmit(goal)
    } else {
      // Quick chat mode.
      onChat(trimmed)
    }
    setValue('')
  }

  return (
    <form onSubmit={handleSubmit} className="relative">
      <div className={`flex items-center gap-3 rounded-lg border bg-surface-800 px-4 py-3 transition-colors ${
        isPlanMessage
          ? 'border-surface-600 focus-within:border-planner/50'
          : 'border-surface-600 focus-within:border-amber-500/50'
      }`}>
        <span className={`font-mono text-sm ${isPlanMessage ? 'text-planner' : 'text-amber-400'}`}>
          {isPlanMessage ? '▶ plan:' : '💬'}
        </span>
        <input
          ref={inputRef}
          type="text"
          value={value}
          onChange={(e) => setValue(e.target.value)}
          placeholder="plan: your goal... or just type to chat"
          disabled={isBusy}
          className="flex-1 bg-transparent font-mono text-sm text-gray-100 placeholder-gray-600 outline-none caret-executor"
        />
        <motion.button
          type="submit"
          disabled={!value.trim() || isBusy}
          className={`rounded px-4 py-1.5 font-mono text-xs font-bold text-white transition-opacity disabled:opacity-30 ${
            isPlanMessage ? 'bg-planner' : 'bg-amber-600'
          }`}
          whileHover={{ scale: 1.05 }}
          whileTap={{ scale: 0.95 }}
        >
          {isBusy ? '...' : '⏎'}
        </motion.button>
      </div>
      {isPlanning && (
        <motion.div
          className="absolute -bottom-1 left-0 h-0.5 bg-gradient-to-r from-planner to-executor"
          initial={{ width: '0%' }}
          animate={{ width: '100%' }}
          transition={{ duration: 8, ease: 'linear' }}
        />
      )}
    </form>
  )
}
