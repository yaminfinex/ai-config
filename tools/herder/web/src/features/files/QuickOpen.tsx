import { useEffect, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { queryKeys, resolveFiles } from '../../api/client'
import type { FileCandidate, FileTarget, FolderTarget } from '../../types'
import { keyboardCandidate, mentionLine } from './fileResolution'
import { FileResults } from './FileResults'
import { candidateDestination } from '../folders/folderModel'
import { placementFromModifiers, type OpenPlacement } from '../layout/openPlacement'
import { quickOpenActionRows, quickOpenDefaultActionIndex, type QuickOpenActionRow } from './quickOpenModel.ts'
import type { SpaceDefinition } from '../spaces/spacesModel.ts'

const QUICK_OPEN_RESULT_LIMIT = 100

function useDebounced(value: string, delay = 120) {
  const [debounced, setDebounced] = useState(value)
  useEffect(() => {
    const timer = window.setTimeout(() => setDebounced(value), delay)
    return () => window.clearTimeout(timer)
  }, [delay, value])
  return debounced
}

export function QuickOpen({ open, agent, groupID, spaces, activeSpaceID, agents, atSpaceCap, onClose, onOpenFile, onOpenFolder, onOpenAgent, onSwitchSpace, onCreateSpace }: {
  open: boolean
  agent?: string
  groupID?: string
  spaces: SpaceDefinition[]
  activeSpaceID: string | null
  agents: string[]
  atSpaceCap: boolean
  onClose: () => void
  onOpenFile: (target: FileTarget, placement?: OpenPlacement) => void
  onOpenFolder: (target: FolderTarget, placement?: OpenPlacement) => void
  onOpenAgent: (name: string) => void
  onSwitchSpace: (id: string) => boolean
  onCreateSpace: (name: string) => boolean
}) {
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

  useEffect(() => setActiveIndex(-1), [debounced, query])

  if (!open) return null
  const choose = (candidate: FileCandidate, placement: OpenPlacement) => {
    if (candidateDestination(candidate) === 'folder') onOpenFolder({ root: candidate.root, path: candidate.path }, placement)
    else onOpenFile({ root: candidate.root, path: candidate.path, line: mentionLine(query).line }, placement)
    onClose()
  }
  const settled = query.trim() === debounced
  const settledResolution = settled ? resolution.data : undefined
  const candidates = settledResolution?.candidates.slice(0, QUICK_OPEN_RESULT_LIMIT) ?? []
  const actions = quickOpenActionRows(query, spaces, agents, atSpaceCap)
  const totalRows = actions.length + candidates.length
  const chooseAction = (row: QuickOpenActionRow) => {
    let chosen = true
    if (row.kind === 'space') chosen = row.id === activeSpaceID || onSwitchSpace(row.id)
    else if (row.kind === 'agent') onOpenAgent(row.name)
    else chosen = onCreateSpace(row.name)
    if (chosen) onClose()
  }
  const spaceActions = actions.map((row, index) => ({ row, index })).filter(({ row }) => row.kind !== 'agent')
  const agentActions = actions.map((row, index) => ({ row, index })).filter(({ row }) => row.kind === 'agent')
  return <div className="quick-open-backdrop" onMouseDown={(event) => { if (event.target === event.currentTarget) onClose() }}>
    <section className="quick-open" role="dialog" aria-modal="true" aria-label="Quick open spaces, agents, files, or folders">
      <header><strong>Quick open</strong><span>{agent ? `prioritizing ${agent}` : 'all roots'}</span><kbd>Esc</kbd></header>
      <input ref={inputRef} value={query} aria-label="Find a space, agent, file, or folder" placeholder="Type a space, agent, file, or folder…" autoComplete="off" spellCheck={false}
        onChange={(event) => setQuery(event.target.value)} onKeyDown={(event) => {
          if (event.key === 'Escape') onClose()
          else if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
            if (totalRows > 0) setActiveIndex((index) => {
              if (index < 0) return event.key === 'ArrowDown' ? 0 : totalRows - 1
              return (index + (event.key === 'ArrowDown' ? 1 : -1) + totalRows) % totalRows
            })
          } else if (event.key === 'Enter') {
            const action = actions[activeIndex < 0 ? quickOpenDefaultActionIndex(actions, query) : activeIndex]
            if (action) chooseAction(action)
            else if (settledResolution) {
              const candidate = keyboardCandidate(settledResolution, candidates, activeIndex - actions.length)
              if (candidate) choose(candidate, placementFromModifiers(event, groupID))
            }
          } else return
          event.preventDefault()
        }} />
      <div className="quick-open-results">
        {spaceActions.length > 0 && <section className="quick-open-section" aria-label="Spaces"><strong>Spaces</strong>
          {spaceActions.map(({ row, index }) => <button type="button" role="option" aria-selected={activeIndex === index}
            className={activeIndex === index ? 'active' : ''} key={`${row.kind}:${row.kind === 'space' ? row.id : row.name}`}
            onMouseDown={(event) => event.preventDefault()} onClick={() => chooseAction(row)}>{row.label}</button>)}
        </section>}
        {agentActions.length > 0 && <section className="quick-open-section" aria-label="Live agents"><strong>Live agents</strong>
          {agentActions.map(({ row, index }) => <button type="button" role="option" aria-selected={activeIndex === index}
            className={activeIndex === index ? 'active' : ''} key={`agent:${row.kind === 'agent' ? row.name : index}`}
            onMouseDown={(event) => event.preventDefault()} onClick={() => chooseAction(row)}>{row.label}</button>)}
        </section>}
        {query.trim() && !settled && <p className="file-results-empty">Searching…</p>}
        {settled && resolution.isPending && debounced && <p className="file-results-empty">Searching current roots…</p>}
        {settled && resolution.error && <p className="file-results-error" role="alert">{resolution.error.message}</p>}
        {settledResolution && <div className="quick-open-section-label">Files and folders</div>}
        <FileResults resolution={settledResolution} activeIndex={activeIndex - actions.length} onSelect={(candidate, event) => choose(candidate, placementFromModifiers(event, groupID))} limit={QUICK_OPEN_RESULT_LIMIT} />
      </div>
      <footer><span>↑↓ choose</span><span>Enter open</span><span>Results are ranked by the server</span></footer>
    </section>
  </div>
}
