import { apiProblem, viewerReadOnlyMessage, type StateRow } from '../api/client.ts'

export type GenericStateRow = StateRow

export type StateSyncStore = {
  all: () => GenericStateRow[]
  // row reads one key's current row; a store without it is read whole, once
  // per send or pull, which suits only small stores.
  row?: (key: string) => GenericStateRow | undefined
  merge: (rows: GenericStateRow[]) => void
  liveIDs: () => string[]
  subscribeMutations?: (listener: (rows: GenericStateRow[]) => void) => () => void
}

// The persisted queue is the keys still to send. Rows are read from the store
// at send time, so a mutation to a queued key rewrites nothing.
export type StateSyncPersistence = {
  readCursor: () => number
  writeCursor: (cursor: number) => void
  readQueue: () => string[]
  writeQueue: (keys: string[]) => void
}

export type StateTransport = {
  since: (rev: number) => Promise<{ rows: GenericStateRow[], rev: number }>
  upsert: (rows: GenericStateRow[]) => Promise<{ accepted: string[], rev: number }>
}

export type StateSyncMessages = {
  browserOnly: string
  pending: (count: number) => string
  queuePersistence: string
  // postRefused names the one row the server refused; the rest still sync.
  postRefused: (rows: GenericStateRow[], detail: string) => string
  cursorPersistence: string
}

export type StateSyncOptions = {
  namespace: string
  compare: (left: GenericStateRow, right: GenericStateRow) => number
  messages: StateSyncMessages
  store: StateSyncStore
  persistence: StateSyncPersistence
  transport: StateTransport
  maxPostBytes?: number
  retry?: (callback: () => void, delay: number) => unknown
  cancelRetry?: (handle: unknown) => void
  onProblem?: (problem: string) => void
  onRows?: (rows: GenericStateRow[]) => void
}

// The server refuses a write body over 64 KiB; each POST stays well under it.
export const maxStatePostBytes = 48 * 1_024

function validStateRow(value: unknown): value is GenericStateRow {
  if (!value || typeof value !== 'object') return false
  const row = value as Partial<GenericStateRow>
  return typeof row.key === 'string' && row.key.length > 0 && typeof row.updated === 'number' && Number.isFinite(row.updated) &&
    typeof row.writeID === 'string' && row.writeID.length > 0 && typeof row.deleted === 'boolean' && 'value' in row
}

const encoder = new TextEncoder()

// chunkRows splits rows into POST bodies under maxBytes; a row too big for
// any chunk goes alone, so the server's answer names it alone.
export function chunkRows(rows: GenericStateRow[], maxBytes: number): GenericStateRow[][] {
  const envelope = encoder.encode('{"rows":[]}').byteLength
  const chunks: GenericStateRow[][] = []
  let chunk: GenericStateRow[] = []
  let bytes = envelope
  for (const row of rows) {
    const size = encoder.encode(JSON.stringify(row)).byteLength
    if (chunk.length && bytes + 1 + size > maxBytes) {
      chunks.push(chunk)
      chunk = []
      bytes = envelope
    }
    bytes += (chunk.length ? 1 : 0) + size
    chunk.push(row)
  }
  if (chunk.length) chunks.push(chunk)
  return chunks
}

export function createStateSync(options: StateSyncOptions) {
  const maxPostBytes = options.maxPostBytes ?? maxStatePostBytes
  const queue = new Set(options.persistence.readQueue())
  // Keys the server refused as too large, held back until they change.
  const held = new Set<string>()
  let refusedMessage = ''
  let cursor = options.persistence.readCursor()
  // Until one full pull lands, what the server holds is unknown: start's
  // first pull reads everything, so only the difference is sent.
  let catchUp = true
  let disposed = false
  let retryHandle: unknown
  let backoff = 500
  let pullInFlight: Promise<void> | null = null
  let pullAgain = false
  let sendInFlight: Promise<void> | null = null
  let sendAgain = false
  let attributionBlocked = false

  const reader = () => {
    if (options.store.row) return options.store.row
    const rows = new Map(options.store.all().map((row) => [row.key, row]))
    return (key: string) => rows.get(key)
  }
  // The queue is written only when its membership changes: editing a note
  // that is already waiting rewrites nothing.
  const writeQueue = () => {
    try { options.persistence.writeQueue([...queue]) } catch {
      options.onProblem?.(options.messages.queuePersistence)
    }
  }
  const enqueueKeys = (keys: string[]) => {
    const before = queue.size
    for (const key of keys) queue.add(key)
    if (queue.size !== before) writeQueue()
  }
  const dequeueKeys = (keys: string[]) => {
    let changed = false
    for (const key of keys) changed = queue.delete(key) || changed
    if (changed) writeQueue()
  }
  const waiting = () => [...queue].filter((key) => !held.has(key))
  const settled = () => {
    if (waiting().length > 0) return
    options.onProblem?.(held.size > 0 ? refusedMessage : '')
  }
  const showPending = () => {
    const count = waiting().length
    if (count > 0) options.onProblem?.(options.messages.pending(count))
  }
  const cancelScheduledRetry = () => {
    if (retryHandle !== undefined && options.cancelRetry) options.cancelRetry(retryHandle)
    retryHandle = undefined
  }
  const tooLarge = (error: unknown) => apiProblem(error).response?.status === 413
  const handleFailure = (error: unknown) => {
    const { response, problem } = apiProblem(error)
    if (response?.status === 409 && problem.error === 'attribution required') {
      attributionBlocked = true
      cancelScheduledRetry()
      options.onProblem?.(`${options.messages.browserOnly} ${viewerReadOnlyMessage(problem, response.status)}`)
      return
    }
    showPending()
    scheduleRetry()
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
    const full = catchUp
    const result = await options.transport.since(full ? 0 : cursor)
    attributionBlocked = false
    options.store.merge(result.rows)
    options.onRows?.(result.rows)
    cursor = result.rev
    try { options.persistence.writeCursor(cursor) } catch {
      options.onProblem?.(options.messages.cursorPersistence)
    }
    const pulled = new Map(result.rows.map((row) => [row.key, row]))
    const current = reader()
    if (pulled.size > 0 && queue.size > 0) {
      dequeueKeys([...queue].filter((key) => {
        const remote = pulled.get(key)
        const local = current(key)
        return !local || remote !== undefined && options.compare(remote, local) >= 0
      }))
    }
    if (full) {
      // A full pull lists every server row: queue only what it lacks or holds older.
      catchUp = false
      enqueueKeys(options.store.all().flatMap((row) => {
        const remote = pulled.get(row.key)
        return !remote || options.compare(row, remote) > 0 ? [row.key] : []
      }))
    }
    if (queue.size === 0) options.onProblem?.('')
  }
  const requestPull = () => {
    if (pullInFlight) {
      pullAgain = true
      return pullInFlight
    }
    pullInFlight = pullOnce().catch((error) => handleFailure(error)).finally(() => {
      pullInFlight = null
      if (pullAgain && !disposed) {
        pullAgain = false
        void requestPull()
      }
    })
    return pullInFlight
  }
  // post sends one chunk. A refused multi-row chunk is retried row by row,
  // so one oversized row is held back alone and the others still sync.
  const post = async (rows: GenericStateRow[]): Promise<boolean> => {
    try {
      await options.transport.upsert(rows)
    } catch (error) {
      if (!tooLarge(error)) {
        handleFailure(error)
        return false
      }
      if (rows.length > 1) {
        for (const row of rows) if (!await post([row])) return false
        return true
      }
      held.add(rows[0].key)
      refusedMessage = options.messages.postRefused(rows, apiProblem(error).problem.detail)
      options.onProblem?.(refusedMessage)
      return true
    }
    const current = reader()
    dequeueKeys(rows.flatMap((sent) => {
      const local = current(sent.key)
      return !local || options.compare(local, sent) <= 0 ? [sent.key] : []
    }))
    return true
  }
  const sendOnce = async () => {
    if (disposed || attributionBlocked) return
    const current = reader()
    const keys = waiting()
    const rows = keys.flatMap((key) => {
      const row = current(key)
      return row ? [row] : []
    })
    dequeueKeys(keys.filter((key) => !current(key)))
    if (rows.length === 0) return
    for (const chunk of chunkRows(rows, maxPostBytes)) {
      if (disposed || !await post(chunk)) return
    }
    backoff = 500
    settled()
    await requestPull()
  }
  const sendQueue = () => {
    if (sendInFlight) {
      sendAgain = true
      return sendInFlight
    }
    sendInFlight = sendOnce().finally(() => {
      sendInFlight = null
      if (sendAgain && !disposed) {
        sendAgain = false
        void sendQueue()
      }
    })
    return sendInFlight
  }
  async function retryNow() {
    if (disposed) return
    if (retryHandle !== undefined && options.cancelRetry) options.cancelRetry(retryHandle)
    retryHandle = undefined
    await requestPull()
    await sendQueue()
  }

  const unsubscribe = options.store.subscribeMutations?.((rows) => {
    // Enqueue only the mutated keys, persisted before any I/O so a crash
    // cannot lose the change between the store write and the request.
    for (const row of rows) held.delete(row.key)
    enqueueKeys(rows.map((row) => row.key))
    void sendQueue()
  })
  return {
    async start() {
      await requestPull()
      // Unreachable or refused: the server's rows are unknown, so every local
      // row waits; the first full pull that lands trims the queue to the difference.
      if (catchUp) enqueueKeys(options.store.all().map((row) => row.key))
      await sendQueue()
    },
    retryNow,
    async stateChanged(namespace: string, rev: number) {
      if (namespace !== options.namespace || rev <= cursor) return
      await requestPull()
      await sendQueue()
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

export function createStateSyncPersistence(storage: StorageLike, namespace: string): StateSyncPersistence {
  const cursorKey = `herder.web.state.v1:${namespace}:rev`
  const queueKey = `herder.web.state.v1:${namespace}:queue`
  return {
    readCursor: () => {
      try {
        const value = Number(storage.getItem(cursorKey) ?? '0')
        return Number.isSafeInteger(value) && value >= 0 ? value : 0
      } catch { return 0 }
    },
    writeCursor: (cursor) => { storage.setItem(cursorKey, String(cursor)) },
    // Earlier builds persisted whole rows; their keys carry over, and the
    // rows themselves are read from the store when sent.
    readQueue: () => {
      try {
        const value = JSON.parse(storage.getItem(queueKey) ?? '[]') as unknown
        if (!Array.isArray(value)) return []
        return value.flatMap((entry) => typeof entry === 'string' && entry ? [entry] : validStateRow(entry) ? [entry.key] : [])
      } catch { return [] }
    },
    writeQueue: (keys) => { storage.setItem(queueKey, JSON.stringify(keys)) },
  }
}

export function resetStateSyncCursor(storage: Pick<Storage, 'setItem'>, namespace: string) {
  try { storage.setItem(`herder.web.state.v1:${namespace}:rev`, '0') } catch { /* local store reports persistence degradation */ }
}
