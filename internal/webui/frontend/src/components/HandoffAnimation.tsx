import { motion, AnimatePresence } from 'framer-motion'

interface HandoffAnimationProps {
  active: boolean
  onComplete: () => void
}

/**
 * HandoffAnimation: a glowing line traces from left panel to right panel
 * when GPT-4o finishes planning and hands off to Claude.
 */
export function HandoffAnimation({ active, onComplete }: HandoffAnimationProps) {
  return (
    <AnimatePresence>
      {active && (
        <motion.div
          className="pointer-events-none absolute inset-0 z-50 flex items-center justify-center"
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          exit={{ opacity: 0 }}
          transition={{ duration: 0.3 }}
        >
          {/* Glow line */}
          <motion.div
            className="h-0.5 rounded-full bg-gradient-to-r from-planner via-white to-executor"
            style={{ filter: 'blur(1px)' }}
            initial={{ width: '0%', opacity: 0 }}
            animate={{ width: '60%', opacity: [0, 1, 1, 0.8] }}
            transition={{ duration: 0.8, ease: 'easeInOut' }}
            onAnimationComplete={onComplete}
          />

          {/* Glow effect behind the line */}
          <motion.div
            className="absolute h-8 rounded-full bg-gradient-to-r from-planner/20 via-white/10 to-executor/20"
            style={{ filter: 'blur(20px)' }}
            initial={{ width: '0%', opacity: 0 }}
            animate={{ width: '60%', opacity: [0, 0.6, 0] }}
            transition={{ duration: 0.8, ease: 'easeInOut' }}
          />

          {/* Center flash */}
          <motion.div
            className="absolute h-2 w-2 rounded-full bg-white"
            style={{ filter: 'blur(4px)' }}
            initial={{ scale: 0, opacity: 0 }}
            animate={{ scale: [0, 3, 0], opacity: [0, 1, 0] }}
            transition={{ duration: 0.6, delay: 0.3 }}
          />
        </motion.div>
      )}
    </AnimatePresence>
  )
}
