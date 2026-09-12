import { useEffect, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { queryKeys, resolveFiles } from '../../api/client'
import type { FileCandidate, FileTarget, FolderTarget } from '../../types'
import { keyboardCandidate, mentionLine } from './fileResolution'
import { FileResults } from './FileResults'
import { candidateDestination } from '../folders/folderModel'
import { placementFromModifiers, type OpenPlacement } from '../layout/openPlacement'
import { quickOpenActionRows, quickOpenEnterTarget, quickOpenInitialSelection, quickOpenMoveSelection, quickOpenSelectedIndex, type QuickOpenActionRow, type QuickOpenLookup } from './quickOpenModel.ts'
import { useNotes } from '../notes/NotesProvider.tsx'
import type { SpaceDefinition } from '../spaces/spacesModel.ts'
import { useWorkspaceActionsContext, useWorkspaceData } from '../workspace/workspaceContext.tsx'

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
  const workspaceActions = useWorkspaceActionsContext()
  const workspaceData = useWorkspaceData()
  const notes = useNotes()
  const [query, setQuery] = useState('')
  // The selection is a row identity (see quickOpenSelectionKeys); its index is derived per render.
  const [selection, setSelection] = useState<string | null>(null)
  const inputRef = useRef<HTMLInputElement>(null)
  const restoreFocus = useRef<HTMLElement | null>(null)
  const resultsRef = useRef<HTMLDivElement>(null)
  const debounced = useDebounced(open ? query.trim() : '')
  const resolution = useQuery({
    queryKey: queryKeys.resolve(debounced, agent),
    queryFn: ({ signal }) => resolveFiles(debounced, agent, fetch, signal),
    enabled: open && Boolean(query.trim()) && query.trim() === debounced,
    retry: false,
    gcTime: 30_000,
  })

  const actions = quickOpenActionRows(query, spaces, agents, atSpaceCap, Boolean(workspaceData.activePanel), activeSpaceID)
  const settled = query.trim() === debounced
  const settledResolution = settled ? resolution.data : undefined
  const candidates = settledResolution?.candidates.slice(0, QUICK_OPEN_RESULT_LIMIT) ?? []
  const fileKeys = candidates.map((candidate) => `${candidate.root}\0${candidate.kind}\0${candidate.path}`)
  const activeIndex = quickOpenSelectedIndex(actions, fileKeys, selection)
  useEffect(() => {
    setQuery('')
    setSelection(open ? quickOpenInitialSelection(quickOpenActionRows('', spaces, agents, atSpaceCap, Boolean(workspaceData.activePanel), activeSpaceID), '') : null)
    if (!open) return
    restoreFocus.current = document.activeElement as HTMLElement | null
    const frame = requestAnimationFrame(() => inputRef.current?.focus())
    return () => {
      cancelAnimationFrame(frame)
      restoreFocus.current?.focus()
    }
  }, [open])

  // The selection resets only on a real query edit (see onChange); the debounce settling never touches it.
  useEffect(() => {
    if (activeIndex < 0) return
    resultsRef.current?.querySelector('[aria-selected="true"]')?.scrollIntoView({ block: 'nearest' })
  }, [activeIndex])

  if (!open) return null
  const choose = (candidate: FileCandidate, placement: OpenPlacement) => {
    if (candidateDestination(candidate) === 'folder') onOpenFolder({ root: candidate.root, path: candidate.path }, placement)
    else onOpenFile({ root: candidate.root, path: candidate.path, line: mentionLine(query).line }, placement)
    onClose()
  }
  const leadingCount = actions.filter((row) => row.kind !== 'note').length
  const noteRow = actions.map((row, index) => ({ row, index })).find(({ row }) => row.kind === 'note')
  const noteIndex = noteRow ? leadingCount + candidates.length : -1
  const chooseAction = (row: QuickOpenActionRow) => {
    let chosen = true
    if (row.kind === 'space') chosen = row.id === activeSpaceID || onSwitchSpace(row.id)
    else if (row.kind === 'agent') onOpenAgent(row.name)
    else if (row.kind === 'create') chosen = onCreateSpace(row.name)
    else if (row.kind === 'note') {
      const result = notes.store.add({ group: 'general', text: row.text })
      notes.announce(result.ok ? 'Saved a note in unassigned.' : result.reason)
      chosen = result.ok
    } else if (row.kind === 'send-space') chosen = Boolean(workspaceData.activePanel && workspaceActions.sendPanelToSpace(workspaceData.activePanel.id, workspaceData.activePanel.params, row.id))
    else chosen = Boolean(workspaceData.activePanel && workspaceActions.sendPanelToNewSpace(workspaceData.activePanel.id, workspaceData.activePanel.params))
    if (chosen) onClose()
  }
  const spaceActions = actions.map((row, index) => ({ row, index })).filter(({ row }) => row.kind === 'space' || row.kind === 'create')
  const sendActions = actions.map((row, index) => ({ row, index })).filter(({ row }) => row.kind === 'send-space' || row.kind === 'send-new')
  const agentActions = actions.map((row, index) => ({ row, index })).filter(({ row }) => row.kind === 'agent')
  return <div className="quick-open-backdrop" onMouseDown={(event) => { if (event.target === event.currentTarget) onClose() }}>
    <section className="quick-open" role="dialog" aria-modal="true" aria-label="Quick open spaces, agents, files, or folders">
      <header><strong>Quick open</strong><span>{agent ? `prioritizing ${agent}` : 'all roots'}</span><kbd>Esc</kbd></header>
      <input ref={inputRef} value={query} aria-label="Find a space, agent, file, or folder" placeholder="Type a space, agent, file, or folder…" autoComplete="off" spellCheck={false}
        onChange={(event) => {
          setQuery(event.target.value)
          setSelection(quickOpenInitialSelection(quickOpenActionRows(event.target.value, spaces, agents, atSpaceCap, Boolean(workspaceData.activePanel), activeSpaceID), event.target.value))
        }} onKeyDown={(event) => {
          if (event.key === 'Escape') onClose()
          else if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
            setSelection(quickOpenMoveSelection(actions, fileKeys, selection, event.key === 'ArrowDown' ? 'down' : 'up'))
          } else if (event.key === 'Enter') {
            const candidate = settledResolution
              ? keyboardCandidate(settledResolution, candidates, activeIndex - leadingCount)
              : null
            // The lookup is pending until the query settles and the resolve for it has answered; a settled error counts as no match.
            const lookup: QuickOpenLookup = !query.trim() || (settled && (settledResolution || resolution.error)) ? (candidate ? 'available' : 'none') : 'pending'
            const target = quickOpenEnterTarget(actions, query, activeIndex, lookup, candidates.length)
            if (target?.kind === 'action') chooseAction(actions[target.index])
            else if (target?.kind === 'file' && candidate) choose(candidate, placementFromModifiers(event, groupID))
          } else return
          event.preventDefault()
        }} />
      <div className="quick-open-results" ref={resultsRef}>
        {spaceActions.length > 0 && <section className="quick-open-section" aria-label="Spaces"><strong>Spaces</strong>
          {spaceActions.map(({ row, index }) => <button type="button" role="option" aria-selected={activeIndex === index}
            className={activeIndex === index ? 'active' : ''} key={`${row.kind}:${row.kind === 'space' ? row.id : row.kind === 'create' ? row.name : row.label}`}
            onMouseDown={(event) => event.preventDefault()} onClick={() => chooseAction(row)}>{row.label}</button>)}
        </section>}
        {sendActions.length > 0 && <section className="quick-open-section" aria-label="Pane actions"><strong>Pane actions</strong>
          {sendActions.map(({ row, index }) => <button type="button" role="option" aria-selected={activeIndex === index}
            className={activeIndex === index ? 'active' : ''} key={`${row.kind}:${row.kind === 'send-space' ? row.id : 'new'}`}
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
        <FileResults resolution={settledResolution} activeIndex={activeIndex - leadingCount} onSelect={(candidate, event) => choose(candidate, placementFromModifiers(event, groupID))} limit={QUICK_OPEN_RESULT_LIMIT} />
        {noteRow && <section className="quick-open-section" aria-label="Notes"><strong>Notes</strong>
          <button type="button" role="option" aria-selected={activeIndex === noteIndex}
            className={activeIndex === noteIndex ? 'active' : ''}
            onMouseDown={(event) => event.preventDefault()} onClick={() => chooseAction(noteRow.row)}>{noteRow.row.label}</button>
        </section>}
      </div>
      <footer><span>↑↓ choose</span><span>Enter open</span><span>No match · Enter saves a note</span><span>Results are ranked by the server</span></footer>
    </section>
  </div>
}
