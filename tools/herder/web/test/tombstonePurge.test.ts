import assert from 'node:assert/strict'
import test from 'node:test'

import { createNotesStore, notesStoragePrefix, type NotesStorage } from '../src/features/notes/notesStore.ts'
import { createNotesSync, createNotesSyncPersistence, notesStoreSyncAdapter } from '../src/features/notes/notesSync.ts'
import { createSpaceMembersStore, spaceMembersRowsKey } from '../src/features/spaces/spaceMembersStore.ts'
import { createSpaceMembersSync, spaceMembersStoreSyncAdapter } from '../src/features/spaces/spaceMembersSync.ts'
import { createSpacesStore } from '../src/features/spaces/spacesStore.ts'
import { createSpacesSync, spacesStoreSyncAdapter, storedSpaceToStateRow } from '../src/features/spaces/spacesSync.ts'
import { mainSpaceID, spaceRecordKey, spacesRecordPrefix } from '../src/features/spaces/spacesModel.ts'
import { createStateSyncPersistence, type GenericStateRow, type StateTransport } from '../src/shared/stateSync.ts'
import { compareStateVersions } from '../src/shared/stateVersion.ts'

class FakeStorage implements NotesStorage {
  readonly values = new Map<string, string>()
  get length() { return this.values.size }
  key(index: number) { return [...this.values.keys()][index] ?? null }
  getItem(key: string) { return this.values.get(key) ?? null }
  setItem(key: string, value: string) { this.values.set(key, value) }
  removeItem(key: string) { this.values.delete(key) }
}

const day = 24 * 60 * 60 * 1_000
const retention = 30 * day
const settle = () => new Promise((resolve) => setTimeout(resolve, 0))

// purgingServer answers like servecmd + webstate after tombstone-purge: a
// sweep drops tombstones older than the cutoff and raises the horizon to the
// newest one it dropped, a cursor below the sweep's floor gets every row, and
// a write creating a row the server lacks at or below the horizon is stale.
function purgingServer() {
  const rows = new Map<string, { row: GenericStateRow, rev: number }>()
  let rev = 0
  let floor = 0
  let horizon = 0
  const posted: string[] = []
  const transport: StateTransport = {
    since: async (since) => ({
      rows: [...rows.values()].filter((entry) => since < floor || entry.rev > since).sort((left, right) => left.rev - right.rev).map((entry) => entry.row),
      rev,
      ...horizon ? { horizon } : {},
    }),
    upsert: async (batch) => {
      const accepted: string[] = []
      const stale: string[] = []
      for (const row of batch) {
        posted.push(row.key)
        const current = rows.get(row.key)
        if (!current && horizon > 0 && row.updated <= horizon) { stale.push(row.key); continue }
        if (current && compareStateVersions(current.row.updated, current.row.writeID, row.updated, row.writeID) >= 0) continue
        rows.set(row.key, { row, rev: ++rev })
        accepted.push(row.key)
      }
      return { accepted, rev, ...stale.length ? { stale } : {} }
    },
  }
  const sweep = (cutoff: number) => {
    let removed = 0
    for (const [key, { row }] of rows) {
      if (!row.deleted || row.updated >= cutoff) continue
      rows.delete(key)
      horizon = Math.max(horizon, row.updated)
      removed++
    }
    if (removed > 0) floor = ++rev
    return removed
  }
  return { rows, posted, transport, sweep, horizon: () => horizon }
}

function notesDevice(storage: FakeStorage, now: () => number, prefix: string) {
  let sequence = 0
  return createNotesStore({ storage, events: null, now, randomID: () => `${prefix}-${++sequence}`, schedule: (callback) => { callback(); return undefined }, cancel: () => undefined })
}

function noteRecordKeys(storage: FakeStorage) {
  return [...storage.values.keys()].filter((key) => key.startsWith(`${notesStoragePrefix}record:`))
}

test('a stale browser holding a live copy of a deleted note does not bring it back once the tombstone is purged (reddens: it re-sent the purged note)', async () => {
  let clock = 1 * day
  const now = () => clock
  const server = purgingServer()
  const laptopStorage = new FakeStorage()
  const desktopStorage = new FakeStorage()

  // The laptop writes a note; the desktop pulls a live copy and goes quiet.
  const laptop = notesDevice(laptopStorage, now, 'laptop')
  const laptopSync = createNotesSync({ store: notesStoreSyncAdapter(laptop), persistence: createNotesSyncPersistence(laptopStorage), transport: server.transport })
  await laptopSync.start()
  const added = laptop.add({ group: 'general', text: 'remember the milk' })
  assert.ok(added.ok)
  await settle()
  const desktop = notesDevice(desktopStorage, now, 'desktop')
  const desktopSync = createNotesSync({ store: notesStoreSyncAdapter(desktop), persistence: createNotesSyncPersistence(desktopStorage), transport: server.transport })
  await desktopSync.start()
  assert.equal(desktop.list().length, 1)
  desktopSync.dispose()
  desktop.dispose()

  // The laptop deletes it; forty days on the server purges the tombstone.
  clock = 2 * day
  assert.ok(laptop.delete([added.value.id]).ok)
  await settle()
  clock = 42 * day
  assert.equal(server.sweep(clock - retention), 1)
  assert.equal(server.rows.size, 0)

  // The desktop reloads with its cached live copy and syncs.
  server.posted.length = 0
  const reloaded = notesDevice(desktopStorage, now, 'desktop')
  assert.equal(reloaded.list().length, 1, 'the cached copy loads before the first pull')
  const reloadedSync = createNotesSync({ store: notesStoreSyncAdapter(reloaded), persistence: createNotesSyncPersistence(desktopStorage), transport: server.transport })
  await reloadedSync.start()
  assert.deepEqual(reloaded.list(), [])
  assert.deepEqual(noteRecordKeys(desktopStorage), [], 'the purged note is gone from browser storage too')
  assert.deepEqual(server.posted, [], 'nothing was sent')
  assert.equal(server.rows.size, 0)
  assert.deepEqual(reloadedSync.pending(), [])
})

test('a stale note that reaches the server anyway is answered stale and dropped, not retried', async () => {
  let clock = 1 * day
  const now = () => clock
  const server = purgingServer()
  const storage = new FakeStorage()
  const store = notesDevice(storage, now, 'stale')
  const added = store.add({ group: 'general', text: 'cached long ago' })
  assert.ok(added.ok)
  // The server held and deleted this note; its tombstone is purged.
  await server.transport.upsert([{ key: added.value.id, value: { id: added.value.id }, updated: 2 * day, writeID: 'elsewhere', deleted: true }])
  clock = 40 * day
  server.sweep(clock - retention)
  server.posted.length = 0

  // The first pull fails, so start queues every local row and posts it.
  let pulls = 0
  const transport: StateTransport = {
    since: async (rev) => { if (pulls++ === 0) throw new Error('offline'); return server.transport.since(rev) },
    upsert: server.transport.upsert,
  }
  const sync = createNotesSync({ store: notesStoreSyncAdapter(store), persistence: createNotesSyncPersistence(storage), transport, retry: () => undefined })
  await sync.start()
  assert.deepEqual(server.posted, [added.value.id])
  assert.equal(server.rows.size, 0, 'the server did not store it')
  assert.deepEqual(store.list(), [])
  assert.deepEqual(noteRecordKeys(storage), [])
  assert.deepEqual(sync.pending(), [])
  await sync.retryNow()
  assert.deepEqual(server.posted, [added.value.id], 'it was not retried')
})

test('old incoming tombstones are not stored, and still delete an older local copy (reddens: every pull re-stored them)', async () => {
  let clock = 5 * day
  const now = () => clock
  const server = purgingServer()
  const storage = new FakeStorage()
  const store = notesDevice(storage, now, 'device')
  const kept = store.add({ group: 'general', text: 'deleted elsewhere long ago' })
  assert.ok(kept.ok)
  clock = 50 * day
  // The server has not swept yet: it holds a 40-day tombstone for the local
  // note (written after it), an unrelated 45-day one and a 10-day one.
  const tombstone = (key: string, age: number): GenericStateRow => ({ key, value: { id: key }, updated: clock - age, writeID: 'w', deleted: true })
  await server.transport.upsert([tombstone(kept.value.id, 40 * day), tombstone('old', 45 * day), tombstone('recent', 10 * day)])

  const sync = createNotesSync({ store: notesStoreSyncAdapter(store), persistence: createNotesSyncPersistence(storage), transport: server.transport })
  await sync.start()
  assert.deepEqual(store.records().map(({ record }) => record.id), ['recent'])
  assert.deepEqual(noteRecordKeys(storage), [`${notesStoragePrefix}record:recent`])
  assert.deepEqual(store.list(), [])

  // Spaces drop old closed spaces the same way.
  const spacesStorage = new FakeStorage()
  const spaces = createSpacesStore({ storage: spacesStorage, events: null, now, randomID: () => 'unused', schedule: (callback) => { callback(); return undefined }, cancel: () => undefined })
  const closed = (id: string, age: number) => storedSpaceToStateRow({ version: 1, writeID: 'w', record: { id, name: id, order: 0, created: 1, updated: clock - age, deleted: true } })
  const spaceServer = purgingServer()
  await spaceServer.transport.upsert([closed('old-space', 45 * day), closed('recent-space', 10 * day)])
  const spacesSync = createSpacesSync({ store: spacesStoreSyncAdapter(spaces), persistence: createStateSyncPersistence(new FakeStorage(), 'spaces'), transport: spaceServer.transport })
  await spacesSync.start()
  assert.deepEqual(spaces.recentlyClosed().map(({ id }) => id), ['recent-space'])
  assert.deepEqual([...spacesStorage.values.keys()].filter((key) => key.startsWith(spacesRecordPrefix)).map((key) => key.slice(spacesRecordPrefix.length)), ['recent-space'])

  // Space members too.
  const membersStorage = new FakeStorage()
  const members = createSpaceMembersStore({ storage: membersStorage, now, randomID: () => 'unused' })
  const membersServer = purgingServer()
  const removed = (key: string, age: number): GenericStateRow => ({ key, value: { members: [], updated: clock - age }, updated: clock - age, writeID: 'w', deleted: true })
  await membersServer.transport.upsert([removed('old-space', 45 * day), removed('recent-space', 10 * day)])
  const membersSync = createSpaceMembersSync({ store: spaceMembersStoreSyncAdapter(members), persistence: createStateSyncPersistence(new FakeStorage(), 'spaces.members'), transport: membersServer.transport })
  await membersSync.start()
  assert.deepEqual(members.rows().map(({ key }) => key), ['recent-space'])
  assert.deepEqual((JSON.parse(membersStorage.values.get(spaceMembersRowsKey) ?? '[]') as GenericStateRow[]).map(({ key }) => key), ['recent-space'])
})

test('add, edit and delete still sync between two browsers after a purge', async () => {
  let clock = 1 * day
  const now = () => clock
  const server = purgingServer()
  await server.transport.upsert([{ key: 'ancient', value: { id: 'ancient' }, updated: clock, writeID: 'w', deleted: true }])
  clock = 40 * day
  server.sweep(clock - retention)
  assert.ok(server.horizon() > 0)

  const aStorage = new FakeStorage()
  const bStorage = new FakeStorage()
  const a = notesDevice(aStorage, now, 'a')
  const b = notesDevice(bStorage, now, 'b')
  const aSync = createNotesSync({ store: notesStoreSyncAdapter(a), persistence: createNotesSyncPersistence(aStorage), transport: server.transport })
  const bSync = createNotesSync({ store: notesStoreSyncAdapter(b), persistence: createNotesSyncPersistence(bStorage), transport: server.transport })
  await aSync.start()
  await bSync.start()

  const added = a.add({ group: 'general', text: 'first draft' })
  assert.ok(added.ok)
  await settle()
  await bSync.retryNow()
  assert.deepEqual(b.list().map(({ text }) => text), ['first draft'])

  clock += 1_000
  assert.ok(b.edit(added.value.id, { text: 'second draft' }).ok)
  await settle()
  await aSync.retryNow()
  assert.deepEqual(a.list().map(({ text }) => text), ['second draft'])

  clock += 1_000
  assert.ok(a.delete([added.value.id]).ok)
  await settle()
  await bSync.retryNow()
  assert.deepEqual(b.list(), [])
  assert.equal(server.rows.get(added.value.id)?.row.deleted, true)
  assert.deepEqual(aSync.pending(), [])
  assert.deepEqual(bSync.pending(), [])
})

test('measure: ~700 tombstones across 60 days — one sweep keeps the <30-day ones and a reload holds no more than the server', async () => {
  const clock = 100 * day
  const now = () => clock
  const server = purgingServer()
  const storage = new FakeStorage()
  const total = 700
  const step = Math.floor(60 * day / total)
  const tombstones: GenericStateRow[] = Array.from({ length: total }, (_, index) => {
    const key = `00000000-0000-4000-8000-${String(index).padStart(12, '0')}`
    return { key, value: { id: key }, updated: clock - index * step, writeID: `write-${index}`, deleted: true }
  })
  await server.transport.upsert(tombstones)
  // The browser cached every one of them before this build.
  for (const row of tombstones) {
    storage.setItem(`${notesStoragePrefix}record:${row.key}`, JSON.stringify({ version: 1, writeID: row.writeID, record: { id: row.key, deleted: true, updated: row.updated } }))
  }
  const bytes = () => [...storage.values].filter(([key]) => key.startsWith(notesStoragePrefix)).reduce((sum, [key, value]) => sum + key.length + value.length, 0)
  const before = { server: server.rows.size, browser: noteRecordKeys(storage).length, bytes: bytes() }

  server.sweep(clock - retention)
  const young = tombstones.filter((row) => clock - row.updated <= retention).length
  assert.equal(server.rows.size, young)
  assert.ok([...server.rows.values()].every(({ row }) => clock - row.updated <= retention))

  const store = notesDevice(storage, now, 'reload')
  const sync = createNotesSync({ store: notesStoreSyncAdapter(store), persistence: createNotesSyncPersistence(storage), transport: server.transport })
  await sync.start()
  const after = { server: server.rows.size, browser: noteRecordKeys(storage).length, bytes: bytes() }
  assert.ok(after.browser <= after.server, `browser holds ${after.browser}, server ${after.server}`)
  assert.equal(store.records().length, after.browser)
  assert.deepEqual(sync.pending(), [])
  console.log(`tombstones: server ${before.server} -> ${after.server}, browser ${before.browser} -> ${after.browser} (${before.bytes} -> ${after.bytes} bytes)`)
})

test('with no horizon a first pull keeps and sends the seeded main space written at updated 0', async () => {
  const server = purgingServer()
  const storage = new FakeStorage()
  // As the v1 migration writes it.
  storage.setItem(spaceRecordKey(mainSpaceID), JSON.stringify({ version: 1, writeID: 'migration-v1', record: { id: mainSpaceID, name: 'main', order: 0, created: 0, updated: 0 } }))
  const spaces = createSpacesStore({ storage, events: null, now: () => 40 * day, randomID: () => 'unused', schedule: (callback) => { callback(); return undefined }, cancel: () => undefined })
  const sync = createSpacesSync({ store: spacesStoreSyncAdapter(spaces), persistence: createStateSyncPersistence(new FakeStorage(), 'spaces'), transport: server.transport })
  await sync.start()
  assert.deepEqual(spaces.list().map(({ id }) => id), [mainSpaceID])
  assert.deepEqual([...server.rows.keys()], [mainSpaceID])
})
