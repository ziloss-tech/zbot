import { motion, AnimatePresence } from 'framer-motion'

interface HandoffAnimationProps {
  active: boolean
  onComplete: () => void
}

/**
 * HandoffAnimation — GPT-4o → Claude plan handoff moment.
 * A particle races from the planner (green) to the executor (orange),
 * leaving a glowing trail. Cinematic but fast.
 */
export function HandoffAnimation({ active, onComplete }: HandoffAnimationProps) {
  return (
    <AnimatePresence>
      {active && (
        <motion.div
          className="pointer-events-none absolute inset-0 z-50 flex items-center justify-center overflow-hidden"
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          exit={{ opacity: 0, transition: { duration: 0.5, delay: 0.2 } }}
        >
          {/* Background vignette */}
          <motion.div
            className="absolute inset-0 bg-black/20"
            initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }}
          />

          {/* The beam */}
          <motion.div
            className="relative h-px w-2/3"
            initial={{ scaleX: 0, originX: 0 }}
            animate={{ scaleX: 1 }}
            transition={{ duration: 0.6, ease: [0.4, 0, 0.2, 1] }}
            onAnimationComplete={onComplete}
          >
            {/* Gradient line: openai green → anthropic orange */}
            <div className="absolute inset-0 rounded-full bg-gradient-to-r from-openai via-white/60 to-anthropic" />

            {/* Glow layer */}
            <div
              className="absolute -inset-y-4 inset-x-0 rounded-full bg-gradient-to-r from-openai/30 via-white/15 to-anthropic/30"
              style={{ filter: 'blur(12px)' }}
            />
          </motion.div>

          {/* Leading particle */}
          <motion.div
            className="absolute h-2 w-2 rounded-full bg-white"
            style={{ filter: 'blur(3px)' }}
            initial={{ x: '-33vw', opacity: 0 }}
            animate={{ x: '33vw', opacity: [0, 1, 1, 0] }}
            transition={{ duration: 0.6, ease: [0.4, 0, 0.2, 1] }}
          />

          {/* Burst at center */}
          <motion.div
            className="absolute"
            initial={{ scale: 0, opacity: 0 }}
            animate={{ scale: [0, 2.5, 0], opacity: [0, 0.6, 0] }}
            transition={{ duration: 0.5, delay: 0.25 }}
          >
            <div className="h-6 w-6 rounded-full bg-white" style={{ filter: 'blur(8px)' }} />
          </motion.div>

          {/* Label */}
          <motion.div
            className="absolute top-1/2 mt-6"
            initial={{ opacity: 0, y: 4 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0 }}
            transition={{ delay: 0.2 }}
          >
            <div className="flex items-center gap-2 rounded-full border border-white/[0.08] bg-surface-800/90 px-3 py-1.5 backdrop-blur-sm">
              <span className="font-mono text-[9px] text-openai">GPT-4o</span>
              <span className="font-mono text-[9px] text-white/30">→</span>
              <span className="font-mono text-[9px] text-anthropic">Claude</span>
              <span className="ml-1 font-mono text-[9px] text-white/25 uppercase tracking-widest">handoff</span>
            </div>
          </motion.div>
        </motion.div>
      )}
    </AnimatePresence>
  )
}
