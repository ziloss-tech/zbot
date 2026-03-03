import type { Metrics } from '../lib/types'

interface MetricsStripProps {
  metrics: Metrics
}

export function MetricsStrip({ metrics }: MetricsStripProps) {
  const taskPct = metrics.total_tasks > 0
    ? Math.round((metrics.done_tasks / metrics.total_tasks) * 100)
    : 0

  return (
    <div className="flex items-center gap-0 overflow-x-auto border-t border-white/[0.03]">
      <Stat label="workflows" value={String(metrics.active_workflows)} accent="text-white/50" />
      <Divider />
      <Stat label="tasks" value={`${metrics.done_tasks}/${metrics.total_tasks}`} accent="text-white/50">
        {metrics.total_tasks > 0 && (
          <span className="ml-1.5 font-mono text-[9px] text-openai/60">{taskPct}%</span>
        )}
      </Stat>
      <Divider />
      <Stat label="tokens" value={metrics.tokens_today.toLocaleString()} accent="text-white/50" />
      <Divider />
      {/* Cost split by model where possible */}
      <div className="flex items-center gap-3 px-4 py-2">
        <span className="font-mono text-[9px] uppercase tracking-widest text-white/20">cost</span>
        <span className="font-mono text-[11px] text-white/50">{metrics.cost_today}</span>
      </div>
    </div>
  )
}

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
