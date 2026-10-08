import assert from 'node:assert/strict'
import test from 'node:test'

import { createNotesStore, notesStoragePrefix, type NotesStorage } from '../src/features/notes/notesStore.ts'
import { createNotesSync, createNotesSyncPersistence, notesStoreSyncAdapter, storedNoteToStateRow } from '../src/features/notes/notesSync.ts'
import { createReadMarkerStore } from '../src/features/spaces/readMarkerStore.ts'
import { createReadMarkersSync, readMarkersNamespace, readMarkerStoreSyncAdapter } from '../src/features/spaces/readMarkerSync.ts'
import { createSpacesSync, spacesStoreSyncAdapter } from '../src/features/spaces/spacesSync.ts'
import { createSpacesStore } from '../src/features/spaces/spacesStore.ts'
import { compareStateVersions } from '../src/shared/stateVersion.ts'
import { chunkRows, createStateSyncPersistence, maxStatePostBytes, type GenericStateRow, type StateTransport } from '../src/shared/stateSync.ts'

class FakeStorage implements NotesStorage {
  readonly values = new Map<string, string>()
  writes: Array<{ key: string, bytes: number }> = []
  get length() { return this.values.size }
  key(index: number) { return [...this.values.keys()][index] ?? null }
  getItem(key: string) { return this.values.get(key) ?? null }
  setItem(key: string, value: string) { this.values.set(key, value); this.writes.push({ key, bytes: value.length }) }
  removeItem(key: string) { this.values.delete(key) }
}

const encoder = new TextEncoder()
const now = 10 * 24 * 60 * 60 * 1_000
const tombstoneID = (index: number) => `00000000-0000-4000-8000-${String(index).padStart(12, '0')}`
const liveID = (index: number) => `11111111-0000-4000-8000-${String(index).padStart(12, '0')}`

// The owner's shape, synthetic: 360 recent tombstones with long write IDs and
// 40 live notes of about 1.2 KB, about 190 KB in all.
function ownerShapedStorage() {
  const storage = new FakeStorage()
  for (let index = 0; index < 360; index++) {
    const id = tombstoneID(index)
    storage.values.set(`${notesStoragePrefix}record:${id}`, JSON.stringify({ version: 1, writeID: `write-${'w'.repeat(200)}-${index}`, record: { id, deleted: true, updated: now - 60_000 - index } }))
  }
  for (let index = 0; index < 40; index++) {
    const id = liveID(index)
    storage.values.set(`${notesStoragePrefix}record:${id}`, JSON.stringify({ version: 1, writeID: `write-live-${index}`, record: { id, group: index % 2 ? 'alpha' : 'bravo', text: `fixture note ${index} ${'x'.repeat(1_200)}`, created: 1_000 + index, updated: 1_000 + index } }))
  }
  return storage
}

function notesStore(storage: FakeStorage) {
  let sequence = 0
  return createNotesStore({ storage, events: null, now: () => now, randomID: () => `22222222-0000-4000-8000-${String(++sequence).padStart(12, '0')}`, schedule: (callback) => { callback(); return 0 }, cancel: () => undefined })
}

// fakeServer answers like servecmd + webstate: a body over 64 KiB is refused
// whole, a value over 64 KiB refuses the batch naming its key, and a row the
// server holds at the same or a newer version is skipped.
function fakeServer(seed: GenericStateRow[] = []) {
  const rows = new Map(seed.map((row) => [row.key, row]))
  const changes = new Map(seed.map((row, index) => [row.key, index + 1]))
  let rev = seed.length
  const posts: Array<{ keys: string[], bytes: number, status: number }> = []
  const refuse = (status: number, error: string, detail: string) => {
    const problem = { error, detail }
    return Object.assign(new Error(detail), { response: new Response(JSON.stringify(problem), { status }), problem })
  }
  const transport: StateTransport = {
    since: async (since) => ({ rows: [...rows.values()].filter((row) => (changes.get(row.key) ?? 0) > since), rev }),
    upsert: async (batch) => {
      const bytes = encoder.encode(JSON.stringify({ rows: batch })).byteLength
      const keys = batch.map((row) => row.key)
      if (bytes > 64 * 1_024) {
        posts.push({ keys, bytes, status: 413 })
        throw refuse(413, 'state batch too large', 'state write body is larger than 65536 bytes')
      }
      const oversized = batch.find((row) => encoder.encode(JSON.stringify(row.value)).byteLength > 64 * 1_024)
      if (oversized) {
        posts.push({ keys, bytes, status: 413 })
        throw refuse(413, 'state refused', `state row value is too large: key "${oversized.key}"`)
      }
      posts.push({ keys, bytes, status: 200 })
      const accepted: string[] = []
      for (const row of batch) {
        const current = rows.get(row.key)
        if (current && compareStateVersions(current.updated, current.writeID, row.updated, row.writeID) >= 0) continue
        rows.set(row.key, row)
        changes.set(row.key, ++rev)
        accepted.push(row.key)
      }
      return { accepted, rev }
    },
  }
  return { rows, posts, transport }
}

const settle = () => new Promise((resolve) => setTimeout(resolve, 0))

test('start pulls first and queues and posts only the rows the server lacks or holds older (reddens: persisted every local row first)', async () => {
  const storage = ownerShapedStorage()
  const store = notesStore(storage)
  // The server already holds every tombstone and 30 of the live notes; it
  // holds note 30 at an older version.
  const local = store.records().map(storedNoteToStateRow)
  const held = local.filter((row) => row.deleted || Number(row.key.slice(-12)) < 30)
  const stale = { ...local.find((row) => row.key === liveID(30))!, updated: 1, writeID: 'older' }
  const server = fakeServer([...held, stale])
  const persistence = createNotesSyncPersistence(storage)
  const sync = createNotesSync({ store: notesStoreSyncAdapter(store), persistence, transport: server.transport })
  storage.writes.length = 0
  await sync.start()
  const queueWrites = storage.writes.filter((write) => write.key === 'herder.web.state.v1:notes:queue')
  assert.ok(queueWrites.every((write) => write.bytes < 1_024), `start persisted a ${Math.max(...queueWrites.map((write) => write.bytes))}-byte queue`)
  const sent = server.posts.flatMap((post) => post.keys).sort()
  assert.deepEqual(sent, Array.from({ length: 10 }, (_, index) => liveID(30 + index)).sort())
  assert.ok(server.posts.every((post) => post.status === 200))
  assert.deepEqual(sync.pending(), [])
  assert.equal(storage.values.get('herder.web.state.v1:notes:queue'), '[]')
})

test('a first catch-up over 64 KiB goes in chunks under 48 KiB and lands every row (reddens: one 413 for the lot)', async () => {
  const storage = ownerShapedStorage()
  const store = notesStore(storage)
  const server = fakeServer()
  const problems: string[] = []
  const sync = createNotesSync({ store: notesStoreSyncAdapter(store), persistence: createNotesSyncPersistence(storage), transport: server.transport, onProblem: (problem) => problems.push(problem) })
  await sync.start()
  const total = server.posts.reduce((sum, post) => sum + post.bytes, 0)
  assert.ok(total > 64 * 1_024, `the catch-up must exceed the server's write cap (${total} bytes)`)
  assert.ok(server.posts.length > 1)
  for (const post of server.posts) {
    assert.equal(post.status, 200)
    assert.ok(post.bytes <= maxStatePostBytes, `a chunk posted ${post.bytes} bytes`)
  }
  assert.equal(server.rows.size, 400)
  assert.deepEqual(sync.pending(), [])
  assert.equal(problems.at(-1), '')

  // Adding and editing a note afterwards posts that one row each time.
  const before = server.posts.length
  const added = store.add({ group: 'alpha', text: 'a new note' })
  assert.equal(added.ok, true)
  await settle()
  if (!added.ok) return
  assert.equal(store.edit(added.value.id, { text: 'an edited note' }).ok, true)
  await settle()
  assert.deepEqual(server.posts.slice(before).map((post) => post.keys), [[added.value.id], [added.value.id]])
  assert.equal((server.rows.get(added.value.id)?.value as { text: string }).text, 'an edited note')
})

test('one row the server refuses as too large is held back and named while the rest sync (reddens: one refusal blocked every row)', async () => {
  const persistence = createStateSyncPersistence(new FakeStorage(), 'notes')
  const rows = new Map<string, GenericStateRow>()
  const add = (key: string, text: string) => rows.set(key, { key, value: { id: key, group: 'alpha', text, created: 1 }, updated: 2, writeID: `w-${key}`, deleted: false })
  add('first', 'an ordinary first note')
  add('huge', `enormous review notes ${'y'.repeat(70 * 1_024)}`)
  for (let index = 0; index < 20; index++) add(`after-${index}`, `ordinary note ${index}`)
  let listener: ((rows: GenericStateRow[]) => void) | undefined
  const server = fakeServer()
  const problems: string[] = []
  const sync = createNotesSync({
    store: {
      all: () => [...rows.values()],
      merge: () => undefined,
      liveIDs: () => [...rows.keys()],
      subscribeMutations: (next) => { listener = next; return () => undefined },
    },
    persistence,
    transport: server.transport,
    onProblem: (problem) => problems.push(problem),
  })
  await sync.start()
  assert.equal(server.rows.size, 21)
  assert.equal(server.rows.has('huge'), false)
  // The message names the refused note, not the first row of the batch.
  assert.match(problems.at(-1) ?? '', /^Note 'enormous review notes y+…' is too large to sync; it stays on this device\.$/)
  assert.deepEqual(sync.pending(), ['huge'])
  const posts = server.posts.length
  await sync.retryNow()
  assert.equal(server.posts.length, posts, 'a refused row is not resent until it changes')

  // Another note still syncs, and the refusal stays visible.
  add('later', 'written after the refusal')
  listener?.([rows.get('later')!])
  await settle()
  assert.equal(server.rows.has('later'), true)
  assert.match(problems.at(-1) ?? '', /enormous review notes/)

  // Shrinking the refused note sends it again, and it lands.
  add('huge', 'trimmed review notes')
  rows.set('huge', { ...rows.get('huge')!, updated: 3 })
  listener?.([rows.get('huge')!])
  await settle()
  assert.equal(server.rows.has('huge'), true)
  assert.deepEqual(sync.pending(), [])
  assert.equal(problems.at(-1), '')
})

test('the persisted queue holds keys, and an edit to a queued note rewrites nothing (reddens: the whole row queue per keystroke)', async () => {
  const storage = ownerShapedStorage()
  const store = notesStore(storage)
  let online = false
  const server = fakeServer()
  const transport: StateTransport = {
    since: async (rev) => { if (!online) throw new Error('offline'); return server.transport.since(rev) },
    upsert: async (rows) => { if (!online) throw new Error('offline'); return server.transport.upsert(rows) },
  }
  const sync = createNotesSync({ store: notesStoreSyncAdapter(store), persistence: createNotesSyncPersistence(storage), transport, retry: () => undefined })
  await sync.start()
  const queueKey = 'herder.web.state.v1:notes:queue'
  const queued = JSON.parse(storage.values.get(queueKey) ?? 'null') as unknown[]
  assert.equal(queued.length, 400)
  assert.ok(queued.every((entry) => typeof entry === 'string'))

  const id = liveID(3)
  storage.writes.length = 0
  for (const text of ['e', 'ed', 'edi', 'edit']) assert.equal(store.edit(id, { text }).ok, true)
  await settle()
  assert.deepEqual(storage.writes.filter((write) => write.key === queueKey), [], 'the key was already queued')

  online = true
  await sync.retryNow()
  assert.equal(server.rows.size, 400)
  assert.equal((server.rows.get(id)?.value as { text: string }).text, 'edit', 'the row is read from the store when sent')
  assert.equal(storage.values.get(queueKey), '[]')
})

test('a queue persisted as rows by an earlier build carries over as keys', () => {
  const storage = new FakeStorage()
  const row: GenericStateRow = { key: 'legacy', value: { id: 'legacy' }, updated: 1, writeID: 'w', deleted: true }
  storage.setItem('herder.web.state.v1:notes:queue', JSON.stringify([row, 'already-a-key', { bogus: true }]))
  assert.deepEqual(createNotesSyncPersistence(storage).readQueue(), ['legacy', 'already-a-key'])
})

test('chunkRows keeps every body under the limit and sends an oversized row alone', () => {
  const row = (key: string, size: number): GenericStateRow => ({ key, value: 'z'.repeat(size), updated: 1, writeID: 'w', deleted: false })
  const rows = [row('a', 20_000), row('b', 20_000), row('c', 20_000), row('big', 60_000), row('d', 10)]
  const chunks = chunkRows(rows, 48 * 1_024)
  assert.deepEqual(chunks.map((chunk) => chunk.map(({ key }) => key)), [['a', 'b'], ['c'], ['big'], ['d']])
  for (const chunk of chunks.filter((chunk) => chunk.length > 1)) {
    assert.ok(encoder.encode(JSON.stringify({ rows: chunk })).byteLength <= 48 * 1_024)
  }
})

test('spaces and read markers still sync through the chunked key queue', async () => {
  let sequence = 0
  const spaces = createSpacesStore({ storage: null, events: null, now: () => 100, randomID: () => `space-${++sequence}`, schedule: (callback) => { callback(); return 0 }, cancel: () => undefined })
  const spaceServer = fakeServer()
  const spacesSync = createSpacesSync({ store: spacesStoreSyncAdapter(spaces), persistence: createStateSyncPersistence(new FakeStorage(), 'spaces'), transport: spaceServer.transport })
  await spacesSync.start()
  const created = spaces.create()
  assert.equal(created.ok, true)
  await settle()
  assert.deepEqual([...spaceServer.rows.keys()].sort(), spaces.records().map((record) => record.record.id).sort())
  assert.ok(created.ok && spaceServer.rows.has(created.value.id))
  assert.deepEqual(spacesSync.pending(), [])

  const markers = createReadMarkerStore({ storage: new FakeStorage(), randomID: () => 'r1', now: () => 100 })
  const markerServer = fakeServer()
  const markersSync = createReadMarkersSync({ store: readMarkerStoreSyncAdapter(markers), persistence: createStateSyncPersistence(new FakeStorage(), readMarkersNamespace), transport: markerServer.transport })
  await markersSync.start()
  markers.apply({ mavu: { turn: 3, pos: null, at: 100, unread: false } })
  await settle()
  assert.equal(markerServer.rows.has('mavu'), true)
  assert.deepEqual(markersSync.pending(), [])
})
