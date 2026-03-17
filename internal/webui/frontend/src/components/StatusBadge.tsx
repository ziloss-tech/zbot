import { motion } from 'framer-motion'

interface StatusBadgeProps {
  status: string
  size?: 'sm' | 'md'
}

const statusConfig: Record<string, { dot: string; text: string; label: string; pulse: boolean }> = {
  pending:  { dot: 'bg-white/20',    text: 'text-white/30',   label: 'pending',   pulse: false },
  planning: { dot: 'bg-anthropic',   text: 'text-anthropic/80', label: 'planning', pulse: true  },
  running:  { dot: 'bg-anthropic',   text: 'text-anthropic/80', label: 'running', pulse: true  },
  handoff:  { dot: 'bg-anthropic/60', text: 'text-anthropic/60', label: 'handoff', pulse: true  },
  done:     { dot: 'bg-emerald-400', text: 'text-emerald-400/70', label: 'done',   pulse: false },
  complete: { dot: 'bg-emerald-400', text: 'text-emerald-400/70', label: 'done',   pulse: false },
  failed:   { dot: 'bg-red-400',     text: 'text-red-400/80', label: 'failed',    pulse: false },
  canceled: { dot: 'bg-white/20',    text: 'text-white/25',   label: 'canceled',  pulse: false },
  error:    { dot: 'bg-red-400',     text: 'text-red-400/80', label: 'error',     pulse: false },
}

export function StatusBadge({ status, size = 'sm' }: StatusBadgeProps) {
  const cfg = statusConfig[status] ?? statusConfig['pending']!
  const px = size === 'sm' ? 'px-1.5 py-px' : 'px-2.5 py-1'
  const textSize = size === 'sm' ? 'text-[9px]' : 'text-[11px]'

  return (
    <span className={`inline-flex items-center gap-1.5 rounded-full border border-white/[0.06] bg-white/[0.04] ${px}`}>
      <motion.span
        className={`h-1 w-1 rounded-full shrink-0 ${cfg.dot}`}
        animate={cfg.pulse ? { opacity: [1, 0.2, 1] } : undefined}
        transition={cfg.pulse ? { duration: 1.4, repeat: Infinity } : undefined}
      />
      <span className={`font-mono font-medium uppercase tracking-widest ${cfg.text} ${textSize}`}>
        {cfg.label}
      </span>
    </span>
  )
}
