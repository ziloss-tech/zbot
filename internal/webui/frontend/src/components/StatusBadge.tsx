import { motion } from 'framer-motion'

interface StatusBadgeProps {
  status: string
  size?: 'sm' | 'md'
}

const statusConfig: Record<string, { bg: string; text: string; pulse: boolean }> = {
  pending: { bg: 'bg-surface-600', text: 'text-gray-400', pulse: false },
  running: { bg: 'bg-executor/20', text: 'text-executor', pulse: true },
  planning: { bg: 'bg-planner/20', text: 'text-planner', pulse: true },
  done: { bg: 'bg-green-500/20', text: 'text-green-400', pulse: false },
  failed: { bg: 'bg-red-500/20', text: 'text-red-400', pulse: false },
  canceled: { bg: 'bg-gray-500/20', text: 'text-gray-500', pulse: false },
  complete: { bg: 'bg-green-500/20', text: 'text-green-400', pulse: false },
  error: { bg: 'bg-red-500/20', text: 'text-red-400', pulse: false },
}

export function StatusBadge({ status, size = 'sm' }: StatusBadgeProps) {
  const config = statusConfig[status] ?? statusConfig['pending']!
  const sizeClass = size === 'sm' ? 'text-xs px-2 py-0.5' : 'text-sm px-3 py-1'

  return (
    <motion.span
      className={`inline-flex items-center gap-1.5 rounded-full font-mono font-medium ${config.bg} ${config.text} ${sizeClass}`}
      animate={config.pulse ? { opacity: [1, 0.6, 1] } : undefined}
      transition={config.pulse ? { duration: 2, repeat: Infinity } : undefined}
    >
      {config.pulse && (
        <span className="relative flex h-2 w-2">
          <span className={`absolute inline-flex h-full w-full animate-ping rounded-full ${config.bg} opacity-75`} />
          <span className={`relative inline-flex h-2 w-2 rounded-full ${status === 'running' ? 'bg-executor' : 'bg-planner'}`} />
        </span>
      )}
      {status}
    </motion.span>
  )
}
