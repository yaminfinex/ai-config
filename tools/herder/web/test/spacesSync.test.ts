import assert from 'node:assert/strict'
import test from 'node:test'

import {
  createServerSpaceLookup,
  createSpacesSync,
  createSpacesSyncPersistence,
  serverSpaceLookupMessage,
  spacesSyncMessages,
  type GenericStateRow,
  type SpacesSyncPersistence,
  type SpacesSyncStore,
  type StateTransport,
} from '../src/features/spaces/spacesSync.ts'

class MemoryPersistence implements SpacesSyncPersistence {
  cursor = 0
  queue: GenericStateRow[] = []
  readCursor() { return this.cursor }
  writeCursor(cursor: number) { this.cursor = cursor }
  readQueue() { return structuredClone(this.queue) }
  writeQueue(rows: GenericStateRow[]) { this.queue = structuredClone(rows) }
}

class MemoryStore implements SpacesSyncStore {
  records = new Map<string, GenericStateRow>()
  mutationListener: ((rows: GenericStateRow[]) => void) | undefined
  constructor(rows: GenericStateRow[] = []) { this.merge(rows) }
  all() { return [...this.records.values()] }
  merge(rows: GenericStateRow[]) {
    for (const row of rows) {
      const current = this.records.get(row.key)
      if (!current || row.updated > current.updated || row.updated === current.updated && row.writeID > current.writeID) this.records.set(row.key, row)
    }
  }
  liveIDs() { return [...this.records.values()].flatMap((row) => row.deleted ? [] : [row.key]) }
  subscribeMutations(listener: (rows: GenericStateRow[]) => void) { this.mutationListener = listener; return () => { this.mutationListener = undefined } }
  mutate(rows: GenericStateRow[]) { this.merge(rows); this.mutationListener?.(rows) }
}

function definition(key: string, updated: number, writeID: string, deleted = false): GenericStateRow {
  return { key, value: { id: key, name: key, order: 0, created: 0 }, updated, writeID, deleted }
}

function stateRefusal(status: number, error: string, detail: string) {
  const problem = { error, detail }
  return Object.assign(new Error(detail), {
    response: new Response(JSON.stringify(problem), { status, headers: { 'Content-Type': 'application/json' } }),
    problem,
  })
}

test('boot offline keeps local paint available, persists one outbound row, and pushes it once on reconnect', async () => {
  const local = definition('main', 1, 'device-a')
  const store = new MemoryStore([local])
  const persistence = new MemoryPersistence()
  let online = false
  const posts: GenericStateRow[][] = []
  const transport: StateTransport = {
    since: async () => { if (!online) throw new Error('offline'); return { rows: [], rev: 0 } },
    upsert: async (rows) => {
      if (!online) throw new Error('offline')
      posts.push(rows)
      return { accepted: rows.map((row) => row.key), rev: 1 }
    },
  }
  const sync = createSpacesSync({ store, persistence, transport, retry: () => undefined })
  const boot = sync.start()
  assert.deepEqual(store.liveIDs(), ['main'], 'local state is available before the first network await settles')
  await boot
  assert.deepEqual(persistence.queue.map((row) => row.key), ['main'])

  sync.dispose()
  online = true
  const restarted = createSpacesSync({ store, persistence, transport, retry: () => undefined })
  await restarted.start()
  assert.equal(posts.length, 1)
  assert.deepEqual(posts[0].map((row) => row.key), ['main'])
  assert.deepEqual(persistence.queue, [])
  await restarted.retryNow()
  assert.equal(posts.length, 1, 'an accepted idempotent row is not posted twice')
})

test('the outbound queue is persisted before the first network attempt', async () => {
  const calls: string[] = []
  const persistence = new MemoryPersistence()
  const originalWrite = persistence.writeQueue.bind(persistence)
  persistence.writeQueue = (rows) => { calls.push(`persist:${rows.map((row) => row.key).join(',')}`); originalWrite(rows) }
  const sync = createSpacesSync({
    store: new MemoryStore([definition('main', 1, 'local')]),
    persistence,
    transport: {
      since: async () => { calls.push('get'); return { rows: [], rev: 0 } },
      upsert: async (rows) => { calls.push(`post:${rows.map((row) => row.key).join(',')}`); return { accepted: ['main'], rev: 1 } },
    },
    retry: () => undefined,
  })
  await sync.start()
  assert.ok(calls.indexOf('persist:main') < calls.indexOf('get'))
  assert.ok(calls.indexOf('persist:main') < calls.indexOf('post:main'))
})

test('a local mutation persists first and then posts without waiting for reconnect', async () => {
  const calls: string[] = []
  const store = new MemoryStore()
  const persistence = new MemoryPersistence()
  const originalWrite = persistence.writeQueue.bind(persistence)
  persistence.writeQueue = (rows) => { calls.push(`persist:${rows.map((row) => row.key).join(',')}`); originalWrite(rows) }
  const sync = createSpacesSync({
    store,
    persistence,
    transport: {
      since: async () => ({ rows: [], rev: 0 }),
      upsert: async (rows) => { calls.push(`post:${rows.map((row) => row.key).join(',')}`); return { accepted: rows.map((row) => row.key), rev: 1 } },
    },
  })
  await sync.start()
  calls.length = 0
  store.mutate([definition('review', 2, 'local-edit')])
  await new Promise((resolve) => setTimeout(resolve, 0))
  assert.deepEqual(calls.slice(0, 2), ['persist:review', 'post:review'])
  assert.deepEqual(persistence.queue, [])
})

test('a server tombstone older than local retention still deletes through the since cursor', async () => {
  const store = new MemoryStore([definition('closed-elsewhere', 10, 'stale-live')])
  const persistence = new MemoryPersistence()
  persistence.cursor = 40
  const transport: StateTransport = {
    since: async (rev) => {
      assert.equal(rev, 40)
      return { rows: [definition('closed-elsewhere', 20, 'server-close', true)], rev: 41 }
    },
    upsert: async () => ({ accepted: [], rev: 41 }),
  }
  const sync = createSpacesSync({ store, persistence, transport, retry: () => undefined })
  await sync.start()
  assert.deepEqual(store.liveIDs(), [])
  assert.equal(persistence.cursor, 41)
})

test('a URL naming a server-only space stays pending through first pull, then shows it', () => {
  const switches: string[] = []
  const lookup = createServerSpaceLookup('server-only', {
    hasLocal: () => false,
    switchTo: (id) => { switches.push(id) },
    fallback: () => { switches.push('fallback') },
    scheduleTimeout: () => 1,
    cancelTimeout: () => undefined,
  })
  assert.equal(serverSpaceLookupMessage, 'Looking up space…')
  assert.equal(lookup.status(), 'pending')
  lookup.firstPullCompleted(['server-only'])
  assert.equal(lookup.status(), 'shown')
  assert.deepEqual(switches, ['server-only'])
})

test('an unknown URL falls back after the bounded first pull and a later delivery never auto-switches', () => {
  const switches: string[] = []
  let timeout = () => undefined
  const lookup = createServerSpaceLookup('late-space', {
    hasLocal: () => false,
    switchTo: (id) => { switches.push(id) },
    fallback: () => { switches.push('fallback') },
    scheduleTimeout: (callback, delay) => { assert.equal(delay, 2_000); timeout = callback; return 1 },
    cancelTimeout: () => undefined,
  })
  timeout()
  assert.equal(lookup.status(), 'fallback')
  lookup.rowsArrived(['late-space'])
  assert.deepEqual(switches, ['fallback'])
})

test('attribution-required state access stays local-only without scheduling futile retries', async () => {
  const store = new MemoryStore([definition('main', 1, 'local')])
  const persistence = new MemoryPersistence()
  const problems: string[] = []
  let retries = 0
  let requests = 0
  const refused = stateRefusal(409, 'attribution required', 'viewer identity is unavailable on loopback')
  const transport: StateTransport = {
    since: async () => { requests++; throw refused },
    upsert: async () => { requests++; throw refused },
  }
  const sync = createSpacesSync({
    store,
    persistence,
    transport,
    retry: () => { retries++; return retries },
    onProblem: (problem) => problems.push(problem),
  })
  await sync.start()
  assert.deepEqual(persistence.queue.map((row) => row.key), ['main'])
  assert.equal(retries, 0)
  assert.equal(requests, 1)
  assert.match(problems.at(-1) ?? '', /saved in this browser only/i)
  assert.match(problems.at(-1) ?? '', /Connect via Tailscale/i)
  assert.doesNotMatch(problems.at(-1) ?? '', /will retry/i)
})

test('the first later attributed pull resumes and drains the local-only queue', async () => {
  const store = new MemoryStore([definition('main', 1, 'local')])
  const persistence = new MemoryPersistence()
  let attributed = false
  let posts = 0
  const sync = createSpacesSync({
    store,
    persistence,
    transport: {
      since: async () => {
        if (!attributed) throw stateRefusal(409, 'attribution required', 'viewer identity is unavailable on loopback')
        return { rows: [], rev: 1 }
      },
      upsert: async (rows) => { posts++; return { accepted: rows.map(({ key }) => key), rev: 2 } },
    },
    retry: () => { throw new Error('structural refusal must not schedule a retry') },
  })
  await sync.start()
  attributed = true
  await sync.stateChanged('spaces', 1)
  assert.equal(posts, 1)
  assert.deepEqual(persistence.queue, [])
})

test('413 retains the queue and reason without retrying until the next local mutation', async () => {
  const store = new MemoryStore([definition('main', 1, 'local')])
  const persistence = new MemoryPersistence()
  const problems: string[] = []
  let retries = 0
  let posts = 0
  const sync = createSpacesSync({
    store,
    persistence,
    transport: {
      since: async () => ({ rows: [], rev: 0 }),
      upsert: async () => { posts++; throw stateRefusal(413, 'state refused', '10,000 row limit reached') },
    },
    retry: () => { retries++; return retries },
    onProblem: (problem) => problems.push(problem),
  })
  await sync.start()
  assert.equal(posts, 1)
  assert.equal(retries, 0)
  assert.match(problems.at(-1) ?? '', /10,000 row limit reached/i)
  assert.doesNotMatch(problems.at(-1) ?? '', /will retry/i)
  await sync.retryNow()
  assert.equal(posts, 1)
  store.mutate([definition('review', 2, 'local-edit')])
  await new Promise((resolve) => setTimeout(resolve, 0))
  assert.equal(posts, 2)
  assert.deepEqual(persistence.queue.map((row) => row.key).sort(), ['main', 'review'])
})

for (const [label, refusal] of [
  ['offline', new Error('offline')],
  ['503', stateRefusal(503, 'state unavailable', 'state directory is unavailable')],
] as const) {
  test(`${label} failures retain the queue and schedule a transient retry`, async () => {
    const persistence = new MemoryPersistence()
    let retries = 0
    const problems: string[] = []
    const sync = createSpacesSync({
      store: new MemoryStore([definition('main', 1, 'local')]),
      persistence,
      transport: {
        since: async () => ({ rows: [], rev: 0 }),
        upsert: async () => { throw refusal },
      },
      retry: () => { retries++; return retries },
      onProblem: (problem) => problems.push(problem),
    })
    await sync.start()
    assert.equal(retries, 1)
    assert.deepEqual(persistence.queue.map(({ key }) => key), ['main'])
    assert.match(problems.at(-1) ?? '', /will retry automatically/i)
  })
}

test('a state change at or behind the cursor does not pull', async () => {
  const persistence = new MemoryPersistence()
  persistence.cursor = 7
  let pulls = 0
  const sync = createSpacesSync({
    store: new MemoryStore(),
    persistence,
    transport: {
      since: async () => { pulls++; return { rows: [], rev: 7 } },
      upsert: async () => ({ accepted: [], rev: 7 }),
    },
  })
  await sync.stateChanged('spaces', 7)
  await sync.stateChanged('spaces', 6)
  assert.equal(pulls, 0)
})

test('state-changed only nudges a pull for the spaces namespace and coalesces revisions', async () => {
  const store = new MemoryStore()
  const persistence = new MemoryPersistence()
  const pulls: number[] = []
  let release = () => undefined
  const transport: StateTransport = {
    since: async (rev) => {
      pulls.push(rev)
      await new Promise<void>((resolve) => { release = resolve })
      return { rows: [], rev: pulls.length }
    },
    upsert: async () => ({ accepted: [], rev: 0 }),
  }
  const sync = createSpacesSync({ store, persistence, transport, retry: () => undefined })
  const first = sync.stateChanged('spaces', 2)
  void sync.stateChanged('notes', 9)
  void sync.stateChanged('spaces', 3)
  assert.deepEqual(pulls, [0])
  release()
  await first
  await new Promise((resolve) => setTimeout(resolve, 0))
  assert.deepEqual(pulls, [0, 1])
  release()
})

test('spaces keeps byte-identical sync keys and owner-facing messages after generalization', () => {
  const values = new Map<string, string>()
  const persistence = createSpacesSyncPersistence({
    getItem: (key) => values.get(key) ?? null,
    setItem: (key, value) => { values.set(key, value) },
  })
  persistence.writeCursor(7)
  persistence.writeQueue([])
  assert.deepEqual([...values.keys()].sort(), [
    'herder.web.state.v1:spaces:queue',
    'herder.web.state.v1:spaces:rev',
  ])
  assert.equal(spacesSyncMessages.browserOnly, 'Spaces are saved in this browser only.')
  assert.equal(spacesSyncMessages.pending(1), 'Spaces are saved on this device, but 1 change could not sync. It will retry automatically.')
  assert.equal(spacesSyncMessages.pending(2), 'Spaces are saved on this device, but 2 changes could not sync. They will retry automatically.')
  assert.equal(spacesSyncMessages.queuePersistence, 'Spaces are saved on this device, but the pending sync queue could not be saved between browser sessions.')
  assert.equal(spacesSyncMessages.postRefused([], '10,000 row limit reached'), 'Spaces are saved on this device, but the server refused to sync them: 10,000 row limit reached')
  assert.equal(spacesSyncMessages.cursorPersistence, 'Spaces are saved on this device, but the sync cursor could not be saved between browser sessions.')
})
