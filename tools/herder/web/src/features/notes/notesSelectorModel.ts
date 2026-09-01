import type { Note } from './notesStore.ts'

export type NotesSelectorMode = 'assign' | 'destination'
export type NotesSelectorRow = { value: string, label: string }

export function selectorRows(notes: Note[], roster: string[], mode: NotesSelectorMode): NotesSelectorRow[] {
  const live = [...new Set(roster)].sort((left, right) => left.localeCompare(right))
  const liveSet = new Set(live)
  const activity = new Map<string, number>()
  for (const note of notes) {
    if (liveSet.has(note.group)) activity.set(note.group, Math.max(activity.get(note.group) ?? 0, note.updated))
  }
  const noted = live.filter((agent) => activity.has(agent)).sort((left, right) => (activity.get(right) ?? 0) - (activity.get(left) ?? 0) || left.localeCompare(right))
  const remaining = live.filter((agent) => !activity.has(agent))
  return [
    ...(mode === 'assign' ? [{ value: 'general', label: 'unassigned' }] : []),
    ...noted.concat(remaining).map((agent) => ({ value: agent, label: agent })),
  ]
}

export function selectorInitialValue(selected: Note[], rows: NotesSelectorRow[], mode: NotesSelectorMode) {
  const values = new Set(rows.map((row) => row.value))
  if (mode === 'assign') {
    const groups = new Set(selected.map((note) => note.group))
    const current = groups.size === 1 ? selected[0]?.group : undefined
    return current && values.has(current) ? current : 'general'
  }
  const counts = new Map<string, number>()
  for (const note of selected) if (values.has(note.group)) counts.set(note.group, (counts.get(note.group) ?? 0) + 1)
  const highest = Math.max(0, ...counts.values())
  return rows.find((row) => (counts.get(row.value) ?? 0) === highest)?.value ?? rows[0]?.value ?? ''
}

export function filterSelectorRows(rows: NotesSelectorRow[], query: string) {
  const needle = query.trim().toLocaleLowerCase()
  return needle ? rows.filter((row) => row.label.toLocaleLowerCase().includes(needle)) : rows
}

export function selectorMove(rows: NotesSelectorRow[], highlighted: string, direction: -1 | 1) {
  if (rows.length === 0) return ''
  const current = rows.findIndex((row) => row.value === highlighted)
  const index = current < 0 ? direction === 1 ? 0 : rows.length - 1 : (current + direction + rows.length) % rows.length
  return rows[index]?.value ?? ''
}

export function selectorBackspace(query: string, highlighted: string, mode: NotesSelectorMode) {
  if (query || mode !== 'assign') return { handled: false, highlighted }
  return { handled: true, highlighted: 'general' }
}
