import assert from 'node:assert/strict'
import test from 'node:test'

import {
  browserOnlyNotesMessage,
  createNotesSync,
  createNotesSyncPersistence,
  notesStoreSyncAdapter,
  stateRowToStoredNote,
  storedNoteToStateRow,
} from '../src/features/notes/notesSync.ts'
import type { StoredNoteRecord } from '../src/features/notes/notesStore.ts'
import { createSpacesSyncPersistence } from '../src/features/spaces/spacesSync.ts'
import type { GenericStateRow, StateSyncPersistence, StateSyncStore, StateTransport } from '../src/shared/stateSync.ts'

class MemoryStorage {
  values = new Map<string, string>()
  getItem(key: string) { return this.values.get(key) ?? null }
  setItem(key: string, value: string) { this.values.set(key, value) }
}

class MemoryPersistence implements StateSyncPersistence {
  cursor = 0
  queue: GenericStateRow[] = []
  readCursor() { return this.cursor }
  writeCursor(cursor: number) { this.cursor = cursor }
  readQueue() { return structuredClone(this.queue) }
  writeQueue(rows: GenericStateRow[]) { this.queue = structuredClone(rows) }
}

class MemoryStore implements StateSyncStore {
  rows = new Map<string, GenericStateRow>()
  mutationListener: ((rows: GenericStateRow[]) => void) | undefined
  constructor(rows: GenericStateRow[] = []) { this.merge(rows) }
  all() { return [...this.rows.values()] }
  merge(rows: GenericStateRow[]) { for (const row of rows) this.rows.set(row.key, row) }
  liveIDs() { return [...this.rows.values()].flatMap((row) => row.deleted ? [] : [row.key]) }
  subscribeMutations(listener: (rows: GenericStateRow[]) => void) { this.mutationListener = listener; return () => { this.mutationListener = undefined } }
}

function refusal(status: number, error: string, detail: string) {
  const problem = { error, detail }
  return Object.assign(new Error(detail), {
    response: new Response(JSON.stringify(problem), { status, headers: { 'Content-Type': 'application/json' } }),
    problem,
  })
}

const fixtures: StoredNoteRecord[] = [
  {
    version: 1,
    writeID: 'legacy-write',
    record: { id: 'legacy', group: 'general', text: 'quoted text\n\ncomment', source: { kind: 'transcript', agent: 'kilo' }, created: 1, updated: 2 },
  },
  {
    version: 1,
    writeID: 'quoted-write',
    record: { id: 'quoted', group: 'general', text: 'comment', quote: 'selected text', created: 3, updated: 4 },
  },
  {
    version: 1,
    writeID: 'sourced-write',
    record: { id: 'sourced', group: 'kilo', text: '', quote: 'const answer = 42', source: { kind: 'file', path: 'src/a.ts', start: 7, end: 7 }, created: 5, updated: 6 },
  },
  { version: 1, writeID: 'delete-write', record: { id: 'gone', deleted: true, updated: 7 } },
]

test('stored note rows round-trip byte-identically without inventing optional fields', () => {
  for (const fixture of fixtures) {
    const roundTrip = stateRowToStoredNote(storedNoteToStateRow(fixture))
    assert.deepEqual(roundTrip, fixture)
    assert.equal(JSON.stringify(roundTrip), JSON.stringify(fixture))
  }
  const legacy = stateRowToStoredNote(storedNoteToStateRow(fixtures[0]))
  assert.equal(legacy && 'quote' in legacy.record, false)
})

test('row validation rejects malformed active notes and mismatched keys', () => {
  const valid = storedNoteToStateRow(fixtures[1])
  assert.equal(stateRowToStoredNote({ ...valid, key: 'elsewhere' }), null)
  assert.equal(stateRowToStoredNote({ ...valid, value: { id: valid.key, group: 4 } }), null)
  assert.equal(stateRowToStoredNote({ ...valid, value: { ...(valid.value as object), source: { kind: 'file' } } }), null)
})

test('notes cursor and queue keys are disjoint from the byte-identical spaces keys', () => {
  const storage = new MemoryStorage()
  const notes = createNotesSyncPersistence(storage)
  const spaces = createSpacesSyncPersistence(storage)
  notes.writeCursor(3)
  notes.writeQueue([storedNoteToStateRow(fixtures[1])])
  spaces.writeCursor(4)
  spaces.writeQueue([])
  assert.deepEqual([...storage.values.keys()].sort(), [
    'herder.web.state.v1:notes:queue',
    'herder.web.state.v1:notes:rev',
    'herder.web.state.v1:spaces:queue',
    'herder.web.state.v1:spaces:rev',
  ])
})

test('notes stateChanged ignores other namespaces and pulls notes revisions', async () => {
  let pulls = 0
  const sync = createNotesSync({
    store: new MemoryStore(),
    persistence: new MemoryPersistence(),
    transport: {
      since: async () => { pulls++; return { rows: [], rev: pulls } },
      upsert: async () => ({ accepted: [], rev: pulls }),
    },
  })
  await sync.stateChanged('spaces', 2)
  assert.equal(pulls, 0)
  await sync.stateChanged('notes', 2)
  assert.equal(pulls, 1)
})

test('notes attribution refusal is local-only and schedules no retry', async () => {
  const problems: string[] = []
  let retries = 0
  const sync = createNotesSync({
    store: new MemoryStore([storedNoteToStateRow(fixtures[1])]),
    persistence: new MemoryPersistence(),
    transport: {
      since: async () => { throw refusal(409, 'attribution required', 'viewer identity is unavailable on loopback') },
      upsert: async () => ({ accepted: [], rev: 0 }),
    },
    retry: () => { retries++; return retries },
    onProblem: (problem) => problems.push(problem),
  })
  await sync.start()
  assert.equal(retries, 0)
  assert.ok(problems.at(-1)?.startsWith(browserOnlyNotesMessage))
  assert.match(problems.at(-1) ?? '', /Connect via Tailscale/i)
})

test('notes 413 refusal names the note and holds sends until a local mutation', async () => {
  const row = storedNoteToStateRow(fixtures[1])
  const store = new MemoryStore([row])
  const problems: string[] = []
  let posts = 0
  const transport: StateTransport = {
    since: async () => ({ rows: [], rev: 0 }),
    upsert: async () => { posts++; throw refusal(413, 'state refused', 'row too large') },
  }
  const sync = createNotesSync({ store, persistence: new MemoryPersistence(), transport, onProblem: (problem) => problems.push(problem) })
  await sync.start()
  assert.equal(posts, 1)
  assert.equal(problems.at(-1), "Note 'comment' is too large to sync; it stays on this device.")
  await sync.retryNow()
  assert.equal(posts, 1)
  store.mutationListener?.([{ ...row, updated: 10, writeID: 'new-write' }])
  await new Promise((resolve) => setTimeout(resolve, 0))
  assert.equal(posts, 2)
})

test('notes adapter exposes all records and keeps pulled merges mutation-silent', () => {
  const merged: StoredNoteRecord[][] = []
  const store = {
    records: () => fixtures,
    merge: (records: StoredNoteRecord[]) => { merged.push(records) },
    subscribeMutations: () => () => undefined,
    list: () => fixtures.flatMap(({ record }) => 'deleted' in record ? [] : [record]),
  }
  const adapter = notesStoreSyncAdapter(store)
  assert.equal(adapter.all().length, fixtures.length)
  adapter.merge(adapter.all())
  assert.deepEqual(merged[0], fixtures)
})
