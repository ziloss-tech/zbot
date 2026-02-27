import { useEffect, useState, useCallback } from 'react'
import { AnimatePresence, motion } from 'framer-motion'

interface OutputPreviewProps {
  filePath: string | null
  onClose: () => void
}

export function OutputPreview({ filePath, onClose }: OutputPreviewProps) {
  const [content, setContent] = useState('')
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    if (!filePath) {
      setContent('')
      return
    }
    setLoading(true)
    fetch(`/api/file?path=${encodeURIComponent(filePath)}`)
      .then((r) => {
        if (!r.ok) throw new Error(`HTTP ${r.status.toString()}`)
        return r.text()
      })
      .then((text) => {
        setContent(text)
        setLoading(false)
      })
      .catch(() => {
        setContent('⚠ Could not load file preview')
        setLoading(false)
      })
  }, [filePath])

  // Close on Escape.
  const handleKeyDown = useCallback(
    (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    },
    [onClose],
  )

  useEffect(() => {
    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [handleKeyDown])

  return (
    <AnimatePresence>
      {filePath && (
        <>
          {/* Backdrop */}
          <motion.div
            className="fixed inset-0 z-40 bg-black/40"
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            onClick={onClose}
          />

          {/* Slide-in drawer */}
          <motion.div
            className="fixed bottom-0 right-0 top-0 z-50 flex w-[480px] max-w-[90vw] flex-col border-l border-surface-600 bg-surface-900"
            initial={{ x: '100%' }}
            animate={{ x: 0 }}
            exit={{ x: '100%' }}
            transition={{ type: 'spring', damping: 30, stiffness: 300 }}
          >
            {/* Header */}
            <div className="flex items-center justify-between border-b border-surface-600 px-4 py-3">
              <div className="flex items-center gap-2 overflow-hidden">
                <span className="text-sm">📄</span>
                <span className="truncate font-mono text-xs text-gray-300">
                  {filePath}
                </span>
              </div>
              <button
                onClick={onClose}
                className="rounded p-1 text-gray-500 transition-colors hover:bg-surface-700 hover:text-gray-300"
              >
                <span className="text-sm">✕</span>
              </button>
            </div>

            {/* Content */}
            <div className="flex-1 overflow-y-auto p-4">
              {loading ? (
                <div className="flex h-full items-center justify-center">
                  <span className="font-mono text-xs text-gray-500">Loading...</span>
                </div>
              ) : (
                <pre className="whitespace-pre-wrap font-mono text-xs leading-relaxed text-gray-300">
                  {content}
                </pre>
              )}
            </div>
          </motion.div>
        </>
      )}
    </AnimatePresence>
  )
}
