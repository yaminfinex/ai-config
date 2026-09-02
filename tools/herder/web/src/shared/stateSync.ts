import { apiProblem, viewerReadOnlyMessage, type StateRow } from '../api/client.ts'

export type GenericStateRow = StateRow

export type StateSyncStore = {
  all: () => GenericStateRow[]
  merge: (rows: GenericStateRow[]) => void
  liveIDs: () => string[]
  subscribeMutations?: (listener: (rows: GenericStateRow[]) => void) => () => void
}

export type StateSyncPersistence = {
  readCursor: () => number
  writeCursor: (cursor: number) => void
  readQueue: () => GenericStateRow[]
  writeQueue: (rows: GenericStateRow[]) => void
}

export type StateTransport = {
  since: (rev: number) => Promise<{ rows: GenericStateRow[], rev: number }>
  upsert: (rows: GenericStateRow[]) => Promise<{ accepted: string[], rev: number }>
}

export type StateSyncMessages = {
  browserOnly: string
  pending: (count: number) => string
  queuePersistence: string
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
  retry?: (callback: () => void, delay: number) => unknown
  cancelRetry?: (handle: unknown) => void
  onProblem?: (problem: string) => void
  onRows?: (rows: GenericStateRow[]) => void
}

function validStateRow(value: unknown): value is GenericStateRow {
  if (!value || typeof value !== 'object') return false
  const row = value as Partial<GenericStateRow>
  return typeof row.key === 'string' && row.key.length > 0 && typeof row.updated === 'number' && Number.isFinite(row.updated) &&
    typeof row.writeID === 'string' && row.writeID.length > 0 && typeof row.deleted === 'boolean' && 'value' in row
}

export function createStateSync(options: StateSyncOptions) {
  const coalesce = (rows: GenericStateRow[]) => {
    const order: string[] = []
    const winners = new Map<string, GenericStateRow>()
    for (const row of rows) {
      const current = winners.get(row.key)
      if (!current) order.push(row.key)
      if (!current || options.compare(row, current) > 0) winners.set(row.key, row)
    }
    return order.flatMap((key) => {
      const row = winners.get(key)
      return row ? [row] : []
    })
  }

  let queue = coalesce(options.persistence.readQueue())
  let cursor = options.persistence.readCursor()
  let disposed = false
  let retryHandle: unknown
  let backoff = 500
  let pullInFlight: Promise<void> | null = null
  let pullAgain = false
  let attributionBlocked = false
  let postRefusedUntilMutation = false

  const persistQueue = (next: GenericStateRow[]) => {
    queue = coalesce(next)
    try { options.persistence.writeQueue(queue) } catch {
      options.onProblem?.(options.messages.queuePersistence)
    }
  }
  const enqueue = (rows: GenericStateRow[]) => {
    // Persist before any caller can attempt I/O, so a crash cannot lose the
    // mutation between the browser store write and the network request.
    persistQueue([...queue, ...rows])
  }
  const showPending = () => {
    if (queue.length > 0) options.onProblem?.(options.messages.pending(queue.length))
  }
  const cancelScheduledRetry = () => {
    if (retryHandle !== undefined && options.cancelRetry) options.cancelRetry(retryHandle)
    retryHandle = undefined
  }
  const handleFailure = (error: unknown, attemptedRows: GenericStateRow[] = []) => {
    const { response, problem } = apiProblem(error)
    if (response?.status === 409 && problem.error === 'attribution required') {
      attributionBlocked = true
      cancelScheduledRetry()
      options.onProblem?.(`${options.messages.browserOnly} ${viewerReadOnlyMessage(problem, response.status)}`)
      return
    }
    if (response?.status === 413) {
      postRefusedUntilMutation = true
      cancelScheduledRetry()
      options.onProblem?.(options.messages.postRefused(attemptedRows.length ? attemptedRows : queue, problem.detail))
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
    const result = await options.transport.since(cursor)
    attributionBlocked = false
    options.store.merge(result.rows)
    options.onRows?.(result.rows)
    cursor = result.rev
    try { options.persistence.writeCursor(cursor) } catch {
      options.onProblem?.(options.messages.cursorPersistence)
    }
    if (result.rows.length > 0 && queue.length > 0) {
      const pulled = new Map(result.rows.map((row) => [row.key, row]))
      persistQueue(queue.filter((queued) => {
        const remote = pulled.get(queued.key)
        return !remote || options.compare(remote, queued) < 0
      }))
    }
    if (queue.length === 0) options.onProblem?.('')
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
  const sendQueue = async () => {
    if (queue.length === 0 || disposed || attributionBlocked || postRefusedUntilMutation) return
    const sent = [...queue]
    try {
      await options.transport.upsert(sent)
      const sentByKey = new Map(sent.map((row) => [row.key, row]))
      persistQueue(queue.filter((row) => {
        const posted = sentByKey.get(row.key)
        return !posted || options.compare(row, posted) > 0
      }))
      backoff = 500
      if (queue.length === 0) options.onProblem?.('')
      await requestPull()
    } catch (error) {
      handleFailure(error, sent)
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
    postRefusedUntilMutation = false
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
    readQueue: () => {
      try {
        const value = JSON.parse(storage.getItem(queueKey) ?? '[]') as unknown
        return Array.isArray(value) ? value.filter(validStateRow) : []
      } catch { return [] }
    },
    writeQueue: (rows) => { storage.setItem(queueKey, JSON.stringify(rows)) },
  }
}

export function resetStateSyncCursor(storage: Pick<Storage, 'setItem'>, namespace: string) {
  try { storage.setItem(`herder.web.state.v1:${namespace}:rev`, '0') } catch { /* local store reports persistence degradation */ }
}
