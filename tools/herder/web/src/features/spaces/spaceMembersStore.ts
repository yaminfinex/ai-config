import { defaultRandomID } from '../notes/notesStore.ts'
import { compareStateVersions } from '../../shared/stateVersion.ts'
import type { GenericStateRow } from '../../shared/stateSync.ts'
import {
  memberID,
  parseMembersRow,
  type SpaceMember,
  type SpaceMembersLocal,
  type SpaceMembersReconcileStore,
  type SpaceMembersRow,
  type SpaceMembersValue,
} from './spaceMembersModel.ts'

export const spaceMembersRowsKey = 'herder.web.space-members.v1:rows'
export const spaceMembersLocalKey = 'herder.web.space-members.v1:local'

type StorageLike = Pick<Storage, 'getItem' | 'setItem'>

type Options = {
  storage?: StorageLike | null
  now?: () => number
  randomID?: () => string
}

export type SpaceMembersStore = SpaceMembersReconcileStore & {
  rows: () => GenericStateRow[]
  merge: (rows: GenericStateRow[]) => void
  subscribeMutations: (listener: (rows: GenericStateRow[]) => void) => () => void
}

export function membersStateRow(row: SpaceMembersRow): GenericStateRow {
  const value: SpaceMembersValue = { members: row.members.map((member) => member.kind === 'agent'
    ? { kind: 'agent', name: member.name }
    : { kind: 'file', root: member.root, path: member.path }), updated: row.updated }
  return { key: row.key, value, updated: row.updated, writeID: row.writeID, deleted: row.deleted }
}

function readJSON(storage: StorageLike | null, key: string): unknown {
  try { return JSON.parse(storage?.getItem(key) ?? 'null') } catch { return null }
}

function parseLocal(value: unknown): SpaceMembersLocal {
  if (!value || typeof value !== 'object') return {}
  const local = value as { baseline?: unknown, applied?: unknown }
  const baseline = Array.isArray(local.baseline)
    ? parseMembersRow({ key: 'baseline', value: { members: local.baseline }, updated: 0, writeID: 'baseline', deleted: false })?.members
    : undefined
  return {
    ...(baseline ? { baseline } : {}),
    ...(typeof local.applied === 'string' && local.applied ? { applied: local.applied } : {}),
  }
}

// createSpaceMembersStore holds the last-write-wins spaces.members rows this
// browser knows, plus per-space local bookkeeping (baseline and applied write)
// the reconciler needs to publish only real membership changes. Both persist
// in localStorage so a reload neither re-adds a tab the user closed nor
// re-publishes an unchanged space; losing storage only costs a republish.
export function createSpaceMembersStore(options: Options = {}): SpaceMembersStore {
  const storage = options.storage === undefined ? browserStorage() : options.storage
  const now = options.now ?? Date.now
  const randomID = options.randomID ?? defaultRandomID
  const rows = new Map<string, SpaceMembersRow>()
  const locals = new Map<string, SpaceMembersLocal>()
  const mutationListeners = new Set<(rows: GenericStateRow[]) => void>()

  const storedRows = readJSON(storage, spaceMembersRowsKey)
  if (Array.isArray(storedRows)) {
    for (const raw of storedRows) {
      const row = parseMembersRow(raw)
      if (row) rows.set(row.key, row)
    }
  }
  const storedLocals = readJSON(storage, spaceMembersLocalKey)
  if (storedLocals && typeof storedLocals === 'object' && !Array.isArray(storedLocals)) {
    for (const [key, value] of Object.entries(storedLocals)) locals.set(key, parseLocal(value))
  }

  const persistRows = () => {
    try { storage?.setItem(spaceMembersRowsKey, JSON.stringify([...rows.values()].map(membersStateRow))) } catch { /* sync still carries the rows */ }
  }
  const persistLocals = () => {
    try { storage?.setItem(spaceMembersLocalKey, JSON.stringify(Object.fromEntries(locals))) } catch { /* next load republishes */ }
  }
  const write = (row: SpaceMembersRow) => {
    rows.set(row.key, row)
    persistRows()
    const stateRow = membersStateRow(row)
    for (const listener of mutationListeners) listener([stateRow])
    return row
  }
  const nextUpdated = (spaceID: string) => Math.max(now(), (rows.get(spaceID)?.updated ?? 0) + 1)

  return {
    rows: () => [...rows.values()].map(membersStateRow),
    row: (spaceID) => rows.get(spaceID),
    merge(incoming) {
      let changed = false
      for (const raw of incoming) {
        const row = parseMembersRow(raw)
        if (!row) continue
        const current = rows.get(row.key)
        if (current && compareStateVersions(row.updated, row.writeID, current.updated, current.writeID) <= 0) continue
        rows.set(row.key, row)
        changed = true
      }
      if (changed) persistRows()
    },
    local: (spaceID) => locals.get(spaceID) ?? {},
    setLocal(spaceID, local) {
      locals.set(spaceID, local)
      persistLocals()
    },
    publish: (spaceID, members: SpaceMember[]) => {
      const seen = new Set<string>()
      const unique = members.filter((member) => !seen.has(memberID(member)) && Boolean(seen.add(memberID(member))))
      return write({ key: spaceID, members: unique, updated: nextUpdated(spaceID), writeID: randomID(), deleted: false })
    },
    remove: (spaceID) => write({ key: spaceID, members: [], updated: nextUpdated(spaceID), writeID: randomID(), deleted: true }),
    subscribeMutations(listener) {
      mutationListeners.add(listener)
      return () => { mutationListeners.delete(listener) }
    },
  }
}

function browserStorage(): StorageLike | null {
  try { return window.localStorage } catch { return null }
}
