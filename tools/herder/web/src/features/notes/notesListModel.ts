import type { Note } from './notesStore.ts'
import { selectorInitialValue } from './notesSelectorModel.ts'

export type NoteSelection = {
  selected: Set<string>
  anchor?: string
  cursor?: string
}

type ClickModifiers = { command?: boolean, shift?: boolean }
type KeyLike = { key: string, metaKey?: boolean, ctrlKey?: boolean, shiftKey?: boolean }
export type NoteListAction = 'copy' | 'delete' | 'hand-off' | 'assign' | 'edit' | 'clear' | null

function range(ordered: string[], fromID: string, toID: string) {
  const from = ordered.indexOf(fromID)
  const to = ordered.indexOf(toID)
  if (from < 0 || to < 0) return new Set([toID])
  return new Set(ordered.slice(Math.min(from, to), Math.max(from, to) + 1))
}

export function selectionAfterClick(state: NoteSelection, ordered: string[], id: string, modifiers: ClickModifiers): NoteSelection {
  if (modifiers.shift) {
    const anchor = state.anchor && ordered.includes(state.anchor) ? state.anchor : id
    return { selected: range(ordered, anchor, id), anchor, cursor: id }
  }
  if (modifiers.command) {
    const selected = new Set(state.selected)
    if (selected.has(id)) selected.delete(id)
    else selected.add(id)
    return { selected, anchor: id, cursor: id }
  }
  return { selected: new Set([id]), anchor: id, cursor: id }
}

export function selectionAfterArrow(state: NoteSelection, ordered: string[], direction: -1 | 1, extend: boolean): NoteSelection {
  if (ordered.length === 0) return state
  const current = state.cursor && ordered.includes(state.cursor) ? ordered.indexOf(state.cursor) : direction > 0 ? -1 : ordered.length
  const nextIndex = Math.max(0, Math.min(ordered.length - 1, current + direction))
  const cursor = ordered[nextIndex]
  if (!extend) return { selected: new Set([cursor]), anchor: cursor, cursor }
  const anchor = state.anchor && ordered.includes(state.anchor) ? state.anchor : cursor
  return { selected: range(ordered, anchor, cursor), anchor, cursor }
}

export function pruneNoteSelection(state: NoteSelection, ordered: string[]): NoteSelection {
  const present = new Set(ordered)
  const selected = new Set([...state.selected].filter((id) => present.has(id)))
  return {
    selected,
    anchor: state.anchor && present.has(state.anchor) ? state.anchor : undefined,
    cursor: state.cursor && present.has(state.cursor) ? state.cursor : undefined,
  }
}

export function noteListAction(event: KeyLike, editable: boolean): NoteListAction {
  if (editable) return null
  const key = event.key.toLowerCase()
  if ((event.metaKey || event.ctrlKey) && key === 'c') return 'copy'
  if (event.key === 'Backspace' || event.key === 'Delete') return 'delete'
  if (event.key === 'Enter') return 'hand-off'
  if (!event.metaKey && !event.ctrlKey && !event.shiftKey && key === 'a') return 'assign'
  if (!event.metaKey && !event.ctrlKey && !event.shiftKey && key === 'e') return 'edit'
  if (event.key === 'Escape') return 'clear'
  return null
}

export function dragNoteIDs(id: string, selected: Set<string>) {
  return selected.has(id) ? [...selected] : [id]
}

export function handOffRoute(notes: Note[], liveOrder: string[]): { kind: 'direct', target: string } | { kind: 'selector', initial: string } {
  const live = new Set(liveOrder)
  const groups = new Set(notes.map((note) => note.group))
  const sole = groups.size === 1 ? notes[0]?.group : undefined
  if (sole && live.has(sole)) return { kind: 'direct', target: sole }
  const rows = liveOrder.map((value) => ({ value, label: value }))
  const initial = selectorInitialValue(notes, rows, 'destination')
  return { kind: 'selector', initial }
}
