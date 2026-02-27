import { useState, useEffect, useMemo } from 'react'
import { AnimatePresence, motion } from 'framer-motion'

interface FilePreviewDrawerProps {
  filePath: string | null
  onClose: () => void
}

type FileType = 'markdown' | 'csv' | 'json' | 'code' | 'text' | 'binary'

const CODE_EXTS = new Set(['py', 'js', 'ts', 'go', 'sh', 'bash', 'rb', 'rs', 'java', 'c', 'cpp', 'h', 'jsx', 'tsx', 'css', 'html', 'yaml', 'yml', 'toml'])
const BINARY_EXTS = new Set(['png', 'jpg', 'jpeg', 'gif', 'pdf', 'zip', 'tar', 'gz', 'exe', 'bin', 'so', 'dll'])

function getFileType(path: string): FileType {
  const ext = path.split('.').pop()?.toLowerCase() || ''
  if (ext === 'md') return 'markdown'
  if (ext === 'csv' || ext === 'tsv') return 'csv'
  if (ext === 'json') return 'json'
  if (CODE_EXTS.has(ext)) return 'code'
  if (BINARY_EXTS.has(ext)) return 'binary'
  return 'text'
}

function getLanguageLabel(path: string): string {
  const ext = path.split('.').pop()?.toLowerCase() || ''
  const labels: Record<string, string> = {
    py: 'Python', js: 'JavaScript', ts: 'TypeScript', go: 'Go',
    sh: 'Shell', bash: 'Bash', rb: 'Ruby', rs: 'Rust', java: 'Java',
    c: 'C', cpp: 'C++', jsx: 'React JSX', tsx: 'React TSX',
    css: 'CSS', html: 'HTML', yaml: 'YAML', yml: 'YAML', toml: 'TOML',
  }
  return labels[ext] || ext.toUpperCase()
}

// Simple markdown renderer.
function renderMarkdown(text: string): string {
  let html = text
    // Headers
    .replace(/^### (.+)$/gm, '<h3 class="text-base font-bold text-gray-100 mt-4 mb-2">$1</h3>')
    .replace(/^## (.+)$/gm, '<h2 class="text-lg font-bold text-gray-100 mt-5 mb-2">$1</h2>')
    .replace(/^# (.+)$/gm, '<h1 class="text-xl font-bold text-gray-100 mt-6 mb-3">$1</h1>')
    // Bold + Italic
    .replace(/\*\*\*(.+?)\*\*\*/g, '<strong><em>$1</em></strong>')
    .replace(/\*\*(.+?)\*\*/g, '<strong class="text-gray-100">$1</strong>')
    .replace(/\*(.+?)\*/g, '<em>$1</em>')
    // Code blocks
    .replace(/```(\w+)?\n([\s\S]*?)```/g, '<pre class="rounded-lg bg-surface-900 border border-surface-600 p-3 my-3 overflow-x-auto"><code class="font-mono text-xs text-green-400">$2</code></pre>')
    // Inline code
    .replace(/`([^`]+)`/g, '<code class="rounded bg-surface-700 px-1.5 py-0.5 font-mono text-xs text-violet-400">$1</code>')
    // Unordered lists
    .replace(/^- (.+)$/gm, '<li class="ml-4 list-disc text-gray-300">$1</li>')
    // Ordered lists
    .replace(/^\d+\. (.+)$/gm, '<li class="ml-4 list-decimal text-gray-300">$1</li>')
    // Links
    .replace(/\[([^\]]+)\]\(([^)]+)\)/g, '<a href="$2" class="text-violet-400 underline" target="_blank" rel="noopener">$1</a>')
    // Horizontal rules
    .replace(/^---$/gm, '<hr class="border-surface-600 my-4" />')
    // Paragraphs (blank line separated)
    .replace(/\n\n/g, '</p><p class="text-gray-300 leading-relaxed mb-3">')

  return `<p class="text-gray-300 leading-relaxed mb-3">${html}</p>`
}

// Parse CSV into 2D array.
function parseCSV(text: string): { headers: string[]; rows: string[][]; rowCount: number; colCount: number } {
  const lines = text.trim().split('\n')
  if (lines.length === 0) return { headers: [], rows: [], rowCount: 0, colCount: 0 }

  const parseLine = (line: string): string[] => {
    const result: string[] = []
    let current = ''
    let inQuotes = false
    for (let i = 0; i < line.length; i++) {
      const c = line[i]
      if (c === '"') {
        inQuotes = !inQuotes
      } else if (c === ',' && !inQuotes) {
        result.push(current.trim())
        current = ''
      } else {
        current += c
      }
    }
    result.push(current.trim())
    return result
  }

  const headers = parseLine(lines[0] ?? '')
  const rows = lines.slice(1).map(parseLine)

  return { headers, rows, rowCount: rows.length, colCount: headers.length }
}

// JSON syntax highlight.
function highlightJSON(text: string): string {
  try {
    const formatted = JSON.stringify(JSON.parse(text), null, 2)
    return formatted
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"([^"]+)":/g, '<span class="text-violet-400">"$1"</span>:')
      .replace(/: "([^"]*)"/g, ': <span class="text-green-400">"$1"</span>')
      .replace(/: (\d+\.?\d*)/g, ': <span class="text-amber-400">$1</span>')
      .replace(/: (true|false)/g, ': <span class="text-blue-400">$1</span>')
      .replace(/: (null)/g, ': <span class="text-gray-500">$1</span>')
  } catch {
    return text
  }
}

export function FilePreviewDrawer({ filePath, onClose }: FilePreviewDrawerProps) {
  const [content, setContent] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const fileType = useMemo(() => filePath ? getFileType(filePath) : 'text', [filePath])
  const fileName = useMemo(() => filePath?.split('/').pop() || '', [filePath])

  useEffect(() => {
    if (!filePath) return
    setLoading(true)
    setError(null)

    fetch(`/api/workspace/preview?path=${encodeURIComponent(filePath)}`)
      .then(async (res) => {
        if (!res.ok) throw new Error(`Failed to load: ${res.status}`)
        const text = await res.text()
        // Check if it's a JSON error (binary file).
        try {
          const obj = JSON.parse(text)
          if (obj.error) {
            setError(obj.error)
            return
          }
        } catch {
          // Not JSON error — it's the actual file content.
        }
        setContent(text)
      })
      .catch((err) => {
        setError(err instanceof Error ? err.message : 'Unknown error')
      })
      .finally(() => setLoading(false))
  }, [filePath])

  const handleDownload = () => {
    if (filePath) {
      window.open(`/api/workspace/download?path=${encodeURIComponent(filePath)}`, '_blank')
    }
  }

  const csvData = useMemo(() => {
    if (fileType === 'csv' && content) return parseCSV(content)
    return null
  }, [fileType, content])

  const jsonHighlighted = useMemo(() => {
    if (fileType === 'json' && content) return highlightJSON(content)
    return ''
  }, [fileType, content])

  return (
    <AnimatePresence>
      {filePath && (
        <>
          {/* Backdrop */}
          <motion.div
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            className="fixed inset-0 z-50 bg-black/50"
            onClick={onClose}
          />

          {/* Drawer */}
          <motion.div
            initial={{ x: '100%' }}
            animate={{ x: 0 }}
            exit={{ x: '100%' }}
            transition={{ type: 'spring', damping: 25, stiffness: 300 }}
            className="fixed right-0 top-0 z-[60] flex h-full w-[600px] flex-col border-l border-surface-600 bg-[#0a0b0d]"
          >
            {/* Header */}
            <div className="flex items-center justify-between border-b border-surface-600 px-4 py-3">
              <div className="flex items-center gap-2">
                <h2 className="truncate font-mono text-sm font-bold text-gray-100">
                  {fileName}
                </h2>
                {fileType === 'code' && (
                  <span className="rounded bg-violet-500/20 px-1.5 py-0.5 font-mono text-[10px] text-violet-400">
                    {getLanguageLabel(filePath!)}
                  </span>
                )}
                {fileType === 'csv' && csvData && (
                  <span className="font-mono text-[10px] text-gray-500">
                    {csvData.rowCount} rows × {csvData.colCount} columns
                  </span>
                )}
              </div>
              <div className="flex items-center gap-2">
                <button
                  onClick={handleDownload}
                  className="rounded bg-blue-500/10 px-2.5 py-1 font-mono text-xs text-blue-400 transition-colors hover:bg-blue-500/20"
                >
                  Download
                </button>
                <button
                  onClick={onClose}
                  className="rounded p-1 text-gray-500 transition-colors hover:bg-surface-700 hover:text-gray-300"
                >
                  <svg width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="2">
                    <path d="M4 4l8 8M12 4l-8 8" />
                  </svg>
                </button>
              </div>
            </div>

            {/* Content */}
            <div className="flex-1 overflow-y-auto p-4">
              {loading && (
                <div className="flex items-center justify-center py-16">
                  <div className="h-6 w-6 animate-spin rounded-full border-2 border-violet-500/30 border-t-violet-500" />
                </div>
              )}

              {error && (
                <div className="flex flex-col items-center justify-center py-16 text-center">
                  <span className="text-3xl opacity-40">📦</span>
                  <p className="mt-3 font-mono text-sm text-gray-400">{error}</p>
                  <button
                    onClick={handleDownload}
                    className="mt-4 rounded bg-blue-500/10 px-4 py-2 font-mono text-xs text-blue-400 transition-colors hover:bg-blue-500/20"
                  >
                    Download file
                  </button>
                </div>
              )}

              {!loading && !error && fileType === 'markdown' && (
                <div
                  className="prose prose-invert max-w-none font-sans text-sm"
                  dangerouslySetInnerHTML={{ __html: renderMarkdown(content) }}
                />
              )}

              {!loading && !error && fileType === 'csv' && csvData && (
                <div className="overflow-x-auto rounded-lg border border-surface-600">
                  <table className="w-full font-mono text-xs">
                    <thead>
                      <tr className="sticky top-0 bg-surface-800">
                        {csvData.headers.map((h, i) => (
                          <th
                            key={i}
                            className="border-b border-surface-600 px-3 py-2 text-left font-bold text-violet-400"
                          >
                            {h}
                          </th>
                        ))}
                      </tr>
                    </thead>
                    <tbody>
                      {csvData.rows.map((row, i) => (
                        <tr
                          key={i}
                          className={i % 2 === 0 ? 'bg-surface-800/30' : 'bg-surface-800/60'}
                        >
                          {row.map((cell, j) => (
                            <td key={j} className="border-b border-surface-700/50 px-3 py-1.5 text-gray-300">
                              {cell}
                            </td>
                          ))}
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}

              {!loading && !error && fileType === 'json' && (
                <pre className="overflow-x-auto rounded-lg border border-surface-600 bg-surface-900 p-4">
                  <code
                    className="font-mono text-xs leading-relaxed"
                    dangerouslySetInnerHTML={{ __html: jsonHighlighted }}
                  />
                </pre>
              )}

              {!loading && !error && fileType === 'code' && (
                <pre className="overflow-x-auto rounded-lg border border-surface-600 bg-surface-900 p-4">
                  <code className="font-mono text-xs leading-relaxed text-gray-300">
                    {content.split('\n').map((line, i) => (
                      <div key={i} className="flex">
                        <span className="mr-4 inline-block w-8 select-none text-right text-gray-600">
                          {i + 1}
                        </span>
                        <span className="flex-1 whitespace-pre">{line}</span>
                      </div>
                    ))}
                  </code>
                </pre>
              )}

              {!loading && !error && fileType === 'text' && (
                <pre className="whitespace-pre-wrap rounded-lg border border-surface-600 bg-surface-900 p-4 font-mono text-xs leading-relaxed text-gray-300">
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
