import { motion } from 'framer-motion'
import type { CriticIssue } from '../lib/types'

interface CriticBadgeProps {
  status: 'reviewing' | 'passed' | 'failed' | 'partial' | 'retrying'
  issues?: CriticIssue[]
}

const badgeConfig: Record<string, { label: string; color: string; icon: string }> = {
  reviewing: { label: 'GPT-4o reviewing...', color: 'bg-planner/20 text-planner', icon: '🔍' },
  passed: { label: 'Passed', color: 'bg-green-500/20 text-green-400', icon: '✓' },
  failed: { label: 'Retry', color: 'bg-red-500/20 text-red-400', icon: '✗' },
  partial: { label: 'Partial', color: 'bg-yellow-500/20 text-yellow-400', icon: '⚠' },
  retrying: { label: 'Retrying', color: 'bg-executor/20 text-executor', icon: '⟳' },
}

export function CriticBadge({ status, issues }: CriticBadgeProps) {
  const config = badgeConfig[status]
  if (!config) return null

  const issueCount = issues?.length ?? 0

  return (
    <motion.div
      initial={{ opacity: 0, scale: 0.8 }}
      animate={{ opacity: 1, scale: 1 }}
      exit={{ opacity: 0, scale: 0.8 }}
      className={`flex items-center gap-1 rounded-full px-2 py-0.5 font-mono text-[10px] ${config.color}`}
    >
      {status === 'reviewing' ? (
        <motion.span
          animate={{ rotate: 360 }}
          transition={{ duration: 2, repeat: Infinity, ease: 'linear' }}
        >
          {config.icon}
        </motion.span>
      ) : (
        <span>{config.icon}</span>
      )}
      <span>{config.label}</span>
      {issueCount > 0 && status !== 'reviewing' && status !== 'retrying' && (
        <span className="rounded bg-black/20 px-1">{issueCount.toString()}</span>
      )}
    </motion.div>
  )
}
