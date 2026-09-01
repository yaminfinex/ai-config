import assert from 'node:assert/strict'
import test from 'node:test'

import {
  createSpacesStore,
  type SpacesStorage,
  type SpacesStoreEventTarget,
} from '../src/features/spaces/spacesStore.ts'
import { mainSpaceID, spaceRecordKey } from '../src/features/spaces/spacesModel.ts'

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
