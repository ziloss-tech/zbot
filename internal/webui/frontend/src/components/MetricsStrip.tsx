import type { Metrics } from '../lib/types'

interface MetricsStripProps {
  metrics: Metrics
}

export function MetricsStrip({ metrics }: MetricsStripProps) {
  return (
    <div className="flex items-center gap-6 overflow-x-auto px-6 py-1.5">
      <Stat label="Active" value={`${metrics.active_workflows} workflows`} />
      <Stat label="Tasks" value={`${metrics.done_tasks}/${metrics.total_tasks} done`} />
      <Stat
        label="Tokens today"
        value={metrics.tokens_today.toLocaleString()}
      />
      <Stat label="Cost today" value={metrics.cost_today} />
    </div>
  )
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-center gap-1.5 whitespace-nowrap">
      <span className="font-mono text-[10px] text-gray-600">{label}:</span>
      <span className="font-mono text-[11px] text-gray-400">{value}</span>
    </div>
  )
}
