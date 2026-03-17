import { motion, AnimatePresence } from 'framer-motion'
import { useState } from 'react'

export type NavPage = 'dashboard' | 'workflows' | 'research' | 'schedules' | 'memory' | 'knowledge'

interface SidebarProps {
  activePage: NavPage
  onNavigate: (page: NavPage) => void
  workflowActive: boolean
}

// Real SVG icons — minimal, clean
const Icons = {
  dashboard: (
    <svg viewBox="0 0 20 20" fill="currentColor" className="w-4 h-4">
      <rect x="2" y="2" width="7" height="7" rx="1.5"/>
      <rect x="11" y="2" width="7" height="7" rx="1.5"/>
      <rect x="2" y="11" width="7" height="7" rx="1.5"/>
      <rect x="11" y="11" width="7" height="7" rx="1.5"/>
    </svg>
  ),
  workflows: (
    <svg viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="1.5" className="w-4 h-4">
      <circle cx="4" cy="4" r="2"/>
      <circle cx="16" cy="10" r="2"/>
      <circle cx="4" cy="16" r="2"/>
      <path d="M6 4h4a2 2 0 012 2v8a2 2 0 01-2 2H6"/>
    </svg>
  ),
  research: (
    <svg viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="1.5" className="w-4 h-4">
      <circle cx="8.5" cy="8.5" r="5.5"/>
      <path d="M17 17l-3.5-3.5"/>
      <path d="M6 8.5h5M8.5 6v5" strokeLinecap="round"/>
    </svg>
  ),
  schedules: (
    <svg viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="1.5" className="w-4 h-4">
      <circle cx="10" cy="10" r="8"/>
      <path d="M10 5v5l3 3" strokeLinecap="round" strokeLinejoin="round"/>
    </svg>
  ),
  memory: (
    <svg viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="1.5" className="w-4 h-4">
      <path d="M10 2C6.13 2 3 5.13 3 9c0 2.38 1.19 4.47 3 5.74V17a1 1 0 001 1h6a1 1 0 001-1v-2.26C15.81 13.47 17 11.38 17 9c0-3.87-3.13-7-7-7z"/>
      <path d="M7 17h6" strokeLinecap="round"/>
    </svg>
  ),
  knowledge: (
    <svg viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="1.5" className="w-4 h-4">
      <path d="M4 4h8a2 2 0 012 2v10a2 2 0 01-2 2H4a2 2 0 01-2-2V6a2 2 0 012-2z"/>
      <path d="M14 6h2a2 2 0 012 2v8a2 2 0 01-2 2h-2"/>
      <path d="M6 8h6M6 11h6M6 14h4" strokeLinecap="round"/>
    </svg>
  ),
  audit: (
    <svg viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="1.5" className="w-4 h-4">
      <path d="M9 11l2 2 4-4"/>
      <path d="M5 3h10a2 2 0 012 2v12a2 2 0 01-2 2H5a2 2 0 01-2-2V5a2 2 0 012-2z"/>
    </svg>
  ),
}

const NAV = [
  { id: 'dashboard' as NavPage, label: 'Dashboard', icon: Icons.dashboard },
  { id: 'workflows' as NavPage, label: 'Workflows', icon: Icons.workflows, pulse: true },
  { id: 'research' as NavPage, label: 'Research', icon: Icons.research },
  { id: 'schedules' as NavPage, label: 'Schedules', icon: Icons.schedules },
  { id: 'memory' as NavPage, label: 'Memory', icon: Icons.memory },
  { id: 'knowledge' as NavPage, label: 'Knowledge', icon: Icons.knowledge },
]

export function Sidebar({ activePage, onNavigate, workflowActive }: SidebarProps) {
  const [expanded, setExpanded] = useState(false)

  return (
    <motion.div
      className="relative flex h-full flex-col border-r border-white/[0.04] bg-surface-900/80"
      animate={{ width: expanded ? 176 : 52 }}
      transition={{ type: 'spring', damping: 28, stiffness: 260 }}
      onMouseEnter={() => setExpanded(true)}
      onMouseLeave={() => setExpanded(false)}
    >
      {/* Logo */}
      <div className="flex h-14 items-center border-b border-white/[0.04] px-3.5">
        <div className="flex items-center gap-2.5">
          <div className="flex gap-1 shrink-0">
            <span className="h-1.5 w-1.5 rounded-full bg-anthropic" />
            <span className="h-1.5 w-1.5 rounded-full bg-anthropic/60" />
            <span className="h-1.5 w-1.5 rounded-full bg-anthropic/30" />
          </div>
          <AnimatePresence>
            {expanded && (
              <motion.span
                initial={{ opacity: 0, x: -6 }}
                animate={{ opacity: 1, x: 0 }}
                exit={{ opacity: 0, x: -6 }}
                transition={{ duration: 0.15 }}
                className="font-display text-sm font-bold tracking-tight text-white whitespace-nowrap"
              >
                ZBOT
              </motion.span>
            )}
          </AnimatePresence>
        </div>
      </div>

      {/* Nav */}
      <nav className="flex flex-1 flex-col gap-0.5 p-1.5 pt-2">
        {NAV.map((item) => {
          const isActive = activePage === item.id
          const showPulse = item.pulse && workflowActive
          return (
            <button
              key={item.id}
              onClick={() => onNavigate(item.id)}
              className={`relative flex items-center gap-3 rounded-lg px-2.5 py-2 transition-all duration-150 ${
                isActive
                  ? 'bg-white/[0.07] text-white'
                  : 'text-white/30 hover:bg-white/[0.04] hover:text-white/70'
              }`}
            >
              <span className="shrink-0">{item.icon}</span>
              <AnimatePresence>
                {expanded && (
                  <motion.span
                    initial={{ opacity: 0, x: -4 }}
                    animate={{ opacity: 1, x: 0 }}
                    exit={{ opacity: 0, x: -4 }}
                    transition={{ duration: 0.12 }}
                    className="whitespace-nowrap font-sans text-xs font-medium"
                  >
                    {item.label}
                  </motion.span>
                )}
              </AnimatePresence>
              {showPulse && (
                <motion.span
                  className="absolute right-2 top-2 h-1.5 w-1.5 rounded-full bg-anthropic shrink-0"
                  animate={{ opacity: [1, 0.2, 1] }}
                  transition={{ duration: 1.8, repeat: Infinity }}
                />
              )}
              {isActive && (
                <motion.div
                  layoutId="nav-pill"
                  className="absolute inset-0 rounded-lg bg-white/[0.06]"
                  style={{ zIndex: -1 }}
                />
              )}
            </button>
          )
        })}
      </nav>

      {/* Bottom */}
      <div className="border-t border-white/[0.04] p-1.5 pb-3">
        <a
          href="/audit"
          className="flex items-center gap-3 rounded-lg px-2.5 py-2 text-white/20 hover:text-white/50 transition-colors"
        >
          <span className="shrink-0">{Icons.audit}</span>
          <AnimatePresence>
            {expanded && (
              <motion.span
                initial={{ opacity: 0 }}
                animate={{ opacity: 1 }}
                exit={{ opacity: 0 }}
                className="whitespace-nowrap font-sans text-xs"
              >
                Audit Log
              </motion.span>
            )}
          </AnimatePresence>
        </a>
      </div>
    </motion.div>
  )
}
