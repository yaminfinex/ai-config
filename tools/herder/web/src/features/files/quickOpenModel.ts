import type { SpaceDefinition } from '../spaces/spacesModel.ts'

export type QuickOpenActionRow =
  | { kind: 'space', id: string, label: string }
  | { kind: 'agent', name: string, label: string }
  | { kind: 'create', name: string, label: string }

export type QuickOpenEnterTarget = { kind: 'action', index: number } | { kind: 'file' }

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
): QuickOpenActionRow[] {
  const name = rawQuery.trim()
  const query = name.toLocaleLowerCase()
  const spaceRows: QuickOpenActionRow[] = ranked(spaces, (space) => space.name, query)
    .map((space) => ({ kind: 'space', id: space.id, label: space.name }))
  const agentRows: QuickOpenActionRow[] = ranked(agents, (agent) => agent, query)
    .map((agent) => ({ kind: 'agent', name: agent, label: agent }))
  const exactSpace = spaces.some((space) => space.name.trim().toLocaleLowerCase() === query)
  return [
    ...spaceRows,
    ...name && !atSpaceCap && !exactSpace
      ? [{ kind: 'create' as const, name, label: `Create space “${name}”` }]
      : [],
    ...agentRows,
  ]
}

export function quickOpenDefaultActionIndex(rows: QuickOpenActionRow[], rawQuery: string) {
  const query = rawQuery.trim().toLocaleLowerCase()
  return query ? rows.findIndex((row) => row.kind !== 'create' && row.label.toLocaleLowerCase() === query) : -1
}

export function quickOpenEnterTarget(
  rows: QuickOpenActionRow[],
  rawQuery: string,
  activeIndex: number,
  fileAvailable: boolean,
): QuickOpenEnterTarget | null {
  if (activeIndex >= 0) return activeIndex < rows.length
    ? { kind: 'action', index: activeIndex }
    : fileAvailable ? { kind: 'file' } : null
  const exact = quickOpenDefaultActionIndex(rows, rawQuery)
  if (exact >= 0) return { kind: 'action', index: exact }
  return fileAvailable ? { kind: 'file' } : null
}
