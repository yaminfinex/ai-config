import { getState, upsertState, type StateRow } from '../../api/client.ts'
import { compareSpaceVersions } from './spacesStore.ts'
import type { StoredSpaceRecord } from './spacesModel.ts'

export type GenericStateRow = StateRow

export type SpacesSyncStore = {
  all: () => GenericStateRow[]
  merge: (rows: GenericStateRow[]) => void
  liveIDs: () => string[]
  subscribeMutations?: (listener: (rows: GenericStateRow[]) => void) => () => void
}

export type SpacesSyncPersistence = {
  readCursor: () => number
  writeCursor: (cursor: number) => void
  readQueue: () => GenericStateRow[]
  writeQueue: (rows: GenericStateRow[]) => void
}

export type StateTransport = {
  since: (rev: number) => Promise<{ rows: GenericStateRow[], rev: number }>
  upsert: (rows: GenericStateRow[]) => Promise<{ accepted: string[], rev: number }>
}

type SyncOptions = {
  store: SpacesSyncStore
  persistence: SpacesSyncPersistence
  transport: StateTransport
  retry?: (callback: () => void, delay: number) => unknown
  cancelRetry?: (handle: unknown) => void
  onProblem?: (problem: string) => void
  onRows?: (rows: GenericStateRow[]) => void
}

const cursorKey = 'herder.web.state.v1:spaces:rev'
const queueKey = 'herder.web.state.v1:spaces:queue'

function compareRows(left: GenericStateRow, right: GenericStateRow) {
  return compareSpaceVersions(left.updated, left.writeID, right.updated, right.writeID)
}

function validStateRow(value: unknown): value is GenericStateRow {
  if (!value || typeof value !== 'object') return false
  const row = value as Partial<GenericStateRow>
  return typeof row.key === 'string' && row.key.length > 0 && typeof row.updated === 'number' && Number.isFinite(row.updated) &&
    typeof row.writeID === 'string' && row.writeID.length > 0 && typeof row.deleted === 'boolean' && 'value' in row
}

function coalesce(rows: GenericStateRow[]) {
  const order: string[] = []
  const winners = new Map<string, GenericStateRow>()
  for (const row of rows) {
    const current = winners.get(row.key)
    if (!current) order.push(row.key)
    if (!current || compareRows(row, current) > 0) winners.set(row.key, row)
  }
  return order.flatMap((key) => {
    const row = winners.get(key)
    return row ? [row] : []
  })
}

function pendingMessage(count: number) {
  return `Spaces are saved on this device, but ${count} ${count === 1 ? 'change' : 'changes'} could not sync. ${count === 1 ? 'It' : 'They'} will retry automatically.`
}

export function createSpacesSync(options: SyncOptions) {
  let queue = coalesce(options.persistence.readQueue())
  let cursor = options.persistence.readCursor()
  let disposed = false
  let retryHandle: unknown
  let backoff = 500
  let pullInFlight: Promise<void> | null = null
  let pullAgain = false

  const persistQueue = (next: GenericStateRow[]) => {
    queue = coalesce(next)
    try { options.persistence.writeQueue(queue) } catch {
      options.onProblem?.('Spaces are saved on this device, but the pending sync queue could not be saved between browser sessions.')
    }
  }
  const enqueue = (rows: GenericStateRow[]) => {
    // The queue is persisted synchronously before any caller can attempt I/O.
    persistQueue([...queue, ...rows])
  }
  const showPending = () => {
    if (queue.length > 0) options.onProblem?.(pendingMessage(queue.length))
  }
  const scheduleRetry = () => {
    if (disposed || retryHandle !== undefined || !options.retry) return
    retryHandle = options.retry(() => {
      retryHandle = undefined
      void retryNow()
    }, backoff)
    backoff = Math.min(backoff * 2, 10_000)
  }
  const pullOnce = async () => {
    const result = await options.transport.since(cursor)
    options.store.merge(result.rows)
    options.onRows?.(result.rows)
    cursor = result.rev
    try { options.persistence.writeCursor(cursor) } catch {
      options.onProblem?.('Spaces are saved on this device, but the sync cursor could not be saved between browser sessions.')
    }
    if (result.rows.length > 0 && queue.length > 0) {
      const pulled = new Map(result.rows.map((row) => [row.key, row]))
      persistQueue(queue.filter((queued) => {
        const remote = pulled.get(queued.key)
        return !remote || compareRows(remote, queued) < 0
      }))
    }
    if (queue.length === 0) options.onProblem?.('')
  }
  const requestPull = () => {
    if (pullInFlight) {
      pullAgain = true
      return pullInFlight
    }
    pullInFlight = pullOnce().catch(() => {
      showPending()
      scheduleRetry()
    }).finally(() => {
      pullInFlight = null
      if (pullAgain && !disposed) {
        pullAgain = false
        void requestPull()
      }
    })
    return pullInFlight
  }
  const sendQueue = async () => {
    if (queue.length === 0 || disposed) return
    const sent = [...queue]
    try {
      await options.transport.upsert(sent)
      const sentByKey = new Map(sent.map((row) => [row.key, row]))
      persistQueue(queue.filter((row) => {
        const posted = sentByKey.get(row.key)
        return !posted || compareRows(row, posted) > 0
      }))
      backoff = 500
      if (queue.length === 0) options.onProblem?.('')
      await requestPull()
    } catch {
      // 409/413/offline all retain the durable queue and remain visible.
      showPending()
      scheduleRetry()
    }
  }
  async function retryNow() {
    if (disposed) return
    if (retryHandle !== undefined && options.cancelRetry) options.cancelRetry(retryHandle)
    retryHandle = undefined
    await requestPull()
    await sendQueue()
  }

  const unsubscribe = options.store.subscribeMutations?.((rows) => {
    enqueue(rows)
    void sendQueue()
  })
  return {
    async start() {
      enqueue(options.store.all())
      await requestPull()
      await sendQueue()
    },
    enqueue,
    retryNow,
    stateChanged(namespace: string, rev: number) {
      if (namespace !== 'spaces' || rev <= cursor) return Promise.resolve()
      return requestPull()
    },
    pending: () => [...queue],
    dispose() {
      disposed = true
      unsubscribe?.()
      if (retryHandle !== undefined && options.cancelRetry) options.cancelRetry(retryHandle)
      retryHandle = undefined
    },
  }
}

type StorageLike = Pick<Storage, 'getItem' | 'setItem'>

export function createSpacesSyncPersistence(storage: StorageLike): SpacesSyncPersistence {
  return {
    readCursor: () => {
      try {
        const value = Number(storage.getItem(cursorKey) ?? '0')
        return Number.isSafeInteger(value) && value >= 0 ? value : 0
      } catch { return 0 }
    },
    writeCursor: (cursor) => { storage.setItem(cursorKey, String(cursor)) },
    readQueue: () => {
      try {
        const value = JSON.parse(storage.getItem(queueKey) ?? '[]') as unknown
        return Array.isArray(value) ? value.filter(validStateRow) : []
      } catch { return [] }
    },
    writeQueue: (rows) => { storage.setItem(queueKey, JSON.stringify(rows)) },
  }
}

export function resetSpacesSyncCursor(storage: Pick<Storage, 'setItem'>) {
  try { storage.setItem(cursorKey, '0') } catch { /* local store reports persistence degradation */ }
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
