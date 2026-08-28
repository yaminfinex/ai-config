import { useEffect, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { queryKeys, resolveFiles } from '../../api/client'
import type { FileCandidate, FileTarget } from '../../types'
import { keyboardCandidate, mentionLine } from './fileResolution'
import { FileResults } from './FileResults'

const QUICK_OPEN_RESULT_LIMIT = 100

function useDebounced(value: string, delay = 120) {
  const [debounced, setDebounced] = useState(value)
  useEffect(() => {
    const timer = window.setTimeout(() => setDebounced(value), delay)
    return () => window.clearTimeout(timer)
  }, [delay, value])
  return debounced
}

export function QuickOpen({ open, agent, onClose, onOpenFile }: { open: boolean, agent?: string, onClose: () => void, onOpenFile: (target: FileTarget) => void }) {
  const [query, setQuery] = useState('')
  const [activeIndex, setActiveIndex] = useState(-1)
  const inputRef = useRef<HTMLInputElement>(null)
  const restoreFocus = useRef<HTMLElement | null>(null)
  const debounced = useDebounced(query.trim())
  const resolution = useQuery({
    queryKey: queryKeys.resolve(debounced, agent),
    queryFn: ({ signal }) => resolveFiles(debounced, agent, fetch, signal),
    enabled: open && Boolean(debounced),
    retry: false,
    gcTime: 30_000,
  })

  useEffect(() => {
    if (!open) return
    restoreFocus.current = document.activeElement as HTMLElement | null
    setActiveIndex(-1)
    requestAnimationFrame(() => inputRef.current?.focus())
    return () => restoreFocus.current?.focus()
  }, [open])

  useEffect(() => setActiveIndex(-1), [debounced])

  if (!open) return null
  const choose = (candidate: FileCandidate) => {
    onOpenFile({ root: candidate.root, path: candidate.path, line: mentionLine(query).line })
    onClose()
  }
  const settled = query.trim() === debounced
  const settledResolution = settled ? resolution.data : undefined
  const candidates = settledResolution?.candidates.slice(0, QUICK_OPEN_RESULT_LIMIT) ?? []
  return <div className="quick-open-backdrop" onMouseDown={(event) => { if (event.target === event.currentTarget) onClose() }}>
    <section className="quick-open" role="dialog" aria-modal="true" aria-label="Quick open file">
      <header><strong>Quick open</strong><span>{agent ? `prioritizing ${agent}` : 'all roots'}</span><kbd>Esc</kbd></header>
      <input ref={inputRef} value={query} aria-label="Find a file" placeholder="Type a filename or path…" autoComplete="off" spellCheck={false}
        onChange={(event) => setQuery(event.target.value)} onKeyDown={(event) => {
          if (event.key === 'Escape') onClose()
          else if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
            if (candidates.length > 0) setActiveIndex((index) => {
              if (index < 0) return event.key === 'ArrowDown' ? 0 : candidates.length - 1
              return (index + (event.key === 'ArrowDown' ? 1 : -1) + candidates.length) % candidates.length
            })
          } else if (event.key === 'Enter' && settledResolution) {
            const candidate = keyboardCandidate(settledResolution, candidates, activeIndex)
            if (candidate) choose(candidate)
          } else return
          event.preventDefault()
        }} />
      <div className="quick-open-results">
        {query.trim() && !settled && <p className="file-results-empty">Searching…</p>}
        {settled && resolution.isPending && debounced && <p className="file-results-empty">Searching current roots…</p>}
        {settled && resolution.error && <p className="file-results-error" role="alert">{resolution.error.message}</p>}
        <FileResults resolution={settledResolution} activeIndex={activeIndex} onSelect={choose} limit={QUICK_OPEN_RESULT_LIMIT} />
      </div>
      <footer><span>↑↓ choose</span><span>Enter open</span><span>Results are ranked by the server</span></footer>
    </section>
  </div>
}
