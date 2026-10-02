import assert from 'node:assert/strict'
import test from 'node:test'
import { readBrowserRecord, writeBrowserRecord } from '../src/features/spaces/browserRecord.ts'
import { createReadMarkerStore, parseReadMarkers, readMarkerRowsKey, readMarkersKey, readReadMarkers } from '../src/features/spaces/readMarkerStore.ts'
import { baselineMarker } from '../src/features/spaces/readMarkerModel.ts'
import { maxSpaceMRU, mruSpaceIDs, parseSpaceMRU, readSpaceMRU, spaceMRUKey, touchSpaceMRU, writeSpaceMRU } from '../src/features/spaces/spaceMRU.ts'
import { clearAllLayoutFamilies } from '../src/features/spaces/spacesModel.ts'

function memoryStorage(initial: Record<string, string> = {}) {
  const values = new Map(Object.entries(initial))
  return {
    values,
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => { values.set(key, value) },
    removeItem: (key: string) => { values.delete(key) },
    key: (index: number) => [...values.keys()][index] ?? null,
    get length() { return values.size },
  }
}

test('both stores use versioned keys outside the layout families', () => {
  assert.equal(readMarkersKey, 'herder.web.read-markers.v2')
  assert.equal(spaceMRUKey, 'herder.web.spaces.mru.v1')
  const storage = memoryStorage({ [readMarkersKey]: 'x', [spaceMRUKey]: 'y' })
  clearAllLayoutFamilies(storage)
  assert.equal(storage.getItem(readMarkersKey), 'x', 'a layout reset does not forget what was read')
  assert.equal(storage.getItem(spaceMRUKey), 'y')
})

test('v2 read markers are still read, for the one-time migration, and reject other versions and shapes', () => {
  const storage = memoryStorage({ [readMarkersKey]: JSON.stringify({ version: 2, markers: { mavu: 348655, ziru: 12 } }) })
  assert.deepEqual(readReadMarkers(storage).markers, { mavu: 348655, ziru: 12 })
  assert.deepEqual(readReadMarkers(memoryStorage()).markers, {})
  assert.equal(parseReadMarkers(JSON.stringify({ version: 1, markers: { a: 's:1' } })), null, 'v1 fingerprints are not migrated')
  assert.equal(parseReadMarkers(JSON.stringify({ version: 2, markers: { a: 's:1' } })), null)
  assert.equal(parseReadMarkers(JSON.stringify({ version: 2, markers: { a: 0 } })), null)
  assert.equal(parseReadMarkers(JSON.stringify({ version: 2, markers: { a: 1.5 } })), null)
  assert.equal(parseReadMarkers(JSON.stringify({ version: 2, markers: [] })), null)
  assert.equal(parseReadMarkers('{broken'), null)
})

test('a corrupt v2 primary is still read from its last-good backup', () => {
  const good = JSON.stringify({ version: 2, markers: { mavu: 1 } })
  const storage = memoryStorage({ [readMarkersKey]: '{broken', [`${readMarkersKey}.last-good`]: good })
  const { markers, state } = readReadMarkers(storage)
  assert.deepEqual(markers, { mavu: 1 })
  assert.equal(state.recovering, true)
})

test('the v3 rows live under their own key, and the v2 record is left in place', () => {
  assert.equal(readMarkerRowsKey, 'herder.web.read-markers.v3:rows')
  const v2 = JSON.stringify({ version: 2, markers: { mavu: 4 } })
  const storage = memoryStorage({ [readMarkersKey]: v2 })
  const store = createReadMarkerStore({ storage, now: () => 1000, randomID: () => 'w' })
  assert.deepEqual(store.markers(), { mavu: baselineMarker(4) })
  assert.equal(storage.getItem(readMarkersKey), v2)
  assert.equal(JSON.parse(storage.getItem(readMarkerRowsKey) ?? '[]').length, 1)
  const storageReset = memoryStorage({ [readMarkerRowsKey]: '[]', [spaceMRUKey]: 'y' })
  clearAllLayoutFamilies(storageReset)
  assert.equal(storageReset.getItem(readMarkerRowsKey), '[]', 'a layout reset does not forget what was read')
})

test('the record helper skips unchanged writes and survives blocked storage', () => {
  const storage = memoryStorage()
  const state = writeBrowserRecord(storage, 'k', 'raw', { recovering: false, lastGoodRaw: null })
  let writes = 0
  assert.equal(writeBrowserRecord({ setItem: () => { writes++ } }, 'k', 'raw', state), state)
  assert.equal(writes, 0)
  const blocked = { setItem: () => { throw new Error('quota') } }
  assert.equal(writeBrowserRecord(blocked, 'k', 'other', state), state)
  const read = readBrowserRecord({ getItem: () => { throw new Error('blocked') } }, 'k', (raw) => raw)
  assert.deepEqual(read, { value: null, state: { recovering: false, lastGoodRaw: null } })
})

test('marker updates keep identity when unchanged and there is no count cap', () => {
  const store = createReadMarkerStore({ storage: null, now: () => 1000, randomID: () => 'w' })
  store.apply({ a: baselineMarker(1), b: baselineMarker(2) })
  const markers = store.markers()
  store.apply({ a: baselineMarker(1) })
  assert.equal(store.markers(), markers)
  store.apply(Object.fromEntries(Array.from({ length: 1000 }, (_, index) => [`agent-${index}`, baselineMarker(index + 1)])))
  assert.equal(Object.keys(store.markers()).length, 1002, 'nothing is evicted to make room')
})

test('prune forgets names outside the keep set locally, preserving identity otherwise, and writes no delete', () => {
  const store = createReadMarkerStore({ storage: null, now: () => 1000, randomID: () => 'w' })
  store.apply({ a: baselineMarker(1), b: baselineMarker(2) })
  const written: unknown[] = []
  store.subscribeMutations((rows) => written.push(...rows))
  const markers = store.markers()
  store.prune(new Set(['a', 'b', 'c']))
  assert.equal(store.markers(), markers)
  store.prune(new Set(['b']))
  assert.deepEqual(store.markers(), { b: baselineMarker(2) })
  assert.deepEqual(written, [])
})

test('MRU touch moves a space to the front, keeps identity when already first and caps', () => {
  const order = ['a', 'b', 'c']
  assert.equal(touchSpaceMRU(order, 'a'), order)
  assert.deepEqual(touchSpaceMRU(order, 'c'), ['c', 'a', 'b'])
  assert.deepEqual(touchSpaceMRU(order, 'new'), ['new', 'a', 'b', 'c'])
  const long = Array.from({ length: maxSpaceMRU }, (_, index) => `s${index}`)
  assert.equal(touchSpaceMRU(long, 'x').length, maxSpaceMRU)
})

test('MRU order is live spaces most recent first, current first, unseen spaces after in list order', () => {
  const spaces = [{ id: 'one' }, { id: 'two' }, { id: 'three' }, { id: 'four' }]
  assert.deepEqual(mruSpaceIDs(['three', 'gone', 'one'], spaces, 'two'), ['two', 'three', 'one', 'four'])
  assert.deepEqual(mruSpaceIDs([], spaces, null), ['one', 'two', 'three', 'four'])
  assert.deepEqual(mruSpaceIDs(['two'], spaces, 'missing'), ['two', 'one', 'three', 'four'])
})

test('MRU storage round-trips, dedupes and recovers from its backup', () => {
  const storage = memoryStorage()
  writeSpaceMRU(storage, ['b', 'a'], readSpaceMRU(storage).state)
  assert.deepEqual(readSpaceMRU(storage).order, ['b', 'a'])
  assert.deepEqual(parseSpaceMRU(JSON.stringify({ version: 1, order: ['a', 'b', 'a'] })), ['a', 'b'])
  assert.equal(parseSpaceMRU(JSON.stringify({ version: 1, order: [1] })), null)
  const recovering = memoryStorage({ [spaceMRUKey]: 'nope', [`${spaceMRUKey}.last-good`]: JSON.stringify({ version: 1, order: ['z'] }) })
  assert.deepEqual(readSpaceMRU(recovering).order, ['z'])
  assert.equal(readSpaceMRU(recovering).state.recovering, true)
})
