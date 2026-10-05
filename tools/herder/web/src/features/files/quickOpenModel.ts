import type { SpaceDefinition } from '../spaces/spacesModel.ts'

export type QuickOpenActionRow =
  | { kind: 'space', id: string, label: string }
  | { kind: 'agent', name: string, label: string }
  | { kind: 'create', name: string, label: string }
  | { kind: 'send-space', id: string, label: string }
  | { kind: 'send-new', label: string }
  | { kind: 'note', text: string, label: string }
  | { kind: 'reassign-action', subject: string, label: string }
  | { kind: 'reassign', subject: string, target: string, label: string, title?: string }

export type QuickOpenMode = { kind: 'normal' } | { kind: 'reassign', subject: string }

export type QuickOpenKeyboardRow = { kind: 'action', index: number } | { kind: 'file', index: number }

const OPENABLE_KINDS: QuickOpenActionRow['kind'][] = ['space', 'agent', 'reassign']

function matchRank(label: string, query: string) {
  const normalized = label.toLocaleLowerCase()
  if (!query) return 1
  if (normalized === query) return 0
  if (normalized.startsWith(query)) return 1
  if (normalized.includes(query)) return 2
  return -1
}

export function ranked<T>(values: T[], label: (value: T) => string, query: string) {
  return values.map((value, index) => ({ value, index, rank: matchRank(label(value), query) }))
    .filter(({ rank }) => rank >= 0)
    .sort((left, right) => left.rank - right.rank || left.index - right.index)
    .map(({ value }) => value)
}

export function quickOpenActionRows(
  rawQuery: string,
  spaces: SpaceDefinition[],
  agents: string[],
  hasActivePanel = false,
  activeSpaceID: string | null = null,
  reassignSubject?: string,
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
  const reassignRows: QuickOpenActionRow[] = reassignSubject ? ranked([
    { kind: 'reassign-action' as const, subject: reassignSubject, label: `Reassign ${reassignSubject}…` },
  ], (row) => `${row.label} ${row.subject}`, query) : []
  return [
    ...spaceRows,
    ...name && !exactSpace
      ? [{ kind: 'create' as const, name, label: `Create space “${name}”` }]
      : [],
    ...sendRows,
    ...reassignRows,
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

// A selection is remembered by identity, not position, so rows arriving or leaving (a file lookup
// landing under a selected note row) never move it onto a different row.
export function quickOpenRowKey(row: QuickOpenActionRow): string {
  switch (row.kind) {
    case 'space': return `action:space:${row.id}`
    case 'agent': return `action:agent:${row.name}`
    case 'create': return `action:create:${row.name}`
    case 'send-space': return `action:send-space:${row.id}`
    case 'send-new': return 'action:send-new'
    case 'note': return 'action:note'
    case 'reassign-action': return `action:reassign:${row.subject}`
    case 'reassign': return `action:reassign-target:${row.subject}:${row.target}`
  }
}

export function quickOpenSelectionKeys(rows: QuickOpenActionRow[], fileKeys: string[]): string[] {
  return quickOpenKeyboardRows(rows, fileKeys.length).map((entry) => entry.kind === 'action' ? quickOpenRowKey(rows[entry.index]) : `file:${fileKeys[entry.index]}`)
}

export function quickOpenSelectedIndex(rows: QuickOpenActionRow[], fileKeys: string[], key: string | null) {
  return key === null ? -1 : quickOpenSelectionKeys(rows, fileKeys).indexOf(key)
}

export function quickOpenMoveSelection(rows: QuickOpenActionRow[], fileKeys: string[], key: string | null, direction: 'up' | 'down'): string | null {
  const keys = quickOpenSelectionKeys(rows, fileKeys)
  if (keys.length === 0) return null
  const current = key === null ? -1 : keys.indexOf(key)
  if (current < 0) return direction === 'down' ? keys[0] : keys[keys.length - 1]
  return keys[(current + (direction === 'down' ? 1 : -1) + keys.length) % keys.length]
}

// A fresh palette (empty query) starts on the first openable row; a typed query starts on its top match.
export function quickOpenInitialIndex(rows: QuickOpenActionRow[], rawQuery: string) {
  if (rawQuery.trim()) return -1
  return quickOpenKeyboardRows(rows, 0).findIndex((entry) => entry.kind === 'action' && OPENABLE_KINDS.includes(rows[entry.index].kind))
}

export function quickOpenInitialSelection(rows: QuickOpenActionRow[], rawQuery: string): string | null {
  const index = quickOpenInitialIndex(rows, rawQuery)
  return index < 0 ? null : quickOpenSelectionKeys(rows, [])[index]
}

// The top-match tier of one action row, best first: exact space, exact agent, any other exact action,
// space prefix, agent prefix, space contains, agent contains. Create and note never rank here.
function topMatchTier(row: QuickOpenActionRow, query: string) {
  const rank = matchRank(row.label, query)
  if (rank < 0) return -1
  if (row.kind === 'space') return [0, 3, 5][rank]
  if (row.kind === 'agent') return [1, 4, 6][rank]
  if (row.kind === 'create' || row.kind === 'note') return -1
  return rank === 0 ? 2 : -1
}

// The row a typed query selects before the user moves it: the best-tier action (rendered order breaks a
// tie), else the confident file (autoOpenCandidate's, as its file key; a fuzzy file never preselects, so
// free text still lands on the note), else the note, else create. While the file lookup is still running
// and no action matches, nothing is selected: a confident file may yet arrive above the note.
export function quickOpenTopMatch(rows: QuickOpenActionRow[], confidentFileKey: string | null, rawQuery: string, lookupPending = false): string | null {
  const query = rawQuery.trim().toLocaleLowerCase()
  if (!query) return quickOpenInitialSelection(rows, '')
  let best = -1
  let bestTier = Infinity
  rows.forEach((row, index) => {
    const tier = topMatchTier(row, query)
    if (tier >= 0 && tier < bestTier) [best, bestTier] = [index, tier]
  })
  if (best >= 0) return quickOpenRowKey(rows[best])
  if (confidentFileKey !== null) return `file:${confidentFileKey}`
  if (lookupPending) return null
  const fallback = rows.find((row) => row.kind === 'note') ?? rows.find((row) => row.kind === 'create')
  return fallback ? quickOpenRowKey(fallback) : null
}

// The highlighted row: the one the user moved to with the arrows while it is still listed, else the
// fallback (the top match, or reassign mode's own pick). A query edit clears the moved row.
export function quickOpenSelection(rows: QuickOpenActionRow[], fileKeys: string[], moved: string | null, fallback: string | null): string | null {
  return moved !== null && quickOpenSelectionKeys(rows, fileKeys).includes(moved) ? moved : fallback
}

// Enter acts on exactly the highlighted row; nothing highlighted, nothing happens.
export function quickOpenEnterTarget(rows: QuickOpenActionRow[], fileCount: number, activeIndex: number): QuickOpenKeyboardRow | null {
  return activeIndex >= 0 ? quickOpenKeyboardRows(rows, fileCount)[activeIndex] ?? null : null
}
