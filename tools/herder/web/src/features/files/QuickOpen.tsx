import { useEffect, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { queryKeys, resolveFiles } from '../../api/client'
import type { FileCandidate, FileTarget, FolderTarget } from '../../types'
import { keyboardCandidate, mentionLine } from './fileResolution'
import { FileResults } from './FileResults'
import { candidateDestination } from '../folders/folderModel'
import { placementFromModifiers, type OpenPlacement } from '../layout/openPlacement'

const QUICK_OPEN_RESULT_LIMIT = 100

function useDebounced(value: string, delay = 120) {
  const [debounced, setDebounced] = useState(value)
  useEffect(() => {
    const timer = window.setTimeout(() => setDebounced(value), delay)
    return () => window.clearTimeout(timer)
  }, [delay, value])
  return debounced
}

export function QuickOpen({ open, agent, groupID, onClose, onOpenFile, onOpenFolder }: { open: boolean, agent?: string, groupID?: string, onClose: () => void, onOpenFile: (target: FileTarget, placement?: OpenPlacement) => void, onOpenFolder: (target: FolderTarget, placement?: OpenPlacement) => void }) {
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
    const frame = requestAnimationFrame(() => inputRef.current?.focus())
    return () => {
      cancelAnimationFrame(frame)
      restoreFocus.current?.focus()
    }
  }, [open])

  useEffect(() => setActiveIndex(-1), [debounced])

  if (!open) return null
  const choose = (candidate: FileCandidate, placement: OpenPlacement) => {
    if (candidateDestination(candidate) === 'folder') onOpenFolder({ root: candidate.root, path: candidate.path }, placement)
    else onOpenFile({ root: candidate.root, path: candidate.path, line: mentionLine(query).line }, placement)
    onClose()
  }
  const settled = query.trim() === debounced
  const settledResolution = settled ? resolution.data : undefined
  const candidates = settledResolution?.candidates.slice(0, QUICK_OPEN_RESULT_LIMIT) ?? []
  return <div className="quick-open-backdrop" onMouseDown={(event) => { if (event.target === event.currentTarget) onClose() }}>
    <section className="quick-open" role="dialog" aria-modal="true" aria-label="Quick open file or folder">
      <header><strong>Quick open</strong><span>{agent ? `prioritizing ${agent}` : 'all roots'}</span><kbd>Esc</kbd></header>
      <input ref={inputRef} value={query} aria-label="Find a file or folder" placeholder="Type a file or folder path…" autoComplete="off" spellCheck={false}
        onChange={(event) => setQuery(event.target.value)} onKeyDown={(event) => {
          if (event.key === 'Escape') onClose()
          else if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
            if (candidates.length > 0) setActiveIndex((index) => {
              if (index < 0) return event.key === 'ArrowDown' ? 0 : candidates.length - 1
              return (index + (event.key === 'ArrowDown' ? 1 : -1) + candidates.length) % candidates.length
            })
          } else if (event.key === 'Enter' && settledResolution) {
            const candidate = keyboardCandidate(settledResolution, candidates, activeIndex)
            if (candidate) choose(candidate, placementFromModifiers(event, groupID))
          } else return
          event.preventDefault()
        }} />
      <div className="quick-open-results">
        {query.trim() && !settled && <p className="file-results-empty">Searching…</p>}
        {settled && resolution.isPending && debounced && <p className="file-results-empty">Searching current roots…</p>}
        {settled && resolution.error && <p className="file-results-error" role="alert">{resolution.error.message}</p>}
        <FileResults resolution={settledResolution} activeIndex={activeIndex} onSelect={(candidate, event) => choose(candidate, placementFromModifiers(event, groupID))} limit={QUICK_OPEN_RESULT_LIMIT} />
      </div>
      <footer><span>↑↓ choose</span><span>Enter open</span><span>Results are ranked by the server</span></footer>
    </section>
  </div>
}
