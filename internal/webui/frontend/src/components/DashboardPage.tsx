import { useEffect, useState } from 'react'
import { motion } from 'framer-motion'
import { fetchWorkflows, fetchMetrics, fetchResearchSessions, fetchSchedules } from '../lib/api'
import type { WorkflowListItem, Metrics, ResearchSession, ScheduledJob } from '../lib/types'

interface DashboardPageProps {
  onNavigate: (page: string) => void
}

// Tiny sparkline component — shows a flat or upward trend bar
function TrendBar({ pct, color }: { pct: number; color: string }) {
  return (
    <div className="h-0.5 w-16 rounded-full bg-white/[0.06] overflow-hidden">
      <motion.div
        className={`h-full rounded-full ${color}`}
        initial={{ width: 0 }}
        animate={{ width: `${pct}%` }}
        transition={{ duration: 0.8, ease: 'easeOut', delay: 0.3 }}
      />
    </div>
  )
}

export function DashboardPage({ onNavigate }: DashboardPageProps) {
  const [metrics, setMetrics] = useState<Metrics | null>(null)
  const [recentWorkflows, setRecentWorkflows] = useState<WorkflowListItem[]>([])
  const [recentResearch, setRecentResearch] = useState<ResearchSession[]>([])
  const [schedules, setSchedules] = useState<ScheduledJob[]>([])

  useEffect(() => {
    void fetchMetrics().then(setMetrics).catch(() => null)
    void fetchWorkflows().then((w) => setRecentWorkflows(w.slice(0, 6))).catch(() => null)
    void fetchResearchSessions(6).then(setRecentResearch).catch(() => null)
    void fetchSchedules().then(setSchedules).catch(() => null)
  }, [])

  const activeSchedules = schedules.filter((s) => s.status === 'active').length
  const taskPct = metrics && metrics.total_tasks > 0
    ? Math.round((metrics.done_tasks / metrics.total_tasks) * 100)
    : 0

  const stats = [
    {
      label: 'Active Workflows',
      value: metrics?.active_workflows ?? '—',
      sub: 'right now',
      color: 'text-anthropic',
      dot: 'bg-anthropic',
      bar: { pct: 60, color: 'bg-anthropic' },
    },
    {
      label: 'Tasks Today',
      value: metrics ? `${metrics.done_tasks}/${metrics.total_tasks}` : '—',
      sub: `${taskPct}% complete`,
      color: 'text-openai',
      dot: 'bg-openai',
      bar: { pct: taskPct, color: 'bg-openai' },
    },
    {
      label: 'Cost Today',
      value: metrics?.cost_today ?? '—',
      sub: 'across all models',
      color: 'text-white/60',
      dot: 'bg-white/20',
      bar: { pct: 30, color: 'bg-white/20' },
    },
    {
      label: 'Active Schedules',
      value: activeSchedules || '—',
      sub: `${schedules.length} total`,
      color: 'text-auditor',
      dot: 'bg-auditor',
      bar: { pct: activeSchedules > 0 ? Math.min((activeSchedules / Math.max(schedules.length, 1)) * 100, 100) : 0, color: 'bg-auditor' },
    },
  ]

  return (
    <div className="flex h-full flex-col overflow-y-auto p-5">

      {/* Header */}
      <div className="mb-5">
        <h2 className="font-display text-base font-semibold text-white/80">Overview</h2>
        <p className="font-mono text-[10px] text-white/20 uppercase tracking-widest mt-0.5">ZBOT · {new Date().toLocaleDateString('en-US', { weekday: 'long', month: 'short', day: 'numeric' })}</p>
      </div>

      {/* Stats row */}
      <div className="mb-5 grid grid-cols-4 gap-2.5">
        {stats.map((s, i) => (
          <motion.div
            key={s.label}
            initial={{ opacity: 0, y: 10 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ delay: i * 0.07 }}
            className="glass-panel rounded-xl p-4"
          >
            <div className="flex items-start justify-between mb-3">
              <motion.div className={`h-1.5 w-1.5 rounded-full mt-1 ${s.dot}`}
                animate={{ opacity: [0.5, 1, 0.5] }} transition={{ duration: 2, repeat: Infinity, delay: i * 0.3 }} />
              <TrendBar pct={s.bar.pct} color={s.bar.color} />
            </div>
            <div className={`font-display text-2xl font-bold tabular-nums ${s.color}`}>{String(s.value)}</div>
            <div className="mt-1 font-mono text-[9px] text-white/25 uppercase tracking-widest">{s.label}</div>
            <div className="mt-0.5 font-mono text-[9px] text-white/20">{s.sub}</div>
          </motion.div>
        ))}
      </div>

      {/* Content grid */}
      <div className="grid flex-1 grid-cols-2 gap-3 min-h-0">

        {/* Recent Workflows */}
        <div className="glass-panel flex flex-col rounded-xl overflow-hidden">
          <div className="flex items-center justify-between border-b border-white/[0.04] px-4 py-3">
            <div className="flex items-center gap-2">
              <div className="h-1.5 w-1.5 rounded-full bg-anthropic" />
              <span className="font-mono text-[11px] font-semibold text-white/60 uppercase tracking-wider">Workflows</span>
            </div>
            <button onClick={() => onNavigate('workflows')}
              className="font-mono text-[9px] text-white/20 hover:text-white/50 transition-colors">
              view all →
            </button>
          </div>
          <div className="flex-1 overflow-y-auto divide-y divide-white/[0.03]">
            {recentWorkflows.length === 0 ? (
              <p className="px-4 py-8 text-center font-mono text-xs text-white/15">No workflows yet</p>
            ) : recentWorkflows.map((w, i) => (
              <motion.div key={w.id} initial={{ opacity: 0 }} animate={{ opacity: 1 }} transition={{ delay: i * 0.05 }}
                className="px-4 py-3 hover:bg-white/[0.02] transition-colors cursor-default">
                <div className="flex items-center justify-between mb-1.5">
                  <WorkflowStatusChip status={w.status} />
                  <span className="font-mono text-[9px] text-white/20 tabular-nums">
                    {w.done_count}/{w.task_count}
                  </span>
                </div>
                <p className="font-sans text-[11px] text-white/60 line-clamp-1 leading-snug">{w.goal}</p>
                <p className="mt-0.5 font-mono text-[9px] text-white/20">
                  {new Date(w.created_at).toLocaleString('en-US', { month: 'short', day: 'numeric', hour: 'numeric', minute: '2-digit' })}
                </p>
              </motion.div>
            ))}
          </div>
        </div>

        {/* Recent Research */}
        <div className="glass-panel flex flex-col rounded-xl overflow-hidden">
          <div className="flex items-center justify-between border-b border-white/[0.04] px-4 py-3">
            <div className="flex items-center gap-2">
              <div className="h-1.5 w-1.5 rounded-full bg-gemini" />
              <span className="font-mono text-[11px] font-semibold text-white/60 uppercase tracking-wider">Research</span>
            </div>
            <button onClick={() => onNavigate('research')}
              className="font-mono text-[9px] text-white/20 hover:text-white/50 transition-colors">
              view all →
            </button>
          </div>
          <div className="flex-1 overflow-y-auto divide-y divide-white/[0.03]">
            {recentResearch.length === 0 ? (
              <p className="px-4 py-8 text-center font-mono text-xs text-white/15">No research yet</p>
            ) : recentResearch.map((r, i) => (
              <motion.div key={r.id} initial={{ opacity: 0 }} animate={{ opacity: 1 }} transition={{ delay: i * 0.05 }}
                className="px-4 py-3 hover:bg-white/[0.02] transition-colors cursor-default">
                <div className="flex items-center justify-between mb-1.5">
                  <ResearchStatusChip status={r.status} />
                  <div className="flex items-center gap-2">
                    <span className="font-mono text-[9px] text-openai/60">{Math.round(r.confidence_score * 100)}%</span>
                    <span className="font-mono text-[9px] text-white/20">${r.cost_usd.toFixed(3)}</span>
                  </div>
                </div>
                <p className="font-sans text-[11px] text-white/60 line-clamp-1 leading-snug">{r.goal}</p>
                <p className="mt-0.5 font-mono text-[9px] text-white/20">
                  {r.iterations} iter · {new Date(r.created_at).toLocaleString('en-US', { month: 'short', day: 'numeric', hour: 'numeric', minute: '2-digit' })}
                </p>
              </motion.div>
            ))}
          </div>
        </div>

      </div>
    </div>
  )
}

function WorkflowStatusChip({ status }: { status: string }) {
  const map: Record<string, string> = {
    complete: 'text-openai/70 bg-openai/10 border-openai/20',
    failed:   'text-red-400/70 bg-red-400/10 border-red-400/20',
    running:  'text-anthropic/70 bg-anthropic/10 border-anthropic/20',
    planning: 'text-openai/70 bg-openai/10 border-openai/20',
  }
  return (
    <span className={`inline-block rounded-md border px-1.5 py-px font-mono text-[9px] uppercase tracking-widest ${map[status] ?? 'text-white/20 bg-white/[0.04] border-white/[0.06]'}`}>
      {status}
    </span>
  )
}

function ResearchStatusChip({ status }: { status: string }) {
  const map: Record<string, string> = {
    complete: 'text-gemini/70 bg-gemini/10 border-gemini/20',
    failed:   'text-red-400/70 bg-red-400/10 border-red-400/20',
    running:  'text-gemini/70 bg-gemini/10 border-gemini/20',
  }
  return (
    <span className={`inline-block rounded-md border px-1.5 py-px font-mono text-[9px] uppercase tracking-widest ${map[status] ?? 'text-white/20 bg-white/[0.04] border-white/[0.06]'}`}>
      {status}
    </span>
  )
}
