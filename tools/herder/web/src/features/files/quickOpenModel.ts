import type { SpaceDefinition } from '../spaces/spacesModel.ts'

export type QuickOpenActionRow =
  | { kind: 'space', id: string, label: string }
  | { kind: 'agent', name: string, label: string }
  | { kind: 'create', name: string, label: string }
  | { kind: 'send-space', id: string, label: string }
  | { kind: 'send-new', label: string }
  | { kind: 'note', text: string, label: string }

export type QuickOpenEnterTarget = { kind: 'action', index: number } | { kind: 'file', index?: number }
export type QuickOpenKeyboardRow = { kind: 'action', index: number } | { kind: 'file', index: number }

const OPENABLE_KINDS: QuickOpenActionRow['kind'][] = ['space', 'agent']

function matchRank(label: string, query: string) {
  const normalized = label.toLocaleLowerCase()
  if (!query) return 1
  if (normalized === query) return 0
  if (normalized.startsWith(query)) return 1
  if (normalized.includes(query)) return 2
  return -1
}

function ranked<T>(values: T[], label: (value: T) => string, query: string) {
  return values.map((value, index) => ({ value, index, rank: matchRank(label(value), query) }))
    .filter(({ rank }) => rank >= 0)
    .sort((left, right) => left.rank - right.rank || left.index - right.index)
    .map(({ value }) => value)
}

export function quickOpenActionRows(
  rawQuery: string,
  spaces: SpaceDefinition[],
  agents: string[],
  atSpaceCap: boolean,
  hasActivePanel = false,
  activeSpaceID: string | null = null,
): QuickOpenActionRow[] {
  const name = rawQuery.trim()
  const query = name.toLocaleLowerCase()
  const spaceRows: QuickOpenActionRow[] = ranked(spaces, (space) => space.name, query)
    .map((space) => ({ kind: 'space', id: space.id, label: space.name }))
  const agentRows: QuickOpenActionRow[] = ranked(agents, (agent) => agent, query)
    .map((agent) => ({ kind: 'agent', name: agent, label: agent }))
  const exactSpace = spaces.some((space) => space.name.trim().toLocaleLowerCase() === query)
  const sendRows: QuickOpenActionRow[] = hasActivePanel ? ranked([
    ...spaces.filter((space) => space.id !== activeSpaceID).map((space) => ({ kind: 'send-space' as const, id: space.id, label: `Send this pane to ${space.name}` })),
    { kind: 'send-new' as const, label: 'Send this pane to a new space' },
  ], (row) => row.label, query) : []
  return [
    ...spaceRows,
    ...name && !atSpaceCap && !exactSpace
      ? [{ kind: 'create' as const, name, label: `Create space “${name}”` }]
      : [],
    ...sendRows,
    ...agentRows,
    ...name ? [{ kind: 'note' as const, text: name, label: `New note: ${name}` }] : [],
  ]
}

// Keyboard order: every action except the note row, then the files, then the note row last.
export function quickOpenKeyboardRows(rows: QuickOpenActionRow[], fileCount: number): QuickOpenKeyboardRow[] {
  const leading = rows.flatMap((row, index): QuickOpenKeyboardRow[] => row.kind === 'note' ? [] : [{ kind: 'action', index }])
  const files = Array.from({ length: Math.max(0, fileCount) }, (_, index): QuickOpenKeyboardRow => ({ kind: 'file', index }))
  const note = rows.flatMap((row, index): QuickOpenKeyboardRow[] => row.kind === 'note' ? [{ kind: 'action', index }] : [])
  return [...leading, ...files, ...note]
}

// A fresh palette (empty query) starts on the first openable row; a typed query leaves Enter to its implicit order.
export function quickOpenInitialIndex(rows: QuickOpenActionRow[], rawQuery: string) {
  if (rawQuery.trim()) return -1
  return quickOpenKeyboardRows(rows, 0).findIndex((entry) => entry.kind === 'action' && OPENABLE_KINDS.includes(rows[entry.index].kind))
}

export function quickOpenDefaultActionIndex(rows: QuickOpenActionRow[], rawQuery: string) {
  const query = rawQuery.trim().toLocaleLowerCase()
  if (!query) return -1
  const exact = rows.findIndex((row) => row.kind !== 'create' && row.kind !== 'note' && row.label.toLocaleLowerCase() === query)
  if (exact >= 0) return exact
  const agent = rows.findIndex((row) => row.kind === 'agent' && row.name.toLocaleLowerCase().includes(query))
  if (agent >= 0) return agent
  return rows.findIndex((row) => row.kind === 'space' && row.label.toLocaleLowerCase().includes(query))
}

export function quickOpenEnterTarget(
  rows: QuickOpenActionRow[],
  rawQuery: string,
  activeIndex: number,
  fileAvailable: boolean,
  fileCount = fileAvailable ? 1 : 0,
): QuickOpenEnterTarget | null {
  if (activeIndex >= 0) return quickOpenKeyboardRows(rows, fileCount)[activeIndex] ?? null
  const exact = quickOpenDefaultActionIndex(rows, rawQuery)
  if (exact >= 0) return { kind: 'action', index: exact }
  if (fileAvailable) return { kind: 'file' }
  const note = rows.findIndex((row) => row.kind === 'note')
  return note >= 0 ? { kind: 'action', index: note } : null
}
