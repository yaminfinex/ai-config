import type { Board, Row } from '../../types.ts'
import type { Note, NoteSource } from './notesStore.ts'

export function liveRosterNames(board: Board | undefined) {
  const names = new Set<string>()
  const add = (row: Row) => {
    if (row.agent && row.agent !== '-' && row.bus_status !== 'retired') names.add(row.agent)
    row.subagents?.forEach(add)
  }
  board?.workspaces.forEach((workspace) => workspace.tabs.forEach((tab) => tab.panes.forEach(add)))
  board?.unplaced.forEach(add)
  return [...names].sort()
}

export function noteGroupRows(notes: Note[], roster: string[]) {
  const rosterSet = new Set(roster)
  const updated = new Map<string, number>([['general', Number.POSITIVE_INFINITY]])
  for (const note of notes) updated.set(note.group, Math.max(updated.get(note.group) ?? 0, note.updated))
  return [...updated].sort(([leftGroup, leftTime], [rightGroup, rightTime]) => {
    if (leftGroup === 'general') return -1
    if (rightGroup === 'general') return 1
    return rightTime - leftTime || leftGroup.localeCompare(rightGroup)
  }).map(([group]) => ({ group, orphaned: group !== 'general' && !rosterSet.has(group) }))
}

export function noteSourceLabel(source: NoteSource | undefined) {
  if (!source) return ''
  if (source.kind === 'transcript') return `Transcript: ${source.agent}`
  const range = source.start === undefined ? '' : `:${source.start}${source.end !== undefined && source.end !== source.start ? `-${source.end}` : ''}`
  const path = `${source.path}${range}`
  return source.kind === 'diff' ? `${path} (vs ${source.base})` : path
}

export function noteTransferText(note: Note) {
  const source = noteSourceLabel(note.source)
  return source ? `${source}\n${note.text}` : note.text
}

export type SelectionGesture = 'plain' | 'toggle' | 'range'

export function selectionAfterGesture(selected: Set<string>, ordered: string[], id: string, gesture: SelectionGesture, anchor?: string) {
  if (gesture === 'plain') return { selected, anchor }
  const next = new Set(selected)
  if (gesture === 'toggle') {
    if (next.has(id)) next.delete(id)
    else next.add(id)
    return { selected: next, anchor: id }
  }
  const from = Math.max(0, ordered.indexOf(anchor ?? id))
  const to = Math.max(0, ordered.indexOf(id))
  for (const ranged of ordered.slice(Math.min(from, to), Math.max(from, to) + 1)) next.add(ranged)
  return { selected: next, anchor: anchor ?? id }
}

export const notesStripPreferencePrefix = `${'herder.web.notes.v1:'}strip:`

export function readNotesStripCollapsed(agent: string, storage: Pick<Storage, 'getItem'> | null = browserStorage()) {
  try { return storage?.getItem(`${notesStripPreferencePrefix}${encodeURIComponent(agent)}`) === 'true' } catch { return false }
}

export function persistNotesStripCollapsed(agent: string, collapsed: boolean, storage: Pick<Storage, 'setItem'> | null = browserStorage()) {
  try { storage?.setItem(`${notesStripPreferencePrefix}${encodeURIComponent(agent)}`, String(collapsed)) } catch { /* preference remains session-only */ }
}

function browserStorage(): Storage | null {
  try { return window.localStorage } catch { return null }
}
