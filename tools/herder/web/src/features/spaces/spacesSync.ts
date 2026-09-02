import { getState, upsertState } from '../../api/client.ts'
import {
  createStateSync,
  createStateSyncPersistence,
  resetStateSyncCursor,
  type GenericStateRow,
  type StateSyncMessages,
  type StateSyncOptions,
  type StateSyncPersistence,
  type StateSyncStore,
  type StateTransport,
} from '../../shared/stateSync.ts'
import { compareStateVersions } from '../../shared/stateVersion.ts'
import type { StoredSpaceRecord } from './spacesModel.ts'

export type { GenericStateRow, StateTransport }
export type SpacesSyncStore = StateSyncStore
export type SpacesSyncPersistence = StateSyncPersistence

export const browserOnlySpacesMessage = 'Spaces are saved in this browser only.'
export const spacesSyncMessages: StateSyncMessages = {
  browserOnly: browserOnlySpacesMessage,
  pending: (count) => `Spaces are saved on this device, but ${count} ${count === 1 ? 'change' : 'changes'} could not sync. ${count === 1 ? 'It' : 'They'} will retry automatically.`,
  queuePersistence: 'Spaces are saved on this device, but the pending sync queue could not be saved between browser sessions.',
  postRefused: (_rows, detail) => `Spaces are saved on this device, but the server refused to sync them: ${detail}`,
  cursorPersistence: 'Spaces are saved on this device, but the sync cursor could not be saved between browser sessions.',
}

type SpacesSyncOptions = Omit<StateSyncOptions, 'namespace' | 'compare' | 'messages'>

export function createSpacesSync(options: SpacesSyncOptions) {
  return createStateSync({
    ...options,
    namespace: 'spaces',
    compare: (left, right) => compareStateVersions(left.updated, left.writeID, right.updated, right.writeID),
    messages: spacesSyncMessages,
  })
}

type StorageLike = Pick<Storage, 'getItem' | 'setItem'>

export function createSpacesSyncPersistence(storage: StorageLike): SpacesSyncPersistence {
  return createStateSyncPersistence(storage, 'spaces')
}

export function resetSpacesSyncCursor(storage: Pick<Storage, 'setItem'>) {
  resetStateSyncCursor(storage, 'spaces')
}

export function storedSpaceToStateRow(stored: StoredSpaceRecord): GenericStateRow {
  const { updated, deleted = false, ...value } = stored.record
  return { key: stored.record.id, value, updated, writeID: stored.writeID, deleted }
}

export function stateRowToStoredSpace(row: GenericStateRow): StoredSpaceRecord | null {
  if (!row.value || typeof row.value !== 'object') return null
  const value = row.value as Partial<StoredSpaceRecord['record']>
  if (value.id !== row.key || typeof value.name !== 'string' || typeof value.order !== 'number' || !Number.isFinite(value.order) ||
    typeof value.created !== 'number' || !Number.isFinite(value.created) || typeof row.updated !== 'number' || !Number.isFinite(row.updated) ||
    typeof row.writeID !== 'string' || (row.deleted !== false && row.deleted !== true)) return null
  return {
    version: 1,
    writeID: row.writeID,
    record: { id: row.key, name: value.name, order: value.order, created: value.created, updated: row.updated, ...(row.deleted ? { deleted: true as const } : {}) },
  }
}

export function spacesStoreSyncAdapter(store: {
  records: () => StoredSpaceRecord[]
  merge: (records: StoredSpaceRecord[]) => void
  subscribeMutations: (listener: (records: StoredSpaceRecord[]) => void) => () => void
  list: () => Array<{ id: string }>
}): SpacesSyncStore {
  // Only definitions cross this boundary. Layouts and pane transfers remain
  // device-local, so a newly synced space honestly starts empty here.
  return {
    all: () => store.records().map(storedSpaceToStateRow),
    merge: (rows) => store.merge(rows.flatMap((row) => {
      const stored = stateRowToStoredSpace(row)
      return stored ? [stored] : []
    })),
    liveIDs: () => store.list().map(({ id }) => id),
    subscribeMutations: (listener) => store.subscribeMutations((records) => listener(records.map(storedSpaceToStateRow))),
  }
}

export function browserSpacesTransport(): StateTransport {
  return {
    since: async (rev) => {
      try { return await getState('spaces', rev) } catch (error) {
        if (error instanceof Error && 'response' in error && (error.response as Response | undefined)?.status === 404) return { rows: [], rev: 0 }
        throw error
      }
    },
    upsert: (rows) => upsertState('spaces', rows),
  }
}

export const serverSpaceLookupMessage = 'Looking up space…'

type LookupOptions = {
  hasLocal: (id: string) => boolean
  switchTo: (id: string) => void
  fallback: () => void
  scheduleTimeout: (callback: () => void, delay: number) => unknown
  cancelTimeout: (handle: unknown) => void
}

export function createServerSpaceLookup(id: string, options: LookupOptions) {
  let state: 'pending' | 'shown' | 'fallback' = options.hasLocal(id) ? 'shown' : 'pending'
  const timer = state === 'pending' ? options.scheduleTimeout(() => {
    if (state !== 'pending') return
    state = 'fallback'
    options.fallback()
  }, 2_000) : undefined
  const arrived = (ids: string[]) => {
    if (state !== 'pending' || !ids.includes(id)) return
    state = 'shown'
    if (timer !== undefined) options.cancelTimeout(timer)
    options.switchTo(id)
  }
  return {
    status: () => state,
    firstPullCompleted(ids: string[]) {
      if (state !== 'pending') return
      if (ids.includes(id)) arrived(ids)
      else {
        state = 'fallback'
        if (timer !== undefined) options.cancelTimeout(timer)
        options.fallback()
      }
    },
    rowsArrived: arrived,
    dispose() { if (timer !== undefined) options.cancelTimeout(timer) },
  }
}
