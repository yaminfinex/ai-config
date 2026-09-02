import assert from 'node:assert/strict'
import test from 'node:test'

import {
  createNotesStore,
  defaultRandomID,
  notesStoragePrefix,
  type StoredNoteRecord,
  type NotesStorage,
  type NotesStoreEventTarget,
} from '../src/features/notes/notesStore.ts'

class FakeStorage implements NotesStorage {
  readonly values = new Map<string, string>()
  writes: string[] = []
  blocked = false

  get length() { return this.values.size }
  key(index: number) { return [...this.values.keys()][index] ?? null }
  getItem(key: string) {
    if (this.blocked) throw new Error('storage blocked')
    return this.values.get(key) ?? null
  }
  setItem(key: string, value: string) {
    if (this.blocked) throw new Error('storage blocked')
    this.values.set(key, value)
    this.writes.push(key)
  }
  removeItem(key: string) {
    if (this.blocked) throw new Error('storage blocked')
    this.values.delete(key)
  }
}

class FakeEvents implements NotesStoreEventTarget {
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

function harness(overrides: Partial<Parameters<typeof createNotesStore>[0]> = {}) {
  const storage = overrides.storage === undefined ? new FakeStorage() : overrides.storage
  const events = overrides.events ?? new FakeEvents()
  let now = 1_000
  let sequence = 0
  const scheduled = new Map<number, () => void>()
  const store = createNotesStore({
    storage,
    events,
    now: () => now,
    randomID: () => `id-${++sequence}`,
    schedule: (callback) => { const id = ++sequence; scheduled.set(id, callback); return id },
    cancel: (id) => { scheduled.delete(id as number) },
    ...overrides,
  })
  return {
    store,
    storage,
    events,
    setNow(value: number) { now = value },
    flushScheduled() { for (const callback of [...scheduled.values()]) callback(); scheduled.clear() },
  }
}

test('notes list by group, sort by recent update, notify subscribers, and debounce persistence', () => {
  const subject = harness()
  let notifications = 0
  subject.store.subscribe(() => { notifications += 1 })

  const first = subject.store.add({ group: 'kilo', text: 'first' })
  subject.setNow(2_000)
  const second = subject.store.add({ group: 'kilo', text: 'second' })
  assert.equal(first.ok, true)
  assert.equal(second.ok, true)
  assert.deepEqual(subject.store.listGroup('kilo').map((note) => note.text), ['second', 'first'])
  assert.equal(notifications, 2)
  assert.equal((subject.storage as FakeStorage).writes.length, 0)

  subject.flushScheduled()
  assert.equal((subject.storage as FakeStorage).writes.filter((key) => key.includes(':record:')).length, 2)
})

test('captured quote is immutable provenance while only its comment is edited', () => {
  const subject = harness()
  const added = subject.store.add({ group: 'kilo', quote: 'selected code', text: '', source: { kind: 'file', path: 'src/App.tsx' } })
  assert.equal(added.ok, true)
  if (!added.ok) return
  assert.equal(added.value.quote, 'selected code')
  const edited = subject.store.edit(added.value.id, { text: 'owner comment' })
  assert.equal(edited.ok, true)
  if (!edited.ok) return
  assert.equal(edited.value.quote, 'selected code')
  assert.equal(edited.value.text, 'owner comment')
})

test('legacy anchored records load without inventing a quote field', () => {
  const seed = harness()
  const added = seed.store.add({ group: 'kilo', text: 'legacy quote\n\nlegacy comment', source: { kind: 'transcript', agent: 'kilo' } })
  assert.equal(added.ok, true)
  seed.flushScheduled()
  const loaded = harness({ storage: seed.storage })
  assert.equal(loaded.store.list()[0]?.quote, undefined)
  assert.equal(loaded.store.list()[0]?.text, 'legacy quote\n\nlegacy comment')
})

test('different-record writes from two tabs cannot clobber each other', () => {
  const storage = new FakeStorage()
  let aID = 0
  let bID = 0
  const tabA = harness({ storage, randomID: () => `a-${++aID}` })
  const tabB = harness({ storage, randomID: () => `b-${++bID}` })

  const a = tabA.store.add({ group: 'kilo', text: 'from A' })
  assert.equal(a.ok, true)
  tabA.flushScheduled()
  const b = tabB.store.add({ group: 'kilo', text: 'from B' })
  assert.equal(b.ok, true)
  tabB.flushScheduled()

  const records = [...storage.values.keys()].filter((key) => key.startsWith(`${notesStoragePrefix}record:`))
  assert.equal(records.length, 2)
})

test('storage events merge one note by last-write-wins and tombstones prevent resurrection', () => {
  const storage = new FakeStorage()
  const eventsA = new FakeEvents()
  const eventsB = new FakeEvents()
  const tabA = harness({ storage, events: eventsA })
  const tabB = harness({ storage, events: eventsB })
  tabB.store.subscribe(() => undefined)
  const added = tabA.store.add({ group: 'kilo', text: 'original' })
  assert.equal(added.ok, true)
  tabA.flushScheduled()
  const note = added.value
  const key = `${notesStoragePrefix}record:${encodeURIComponent(note.id)}`
  eventsB.dispatch('storage', { key, newValue: storage.getItem(key), storageArea: storage as Storage })
  assert.equal(tabB.store.list()[0]?.text, 'original')

  tabA.setNow(3_000)
  tabA.store.delete([note.id])
  tabA.flushScheduled()
  eventsB.dispatch('storage', { key, newValue: storage.getItem(key), storageArea: storage as Storage })
  assert.deepEqual(tabB.store.list(), [])

  tabB.setNow(2_000)
  const stale = tabB.store.edit(note.id, { text: 'stale resurrection' })
  assert.equal(stale.ok, false)
  assert.deepEqual(tabB.store.list(), [])
})

test('pulled merges and storage events notify readers without notifying mutation subscribers', () => {
  const subject = harness()
  let reads = 0
  let mutations = 0
  subject.store.subscribe(() => { reads++ })
  subject.store.subscribeMutations(() => { mutations++ })
  const remote: StoredNoteRecord = {
    version: 1,
    writeID: 'remote-write',
    record: { id: 'remote-note', group: 'general', text: 'from server', created: 1, updated: 2 },
  }
  subject.store.merge([remote])
  assert.equal(reads, 1)
  assert.equal(mutations, 0)

  const storage = subject.storage as FakeStorage
  subject.flushScheduled()
  const eventRecord: StoredNoteRecord = { ...remote, writeID: 'tab-write', record: { ...remote.record, text: 'from tab', updated: 3 } }
  const key = `${notesStoragePrefix}record:${encodeURIComponent(eventRecord.record.id)}`
  storage.values.set(key, JSON.stringify(eventRecord))
  ;(subject.events as FakeEvents).dispatch('storage', { key, newValue: JSON.stringify(eventRecord), storageArea: storage as Storage })
  assert.equal(reads, 2)
  assert.equal(mutations, 0)
})

test('equal-updated merges choose the greater writeID regardless of arrival order', () => {
  const lower: StoredNoteRecord = {
    version: 1,
    writeID: 'a-write',
    record: { id: 'same-note', group: 'general', text: 'lower writeID', created: 1, updated: 42 },
  }
  const greater: StoredNoteRecord = {
    version: 1,
    writeID: 'z-write',
    record: { id: 'same-note', group: 'general', text: 'greater writeID', created: 1, updated: 42 },
  }

  for (const records of [[lower, greater], [greater, lower]]) {
    const subject = harness()
    subject.store.merge(records)
    assert.equal(subject.store.list()[0]?.text, 'greater writeID')
    assert.equal(subject.store.records()[0]?.writeID, 'z-write')
  }
})

test('each successful local add, edit, and delete call emits one mutation batch', () => {
  const subject = harness()
  const batches: StoredNoteRecord[][] = []
  subject.store.subscribeMutations((records) => batches.push(records))
  const first = subject.store.add({ group: 'general', text: 'first' })
  const second = subject.store.add({ group: 'general', text: 'second' })
  assert.equal(first.ok, true)
  assert.equal(second.ok, true)
  if (!first.ok || !second.ok) return
  assert.equal(batches.length, 2)
  subject.store.edit(first.value.id, { text: 'edited' })
  assert.equal(batches.length, 3)
  subject.store.delete([first.value.id, second.value.id])
  assert.equal(batches.length, 4)
  assert.equal(batches[3]?.length, 2)
  assert.ok(batches[3]?.every(({ record }) => 'deleted' in record))
})

test('pagehide flushes pending writes and backups remain under the one notes namespace', () => {
  const subject = harness()
  subject.store.subscribe(() => undefined)
  subject.store.add({ group: 'general', text: 'flush me' })
  assert.equal((subject.storage as FakeStorage).writes.length, 0)
  ;(subject.events as FakeEvents).dispatch('pagehide')
  const keys = [...(subject.storage as FakeStorage).values.keys()]
  assert.ok(keys.some((key) => key.startsWith(`${notesStoragePrefix}record:`)))
  assert.ok(keys.every((key) => key.startsWith(notesStoragePrefix)))
})

test('event listeners are lazy and dispose flushes pending writes before detaching them', () => {
  const subject = harness()
  const events = subject.events as FakeEvents
  assert.equal(events.count('storage'), 0)
  const unsubscribe = subject.store.subscribe(() => undefined)
  assert.equal(events.count('storage'), 1)
  subject.store.add({ group: 'general', text: 'survive disposal' })
  subject.store.dispose()
  unsubscribe()
  assert.equal(events.count('storage'), 0)
  assert.ok([...(subject.storage as FakeStorage).values.keys()].some((key) => key.startsWith(`${notesStoragePrefix}record:`)))
})

test('corrupt primary is preserved, last-good is restored, and recovery is visible', () => {
  const seed = harness()
  const added = seed.store.add({ group: 'general', text: 'safe copy' })
  assert.equal(added.ok, true)
  seed.flushScheduled()
  const note = added.value
  seed.setNow(2_000)
  seed.store.edit(note.id, { text: 'new safe copy' })
  seed.flushScheduled()
  const storage = seed.storage as FakeStorage
  const primary = `${notesStoragePrefix}record:${encodeURIComponent(note.id)}`
  storage.values.set(primary, '{broken')

  const recovered = harness({ storage })
  assert.equal(recovered.store.list()[0]?.text, 'safe copy')
  assert.equal(recovered.store.status().recovered, true)
  assert.ok([...storage.values.keys()].some((key) => key.startsWith(`${notesStoragePrefix}recovery:`)))

  harness({ storage })
  assert.equal([...storage.values.keys()].filter((key) => key.startsWith(`${notesStoragePrefix}recovery:`)).length, 1)
})

test('deleting a note removes its last-good text while retaining the tombstone', () => {
  const subject = harness()
  const added = subject.store.add({ group: 'general', text: 'delete this text' })
  assert.equal(added.ok, true)
  subject.flushScheduled()
  subject.store.delete([added.value.id])
  subject.flushScheduled()
  const storage = subject.storage as FakeStorage
  assert.equal(storage.getItem(`${notesStoragePrefix}last-good:${encodeURIComponent(added.value.id)}`), null)
  assert.doesNotMatch(storage.getItem(`${notesStoragePrefix}record:${encodeURIComponent(added.value.id)}`) ?? '', /delete this text/)
})

test('malformed record keys are ignored without disabling persistence', () => {
  const storage = new FakeStorage()
  storage.values.set(`${notesStoragePrefix}record:%`, '{}')
  const subject = harness({ storage })
  assert.equal(subject.store.status().persistent, true)
  assert.equal(subject.store.add({ group: 'general', text: 'still persistent' }).ok, true)
  subject.flushScheduled()
  assert.ok([...storage.values.keys()].some((key) => key.startsWith(`${notesStoragePrefix}record:id-`)))
})

test('reconcile notifies subscribers when persisted state wins', () => {
  const storage = new FakeStorage()
  const first = harness({ storage })
  const added = first.store.add({ group: 'general', text: 'remote deletion' })
  assert.equal(added.ok, true)
  first.flushScheduled()
  const second = harness({ storage })
  second.setNow(2_000)
  second.store.delete([added.value.id])
  second.flushScheduled()
  let notifications = 0
  first.store.subscribe(() => { notifications += 1 })
  assert.equal(first.store.edit(added.value.id, { text: 'stale edit' }).ok, false)
  assert.equal(notifications, 1)
  assert.deepEqual(first.store.list(), [])
})

test('unavailable storage degrades to memory and reports the problem without losing session notes', () => {
  const storage = new FakeStorage()
  storage.blocked = true
  const subject = harness({ storage })
  const added = subject.store.add({ group: 'general', text: 'still usable' })
  assert.equal(added.ok, true)
  subject.flushScheduled()
  assert.equal(subject.store.list()[0]?.text, 'still usable')
  assert.equal(subject.store.status().persistent, false)
  assert.match(subject.store.status().problem, /not saved between browser sessions/i)
})

test('bounds refuse new writing and never evict older notes', () => {
  const subject = harness({ maxActiveNotes: 1, maxNoteBytes: 30, maxTotalBytes: 1_000 })
  assert.equal(subject.store.add({ group: 'general', text: 'kept' }).ok, true)
  const countRefusal = subject.store.add({ group: 'general', text: 'refused' })
  assert.equal(countRefusal.ok, false)
  assert.match(countRefusal.reason, /100|limit|room/i)
  assert.deepEqual(subject.store.list().map((note) => note.text), ['kept'])

  const bytes = harness({ maxActiveNotes: 10, maxNoteBytes: 5, maxTotalBytes: 1_000 })
  const byteRefusal = bytes.store.add({ group: 'general', text: 'too long' })
  assert.equal(byteRefusal.ok, false)
  assert.match(byteRefusal.reason, /long/i)
  const quoteRefusal = bytes.store.add({ group: 'kilo', quote: 'too long', text: '', source: { kind: 'transcript', agent: 'kilo' } })
  assert.equal(quoteRefusal.ok, false)
  assert.match(quoteRefusal.reason, /long/i)
})

test('old tombstones purge after 30 days without returning deleted notes', () => {
  const storage = new FakeStorage()
  const original = harness({ storage })
  const added = original.store.add({ group: 'general', text: 'temporary' })
  assert.equal(added.ok, true)
  original.flushScheduled()
  original.setNow(2_000)
  original.store.delete([added.value.id])
  original.flushScheduled()

  const later = harness({ storage, now: () => 2_000 + 31 * 24 * 60 * 60 * 1_000 })
  assert.deepEqual(later.store.list(), [])
  assert.equal([...storage.values.keys()].some((key) => key.includes(encodeURIComponent(added.value.id))), false)
})

test('add still works when crypto.randomUUID is missing (insecure origins such as plain-http tailnet)', () => {
  const descriptor = Object.getOwnPropertyDescriptor(crypto, 'randomUUID')
  Object.defineProperty(crypto, 'randomUUID', { value: undefined, configurable: true, writable: true })
  try {
    const storage = new FakeStorage()
    const store = createNotesStore({ storage, events: new FakeEvents(), schedule: () => undefined, cancel: () => undefined })
    const added = store.add({ group: 'general', text: 'saved without randomUUID' })
    assert.equal(added.ok, true)
    if (added.ok) assert.match(added.value.id, /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/)
    assert.equal(store.list()[0]?.text, 'saved without randomUUID')
    assert.equal(defaultRandomID() === defaultRandomID(), false)
  } finally {
    if (descriptor) Object.defineProperty(crypto, 'randomUUID', descriptor)
  }
})

test('an unexpected exception during a mutation becomes a visible refusal, never a silent throw', () => {
  const subject = harness()
  assert.equal(subject.store.add({ group: 'general', text: 'kept safe' }).ok, true)
  const kept = subject.store.list()
  const broken = createNotesStore({
    storage: new FakeStorage(),
    events: new FakeEvents(),
    randomID: () => { throw new Error('id generation exploded') },
    schedule: () => undefined,
    cancel: () => undefined,
  })
  const added = broken.add({ group: 'general', text: 'doomed' })
  assert.equal(added.ok, false)
  if (!added.ok) assert.match(added.reason, /was not saved|left untouched/i)
  assert.deepEqual(subject.store.list(), kept)
})
