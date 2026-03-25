import { motion } from 'framer-motion'
import type { Metrics } from '../lib/types'
import type { CortexStatus } from '../hooks/useCortexStatus'

// ─── Props ──────────────────────────────────────────────────────────────────

interface MetricsStripProps {
  metrics: Metrics
  cortexStatus?: CortexStatus
  currentTool?: string | null
  elapsedMs?: number
  connected?: boolean
}

// ─── Status Indicator ───────────────────────────────────────────────────────

function StatusIndicator({ status, currentTool, elapsedMs }: {
  status: CortexStatus
  currentTool?: string | null
  elapsedMs?: number
}) {
  const elapsedStr = elapsedMs && elapsedMs > 0 ? `${(elapsedMs / 1000).toFixed(1)}s` : null

  switch (status) {
    case 'idle':
      return (
        <div className="flex items-center gap-2 px-3 py-2">
          <motion.span
            className="h-2 w-2 rounded-full bg-emerald-400"
            animate={{ opacity: [0.6, 1, 0.6] }}
            transition={{ duration: 3, repeat: Infinity }}
          />
          <span className="font-mono text-[10px] text-emerald-400/60">Ready</span>
        </div>
      )

    case 'thinking':
      return (
        <div className="flex items-center gap-2 px-3 py-2">
          <motion.span
            className="h-4 w-4 rounded-full bg-cyan-400/80"
            animate={{
              scale: [1, 1.2, 1],
              boxShadow: [
                '0 0 4px rgba(0,212,255,0.2)',
                '0 0 16px rgba(0,212,255,0.5)',
                '0 0 4px rgba(0,212,255,0.2)',
              ],
            }}
            transition={{ duration: 1.5, repeat: Infinity, ease: 'easeInOut' }}
          />
          <span className="font-mono text-[10px] text-cyan-400">Thinking...</span>
          {elapsedStr && (
            <span className="font-mono text-[9px] text-cyan-400/40 tabular-nums">{elapsedStr}</span>
          )}
        </div>
      )

    case 'tool_calling':
      return (
        <div className="flex items-center gap-2 px-3 py-2">
          <motion.span
            className="h-4 w-4 rounded-full bg-anthropic/80"
            animate={{ rotate: 360 }}
            transition={{ duration: 1.2, repeat: Infinity, ease: 'linear' }}
            style={{ borderTop: '2px solid rgba(217,119,87,0.8)', borderRadius: '50%', background: 'rgba(217,119,87,0.15)' }}
          />
          <span className="font-mono text-[10px] text-anthropic">
            {currentTool ? `${currentTool}...` : 'Calling tool...'}
          </span>
          {elapsedStr && (
            <span className="font-mono text-[9px] text-anthropic/40 tabular-nums">{elapsedStr}</span>
          )}
        </div>
      )

    case 'streaming':
      return (
        <div className="flex items-center gap-2 px-3 py-2">
          <div className="flex items-center gap-0.5">
            {[0, 1, 2].map(i => (
              <motion.span
                key={i}
                className="h-1.5 w-1.5 rounded-full bg-cyan-400/70"
                animate={{ opacity: [0.3, 1, 0.3] }}
                transition={{ duration: 0.8, delay: i * 0.15, repeat: Infinity }}
              />
            ))}
          </div>
          <span className="font-mono text-[10px] text-cyan-400/80">Writing response...</span>
          {elapsedStr && (
            <span className="font-mono text-[9px] text-cyan-400/40 tabular-nums">{elapsedStr}</span>
          )}
        </div>
      )

    case 'error':
      return (
        <div className="flex items-center gap-2 px-3 py-2">
          <span className="h-2 w-2 rounded-full bg-red-400" />
          <span className="font-mono text-[10px] text-red-400/80">Error</span>
        </div>
      )
  }
}

// ─── Connection Badge ───────────────────────────────────────────────────────

function ConnectionBadge({ connected }: { connected: boolean }) {
  if (connected) {
    return (
      <div className="flex items-center gap-1.5 px-2 py-1 rounded-md">
        <span className="h-1 w-1 rounded-full bg-emerald-400/60" />
        <span className="font-mono text-[8px] text-emerald-400/40 uppercase tracking-wider">Connected</span>
      </div>
    )
  }

  return (
    <motion.div
      className="flex items-center gap-1.5 px-2 py-1 rounded-md bg-red-500/10 border border-red-500/20"
      animate={{ opacity: [0.6, 1, 0.6] }}
      transition={{ duration: 1.5, repeat: Infinity }}
    >
      <span className="h-1.5 w-1.5 rounded-full bg-red-400" />
      <span className="font-mono text-[9px] text-red-400 uppercase tracking-wider">Disconnected</span>
    </motion.div>
  )
}

// ─── Main Component ─────────────────────────────────────────────────────────

export function MetricsStrip({
  metrics,
  cortexStatus = 'idle',
  currentTool,
  elapsedMs = 0,
  connected = true,
}: MetricsStripProps) {
  const taskPct = metrics.total_tasks > 0
    ? Math.round((metrics.done_tasks / metrics.total_tasks) * 100)
    : 0

  return (
    <div className="flex items-center gap-0 overflow-x-auto border-t border-white/[0.03]">
      {/* Status Indicator — most prominent element */}
      <StatusIndicator status={cortexStatus} currentTool={currentTool} elapsedMs={elapsedMs} />

      <Divider />

      <Stat label="workflows" value={String(metrics.active_workflows)} accent="text-white/50" />
      <Divider />
      <Stat label="tasks" value={`${metrics.done_tasks}/${metrics.total_tasks}`} accent="text-white/50">
        {metrics.total_tasks > 0 && (
          <span className="ml-1.5 font-mono text-[9px] text-emerald-400/60">{taskPct}%</span>
        )}
      </Stat>
      <Divider />
      <Stat label="tokens" value={metrics.tokens_today.toLocaleString()} accent="text-white/50" />
      <Divider />
      <div className="flex items-center gap-3 px-4 py-2">
        <span className="font-mono text-[9px] uppercase tracking-widest text-white/20">cost</span>
        <span className="font-mono text-[11px] text-white/50">{metrics.cost_today}</span>
      </div>

      {/* Connection badge — right-aligned */}
      <div className="ml-auto flex items-center gap-2 px-3">
        <ConnectionBadge connected={connected} />
      </div>
    </div>
  )
}

// ─── Helpers ────────────────────────────────────────────────────────────────

function Divider() {
  return <div className="h-3 w-px bg-white/[0.06] shrink-0" />
}

function Stat({
  label,
  value,
  accent,
  children,
}: {
  label: string
  value: string
  accent: string
  children?: React.ReactNode
}) {
  return (
    <div className="flex items-center gap-2 px-4 py-2 whitespace-nowrap">
      <span className="font-mono text-[9px] uppercase tracking-widest text-white/20">{label}</span>
      <span className={`font-mono text-[11px] tabular-nums ${accent}`}>{value}</span>
      {children}
    </div>
  )
}
