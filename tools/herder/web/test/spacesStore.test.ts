import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

import {
  compareStoredSpaceRecords,
  createSpacesStore,
  type SpacesStorage,
  type SpacesStoreEventTarget,
} from '../src/features/spaces/spacesStore.ts'
import { mainSpaceID, spaceRecordKey, type StoredSpaceRecord } from '../src/features/spaces/spacesModel.ts'

class FakeStorage implements SpacesStorage {
  readonly values = new Map<string, string>()
  get length() { return this.values.size }
  key(index: number) { return [...this.values.keys()][index] ?? null }
  getItem(key: string) { return this.values.get(key) ?? null }
  setItem(key: string, value: string) { this.values.set(key, value) }
  removeItem(key: string) { this.values.delete(key) }
}

class FakeEvents implements SpacesStoreEventTarget {
  private listeners = new Map<string, Set<(event: StorageEvent | PageTransitionEvent) => void>>()
  addEventListener(type: 'storage' | 'pagehide', listener: (event: StorageEvent | PageTransitionEvent) => void) {
    const listeners = this.listeners.get(type) ?? new Set()
    listeners.add(listener)
    this.listeners.set(type, listeners)
  }
  removeEventListener(type: 'storage' | 'pagehide', listener: (event: StorageEvent | PageTransitionEvent) => void) {
    this.listeners.get(type)?.delete(listener)
  }
  dispatch(type: 'storage' | 'pagehide', event: Partial<StorageEvent> = {}) {
    for (const listener of this.listeners.get(type) ?? []) listener({ type, ...event } as StorageEvent & PageTransitionEvent)
  }
  count(type: 'storage' | 'pagehide') { return this.listeners.get(type)?.size ?? 0 }
}

function seedMain(storage: FakeStorage) {
  storage.values.set(spaceRecordKey(mainSpaceID), JSON.stringify({
    version: 1,
    writeID: 'migration-v1',
    record: { id: mainSpaceID, name: 'main', order: 0, created: 0, updated: 0 },
  }))
}

function harness(overrides: Partial<Parameters<typeof createSpacesStore>[0]> = {}) {
  const storage = overrides.storage === undefined ? new FakeStorage() : overrides.storage
  if (storage instanceof FakeStorage && storage.length === 0) seedMain(storage)
  let now = 1_000
  let id = 0
  const scheduled = new Map<number, () => void>()
  const store = createSpacesStore({
    storage,
    events: new FakeEvents(),
    now: () => now,
    randomID: () => `id-${++id}`,
    schedule: (callback) => { const handle = ++id; scheduled.set(handle, callback); return handle },
    cancel: (handle) => scheduled.delete(handle as number),
    ...overrides,
  })
  return {
    store,
    storage,
    setNow(value: number) { now = value },
    flushScheduled() { for (const callback of [...scheduled.values()]) callback(); scheduled.clear() },
  }
}

test('the client comparator matches the shared client/server corpus', () => {
  const corpus = JSON.parse(readFileSync(new URL('../../testdata/state-comparator.json', import.meta.url), 'utf8')) as Array<{
    left: { updated: number, writeID: string }
    right: { updated: number, writeID: string }
    winner: 'left' | 'right' | 'equal'
  }>
  for (const item of corpus) {
    const left = { version: 1 as const, writeID: item.left.writeID, record: { id: 'left', name: 'left', order: 0, created: 0, updated: item.left.updated } }
    const right = { version: 1 as const, writeID: item.right.writeID, record: { id: 'right', name: 'right', order: 0, created: 0, updated: item.right.updated } }
    assert.equal(Math.sign(compareStoredSpaceRecords(left, right)), { left: 1, equal: 0, right: -1 }[item.winner])
  }
})

test('create, rename, close, and reopen preserve identity and ordering', () => {
  const subject = harness()
  const created = subject.store.create()
  assert.equal(created.ok, true)
  if (!created.ok) return
  assert.equal(created.value.name, 'space 2')
  assert.deepEqual(subject.store.list().map((space) => space.name), ['main', 'space 2'])

  subject.setNow(2_000)
  assert.equal(subject.store.rename(created.value.id, 'review').ok, true)
  assert.equal(subject.store.close(created.value.id).ok, true)
  assert.deepEqual(subject.store.list().map((space) => space.name), ['main'])
  assert.deepEqual(subject.store.recentlyClosed().map((space) => space.name), ['review'])
  assert.equal(subject.store.reopen(created.value.id).ok, true)
  assert.deepEqual(subject.store.list().map((space) => space.name), ['main', 'review'])
})

test('reorder persists one whole shared-stamp ordering and reopen returns at the end', () => {
  const subject = harness()
  const second = subject.store.create()
  const third = subject.store.create()
  assert.equal(second.ok && third.ok, true)
  if (!second.ok || !third.ok) return
  assert.equal(subject.store.reorder(third.value.id, 0).ok, true)
  subject.store.flush()
  const stored = subject.store.list().map((space) => JSON.parse(subject.storage.getItem(spaceRecordKey(space.id)) ?? ''))
  assert.equal(new Set(stored.map((value) => value.writeID)).size, 1)
  assert.equal(new Set(stored.map((value) => value.record.updated)).size, 1)
  assert.deepEqual(subject.store.list().map((space) => space.id), [third.value.id, mainSpaceID, second.value.id])
  subject.store.close(third.value.id)
  subject.store.reopen(third.value.id)
  assert.equal(subject.store.list().at(-1)?.id, third.value.id)
})

test('equal order values from a concurrent create/reorder collision sort deterministically by id', () => {
  const storage = new FakeStorage()
  for (const id of ['z-created', 'a-reordered']) storage.values.set(spaceRecordKey(id), JSON.stringify({
    version: 1, writeID: id, record: { id, name: id, order: 2, created: 0, updated: 1 },
  }))
  const subject = harness({ storage })
  assert.deepEqual(subject.store.list().map((space) => space.id), ['a-reordered', 'z-created'])
})

test('conflicting two-tab reorder batches converge to one whole LWW ordering', () => {
  const storage = new FakeStorage()
  for (const [id, order] of [['a', 0], ['b', 1], ['c', 2]] as const) storage.values.set(spaceRecordKey(id), JSON.stringify({
    version: 1, writeID: 'seed', record: { id, name: id, order, created: 0, updated: 0 },
  }))
  const eventsA = new FakeEvents()
  const eventsB = new FakeEvents()
  const tabA = harness({ storage, events: eventsA, now: () => 1_000, randomID: () => 'a-batch' })
  const tabB = harness({ storage, events: eventsB, now: () => 1_000, randomID: () => 'z-batch' })
  tabA.store.subscribe(() => undefined)
  tabB.store.subscribe(() => undefined)
  tabA.store.reorder('c', 0)
  tabB.store.reorder('a', 2)
  tabA.store.flush()
  tabB.store.flush()
  for (const id of ['a', 'b', 'c']) {
    const key = spaceRecordKey(id)
    eventsA.dispatch('storage', { key, newValue: storage.getItem(key), storageArea: storage as Storage })
    eventsB.dispatch('storage', { key, newValue: storage.getItem(key), storageArea: storage as Storage })
  }
  assert.deepEqual(tabA.store.list().map((space) => space.id), ['b', 'c', 'a'])
  assert.deepEqual(tabB.store.list().map((space) => space.id), ['b', 'c', 'a'])
})

test('a pending create can roll back without becoming recently closed', () => {
  const subject = harness()
  const created = subject.store.create()
  assert.equal(created.ok, true)
  if (!created.ok) return
  assert.equal(subject.store.rollbackCreate(created.value.id), true)
  assert.deepEqual(subject.store.list().map((space) => space.name), ['main'])
  assert.deepEqual(subject.store.recentlyClosed(), [])
  subject.flushScheduled()
  assert.equal(subject.storage.getItem(spaceRecordKey(created.value.id)), null)
})

test('the active cap refuses create and reopen without evicting spaces', () => {
  const subject = harness({ maxSpaces: 2 })
  const second = subject.store.create()
  assert.equal(second.ok, true)
  const refused = subject.store.create()
  assert.equal(refused.ok, false)
  if (!refused.ok) assert.match(refused.reason, /2-space limit/i)
  assert.equal(subject.store.list().length, 2)

  if (!second.ok) return
  subject.store.close(second.value.id)
  subject.store.create()
  const reopen = subject.store.reopen(second.value.id)
  assert.equal(reopen.ok, false)
  assert.equal(subject.store.list().length, 2)
})

test('a cross-device union above the creation cap stays visible and refuses a new create with the real count', () => {
  const storage = new FakeStorage()
  for (let index = 0; index < 17; index++) {
    const id = `space-${index}`
    storage.values.set(spaceRecordKey(id), JSON.stringify({
      version: 1,
      writeID: `device-${index}`,
      record: { id, name: id, order: index, created: index, updated: index },
    }))
  }
  const subject = harness({ storage, maxSpaces: 16 })
  assert.equal(subject.store.list().length, 17)
  const result = subject.store.create()
  assert.equal(result.ok, false)
  if (!result.ok) assert.match(result.reason, /17 spaces/i)
  assert.equal(subject.store.list().length, 17)
})

test('storage events merge per-record LWW tombstones without resurrecting a closed space', () => {
  const storage = new FakeStorage()
  seedMain(storage)
  const eventsA = new FakeEvents()
  const eventsB = new FakeEvents()
  const tabA = harness({ storage, events: eventsA })
  const tabB = harness({ storage, events: eventsB })
  tabB.store.subscribe(() => undefined)
  const created = tabA.store.create()
  assert.equal(created.ok, true)
  if (!created.ok) return
  tabA.flushScheduled()
  const key = spaceRecordKey(created.value.id)
  eventsB.dispatch('storage', { key, newValue: storage.getItem(key), storageArea: storage as Storage })
  assert.equal(tabB.store.list().length, 2)

  tabA.setNow(3_000)
  tabA.store.close(created.value.id)
  tabA.flushScheduled()
  eventsB.dispatch('storage', { key, newValue: storage.getItem(key), storageArea: storage as Storage })
  assert.deepEqual(tabB.store.list().map((space) => space.name), ['main'])
  assert.equal(tabB.store.rename(created.value.id, 'stale').ok, false)
})

test('server merges notify readers without re-enqueueing mutation listeners', () => {
  const subject = harness()
  let mutations = 0
  let reads = 0
  subject.store.subscribeMutations(() => { mutations++ })
  subject.store.subscribe(() => { reads++ })
  subject.store.merge([{
    version: 1,
    writeID: 'server-device',
    record: { id: 'server-only', name: 'server only', order: 1, created: 1, updated: 2 },
  }])
  assert.equal(mutations, 0)
  assert.equal(reads, 1)
  assert.deepEqual(subject.store.list().map(({ id }) => id), ['main', 'server-only'])
})

test('equal-updated merges choose the greater writeID regardless of arrival order', () => {
  const lower: StoredSpaceRecord = {
    version: 1,
    writeID: 'a-write',
    record: { id: 'same-space', name: 'lower writeID', order: 1, created: 1, updated: 42 },
  }
  const greater: StoredSpaceRecord = {
    version: 1,
    writeID: 'z-write',
    record: { id: 'same-space', name: 'greater writeID', order: 1, created: 1, updated: 42 },
  }

  for (const records of [[lower, greater], [greater, lower]]) {
    const subject = harness()
    subject.store.merge(records)
    assert.equal(subject.store.list().find(({ id }) => id === 'same-space')?.name, 'greater writeID')
    assert.equal(subject.store.records().find(({ record }) => record.id === 'same-space')?.writeID, 'z-write')
  }
})

test('event listeners are lazy and detach after the final subscriber leaves', () => {
  const events = new FakeEvents()
  const subject = harness({ events })
  assert.equal(events.count('storage'), 0)
  const unsubscribe = subject.store.subscribe(() => undefined)
  assert.equal(events.count('storage'), 1)
  assert.equal(events.count('pagehide'), 1)
  unsubscribe()
  assert.equal(events.count('storage'), 0)
  assert.equal(events.count('pagehide'), 0)
})

test('old tombstones purge their paired layout recovery', () => {
  const storage = new FakeStorage()
  seedMain(storage)
  const first = harness({ storage })
  const created = first.store.create()
  assert.equal(created.ok, true)
  if (!created.ok) return
  first.flushScheduled()
  first.setNow(2_000)
  first.store.close(created.value.id)
  first.flushScheduled()
  const purged: string[] = []

  harness({
    storage,
    now: () => 2_000 + 31 * 24 * 60 * 60 * 1_000,
    onPurge: (id) => purged.push(id),
  })
  assert.deepEqual(purged, [created.value.id])
  assert.equal(storage.getItem(spaceRecordKey(created.value.id)), null)
})
