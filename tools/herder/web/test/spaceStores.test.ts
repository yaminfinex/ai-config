import assert from 'node:assert/strict'
import test from 'node:test'
import { readBrowserRecord, writeBrowserRecord } from '../src/features/spaces/browserRecord.ts'
import { maxReadMarkers, parseReadMarkers, readMarkersKey, readReadMarkers, setMarkers, writeReadMarkers } from '../src/features/spaces/readMarkerStore.ts'
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
  assert.equal(readMarkersKey, 'herder.web.read-markers.v1')
  assert.equal(spaceMRUKey, 'herder.web.spaces.mru.v1')
  const storage = memoryStorage({ [readMarkersKey]: 'x', [spaceMRUKey]: 'y' })
  clearAllLayoutFamilies(storage)
  assert.equal(storage.getItem(readMarkersKey), 'x', 'a layout reset does not forget what was read')
  assert.equal(storage.getItem(spaceMRUKey), 'y')
})

test('read markers round-trip and reject other versions and shapes', () => {
  const storage = memoryStorage()
  const { markers, state } = readReadMarkers(storage)
  assert.deepEqual(markers, {})
  writeReadMarkers(storage, { mavu: 's:10', ziru: '' }, state)
  assert.deepEqual(readReadMarkers(storage).markers, { mavu: 's:10', ziru: '' })
  assert.equal(parseReadMarkers(JSON.stringify({ version: 2, markers: {} })), null)
  assert.equal(parseReadMarkers(JSON.stringify({ version: 1, markers: { a: 1 } })), null)
  assert.equal(parseReadMarkers(JSON.stringify({ version: 1, markers: [] })), null)
  assert.equal(parseReadMarkers('{broken'), null)
})

test('a corrupt primary recovers from the last-good backup without rotating the backup away', () => {
  const good = JSON.stringify({ version: 1, markers: { mavu: 's:1' } })
  const storage = memoryStorage({ [readMarkersKey]: '{broken', [`${readMarkersKey}.last-good`]: good })
  const { markers, state } = readReadMarkers(storage)
  assert.deepEqual(markers, { mavu: 's:1' })
  assert.equal(state.recovering, true)
  const next = writeReadMarkers(storage, { mavu: 's:2' }, state)
  assert.equal(storage.getItem(`${readMarkersKey}.last-good`), good, 'recovery writes the primary only')
  assert.deepEqual(next, { recovering: false, lastGoodRaw: storage.getItem(readMarkersKey) })
  writeReadMarkers(storage, { mavu: 's:3' }, next)
  assert.equal(storage.getItem(`${readMarkersKey}.last-good`), next.lastGoodRaw, 'the backup then trails the last good primary')
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

test('marker updates keep identity when unchanged and cap by dropping the least recently touched', () => {
  const markers = { a: '1', b: '2' }
  assert.equal(setMarkers(markers, { a: '1' }), markers)
  assert.deepEqual(Object.keys(setMarkers(markers, { a: '3' })), ['b', 'a'])
  const full = Object.fromEntries(Array.from({ length: maxReadMarkers }, (_, index) => [`agent-${index}`, 'x']))
  const touched = setMarkers(full, { 'agent-0': 'y' })
  const capped = setMarkers(touched, { newcomer: '' })
  assert.equal(Object.keys(capped).length, maxReadMarkers)
  assert.equal(capped['agent-1'], undefined, 'the oldest untouched marker goes first')
  assert.equal(capped['agent-0'], 'y')
  assert.equal(capped.newcomer, '')
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
