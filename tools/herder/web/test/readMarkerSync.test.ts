import assert from 'node:assert/strict'
import test from 'node:test'
import type { GenericStateRow } from '../src/shared/stateSync.ts'
import { comparePositions, mergeMarkerRow, migratedMarkerRows, nextArmed, parseMarkerRow, weakUpdated, type ReadMarker, type ReadMarkerRow, type ReadPosition } from '../src/features/spaces/readMarkerModel.ts'
import { createReadMarkerStore, readMarkerRowsKey, readMarkersKey } from '../src/features/spaces/readMarkerStore.ts'
import { createReadMarkersSync, readMarkersNamespace, readMarkerStoreSyncAdapter } from '../src/features/spaces/readMarkerSync.ts'

function memoryStorage(initial: Record<string, string> = {}) {
  const values = new Map(Object.entries(initial))
  return {
    values,
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => { values.set(key, value) },
  }
}

type Extra = { pos?: ReadPosition | null, at?: number, writeID?: string }

const at = (session: string, offset: number): ReadPosition => ({ session, offset, ts: `t${offset}` })
const mk = (turn: number, unread = false, extra: Extra = {}): ReadMarker => ({ turn, pos: extra.pos ?? null, at: extra.at ?? 0, unread })

function row(key: string, turn: number, unread: boolean, updated: number, extra: Extra = {}): ReadMarkerRow {
  return { key, ...mk(turn, unread, extra), updated, writeID: extra.writeID ?? `w${updated}`, deleted: false }
}

function stateRow(key: string, turn: number, unread: boolean, updated: number, extra: Extra = {}): GenericStateRow {
  return { key, value: { ...mk(turn, unread, extra), updated }, updated, writeID: extra.writeID ?? `w${updated}`, deleted: false }
}

function ids() {
  let next = 0
  return () => `id${++next}`
}

const flush = () => new Promise((resolve) => setTimeout(resolve, 0))

test('the namespace is read.markers and a row value is { turn, pos, at, unread, updated }', () => {
  assert.equal(readMarkersNamespace, 'read.markers')
  const pos = at('s1', 400)
  assert.deepEqual(parseMarkerRow(stateRow('mavu', 12, true, 5, { pos, at: 70 })), row('mavu', 12, true, 5, { pos, at: 70 }))
  assert.deepEqual(parseMarkerRow(stateRow('mavu', 12, false, 5)), row('mavu', 12, false, 5), 'pos null: never read a transcript')
  const value = { turn: 1, pos: null, at: 0, unread: false }
  const bad = (patch: Record<string, unknown>) => parseMarkerRow({ ...stateRow('mavu', 1, false, 5), value: { ...value, ...patch } })
  assert.equal(bad({ turn: -1 }), null)
  assert.equal(bad({ turn: 1.5 }), null)
  assert.equal(bad({ unread: undefined }), null)
  assert.equal(bad({ at: -1 }), null)
  assert.equal(bad({ pos: { session: '', offset: 1, ts: '' } }), null)
  assert.equal(bad({ pos: { session: 's', offset: -1, ts: '' } }), null)
  assert.equal(bad({ pos: { session: 's', offset: 1 } }), null)
  assert.equal(parseMarkerRow({ ...stateRow('mavu', 12, true, 5), value: { read: 1, unread: false } }), null, 'the old { read, unread } shape is refused')
  assert.equal(parseMarkerRow({ ...stateRow('mavu', 12, true, 5), key: '' }), null)
  assert.equal(parseMarkerRow({ key: 'gone', value: null, updated: 9, writeID: 'w', deleted: true })?.deleted, true)
})

test('positions compare by offset within a session; across sessions the newer read wins', () => {
  assert.ok(comparePositions(at('s1', 10), 900, at('s1', 20), 100) > 0, 'further in the session is ahead, whenever read')
  assert.ok(comparePositions(at('s1', 20), 100, at('s1', 10), 900) < 0)
  assert.ok(comparePositions(at('s1', 900), 100, at('s2', 5), 200) > 0, 'a new session read later is ahead')
  assert.ok(comparePositions(at('s2', 5), 200, at('s1', 900), 100) < 0)
  assert.ok(comparePositions(null, 0, at('s1', 1), 1) > 0)
  assert.equal(comparePositions(null, 0, null, 0), 0)
})

test('merge: the newer row wins but turn and position never move backward', () => {
  assert.deepEqual(mergeMarkerRow(undefined, row('a', 5, false, 1)), { row: row('a', 5, false, 1), repair: false })
  const ahead = row('a', 10, false, 100, { pos: at('s1', 500), at: 100 })
  const further = row('a', 12, false, 200, { pos: at('s1', 700), at: 200 })
  assert.deepEqual(mergeMarkerRow(ahead, further), { row: further, repair: false }, 'a newer, further read is taken')
  assert.deepEqual(mergeMarkerRow(ahead, row('a', 5, false, 200, { pos: at('s1', 300), at: 200 })),
    { row: row('a', 10, false, 200, { pos: at('s1', 500), at: 200 }), repair: true },
    'an older device writing a stale read later keeps the further turn and position and republishes them')
  assert.deepEqual(mergeMarkerRow(ahead, row('a', 3, false, 50)), { row: ahead, repair: false }, 'an older row changes nothing')
  assert.deepEqual(mergeMarkerRow(ahead, row('a', 10, false, 200, { pos: null, at: 200 })).row.pos, at('s1', 500), 'a missing position never erases one')
})

test('merge: across sessions the position read more recently stands', () => {
  const oldSession = row('a', 10, false, 100, { pos: at('s1', 9000), at: 100 })
  const newSession = row('a', 10, false, 200, { pos: at('s2', 40), at: 200 })
  assert.deepEqual(mergeMarkerRow(oldSession, newSession), { row: newSession, repair: false }, 'a restarted transcript is read from its own start')
  const stale = row('a', 10, false, 300, { pos: at('s1', 9500), at: 50 })
  assert.deepEqual(mergeMarkerRow(newSession, stale).row.pos, at('s2', 40), 'a newer version carrying an older session read keeps the newer read')
})

test('merge: a newer deliberate unread stands exactly; a newer read clears an older unread', () => {
  const read = row('a', 10, false, 100, { pos: at('s1', 500), at: 100 })
  const marked = row('a', 10, true, 200, { pos: at('s1', 200), at: 100 })
  assert.deepEqual(mergeMarkerRow(read, marked), { row: marked, repair: false }, 'mark unread may move the position back')
  assert.deepEqual(mergeMarkerRow(read, row('a', 9, true, 50)).row, read, 'an older unread is already cleared')
  const cleared = row('a', 10, false, 300, { pos: at('s1', 600), at: 300 })
  assert.deepEqual(mergeMarkerRow(marked, cleared), { row: cleared, repair: false })
})

test('merge: a weak row fills an empty slot but never takes part against a real one', () => {
  const seed = row('a', 20, false, weakUpdated)
  const real = row('a', 10, false, 1000, { pos: at('s1', 50), at: 900 })
  assert.deepEqual(mergeMarkerRow(undefined, seed), { row: seed, repair: false }, 'a weak row fills an empty slot')
  assert.deepEqual(mergeMarkerRow(seed, real), { row: real, repair: false }, 'a real row replaces a held seed outright')
  assert.deepEqual(mergeMarkerRow(real, seed), { row: real, repair: false }, 'an incoming seed changes nothing')
  assert.deepEqual(mergeMarkerRow(row('a', 20, false, weakUpdated, { pos: at('s1', 900), at: 950 }), real), { row: real, repair: false },
    'a seed further on in turn, position and time still loses whole')
  assert.deepEqual(mergeMarkerRow(row('a', 30, false, weakUpdated, { writeID: 'w0' }), seed), { row: row('a', 30, false, weakUpdated), repair: true },
    'two weak rows merge field-wise as before')
})

test('the first run seeds weak rows from the v2 markers once, and a server row wins over them', () => {
  const storage = memoryStorage({ [readMarkersKey]: JSON.stringify({ version: 2, markers: { mavu: 40, ziru: 7 } }) })
  const store = createReadMarkerStore({ storage, randomID: ids(), now: () => 1000 })
  assert.deepEqual(store.markers(), { mavu: mk(40), ziru: mk(7) })
  assert.ok(store.rows().every((stored) => stored.updated === weakUpdated))
  assert.ok(storage.getItem(readMarkerRowsKey), 'migration is recorded so it runs once')
  assert.deepEqual(migratedMarkerRows({ a: 3 }, 'm'), [row('a', 3, false, weakUpdated, { writeID: 'm' })])

  store.merge([stateRow('mavu', 30, true, 500), stateRow('ziru', 9, false, 600, { pos: at('s1', 8), at: 600 })])
  assert.deepEqual(store.markers(), { mavu: mk(30, true), ziru: mk(9, false, { pos: at('s1', 8), at: 600 }) },
    'the server rows win over the weak migration')

  storage.setItem(readMarkersKey, JSON.stringify({ version: 2, markers: { other: 1 } }))
  const again = createReadMarkerStore({ storage, randomID: ids() })
  assert.deepEqual(Object.keys(again.markers()).sort(), ['mavu', 'ziru'], 'a second run reads the v3 rows, not v2')
})

test('apply writes changed markers with a fresh version; weak writes only create rows', () => {
  let clock = 1000
  const storage = memoryStorage()
  const store = createReadMarkerStore({ storage, randomID: ids(), now: () => clock })
  const posted: GenericStateRow[][] = []
  store.subscribeMutations((rows) => posted.push(rows))
  let notified = 0
  store.subscribe(() => { notified += 1 })

  store.apply({ mavu: mk(5) }, { weak: true })
  assert.equal(store.rows()[0]?.updated, weakUpdated)
  store.apply({ mavu: mk(9) }, { weak: true })
  assert.equal(store.markers().mavu?.turn, 5, 'a weak write never overwrites a row')
  store.apply({ mavu: mk(5) })
  assert.equal(posted.length, 1, 'an unchanged marker writes nothing')
  store.apply({ mavu: mk(5, true) })
  assert.equal(store.rows()[0]?.updated, 1000)
  clock = 500
  store.apply({ mavu: mk(6, false, { pos: at('s1', 3), at: 500 }) })
  assert.equal(store.rows()[0]?.updated, 1001, 'versions only move forward even if the clock does not')
  assert.equal(posted.length, 3)
  assert.equal(notified, 3)
  assert.deepEqual(JSON.parse(storage.getItem(readMarkerRowsKey) ?? '[]').map((stored: GenericStateRow) => stored.value), [{ ...mk(6, false, { pos: at('s1', 3), at: 500 }), updated: 1001 }])
})

test('pruning forgets locally and never writes a delete', () => {
  const storage = memoryStorage()
  const store = createReadMarkerStore({ storage, randomID: ids(), now: () => 10 })
  store.apply({ mavu: mk(1), ziru: mk(2, true) })
  const posted: GenericStateRow[][] = []
  store.subscribeMutations((rows) => posted.push(rows))
  store.prune(new Set(['mavu']))
  assert.deepEqual(Object.keys(store.markers()), ['mavu'])
  assert.deepEqual(posted, [])
  assert.equal(JSON.parse(storage.getItem(readMarkerRowsKey) ?? '[]').length, 1)
})

test('a merge that is ahead of the server republishes it after the pull', async () => {
  const store = createReadMarkerStore({ storage: memoryStorage(), randomID: ids(), now: () => 900 })
  store.apply({ mavu: mk(10, false, { pos: at('s1', 500), at: 900 }) })
  const posted: GenericStateRow[][] = []
  store.subscribeMutations((rows) => posted.push(rows))
  store.merge([stateRow('mavu', 4, false, 950, { pos: at('s1', 100), at: 950 })])
  assert.deepEqual(posted, [], 'the repair waits for the pull to settle')
  await flush()
  assert.equal(posted.length, 1)
  assert.deepEqual(posted[0]?.[0]?.value, { ...mk(10, false, { pos: at('s1', 500), at: 950 }), updated: 951 })
  store.merge([stateRow('mavu', 10, false, 951, { pos: at('s1', 500), at: 950, writeID: posted[0]?.[0]?.writeID })])
  await flush()
  assert.equal(posted.length, 1, 'the echo converges without another write')
})

test('two devices sharing a server converge: a read on one clears the unread on the other', async () => {
  const server = new Map<string, GenericStateRow>()
  let rev = 0
  const changes: { rev: number, key: string }[] = []
  const transport = {
    since: async (since: number) => ({ rows: changes.filter((change) => change.rev > since).map((change) => server.get(change.key) as GenericStateRow), rev }),
    upsert: async (rows: GenericStateRow[]) => {
      const accepted: string[] = []
      for (const incoming of rows) {
        const current = server.get(incoming.key)
        if (current && (current.updated > incoming.updated || (current.updated === incoming.updated && current.writeID >= incoming.writeID))) continue
        server.set(incoming.key, incoming)
        changes.push({ rev: ++rev, key: incoming.key })
        accepted.push(incoming.key)
      }
      return { accepted, rev }
    },
  }
  const device = (clock: () => number, prefix: string) => {
    let n = 0
    const store = createReadMarkerStore({ storage: memoryStorage(), randomID: () => `${prefix}${++n}`, now: clock })
    const queue: { cursor: number, keys: string[] } = { cursor: 0, keys: [] }
    const sync = createReadMarkersSync({
      store: readMarkerStoreSyncAdapter(store),
      persistence: { readCursor: () => queue.cursor, writeCursor: (cursor) => { queue.cursor = cursor }, readQueue: () => queue.keys, writeQueue: (keys) => { queue.keys = keys } },
      transport,
    })
    return { store, sync }
  }
  let clock = 1000
  const a = device(() => clock, 'a')
  const b = device(() => clock, 'b')
  await a.sync.start()
  await b.sync.start()
  a.store.apply({ mavu: mk(10, true, { pos: at('s1', 200) }) })
  await flush()
  await b.sync.stateChanged(readMarkersNamespace, rev)
  assert.deepEqual(b.store.markers().mavu, mk(10, true, { pos: at('s1', 200) }), 'a mark on A reaches B')
  clock = 2000
  b.store.apply({ mavu: mk(12, false, { pos: at('s1', 900), at: 2000 }) })
  await flush()
  await a.sync.stateChanged(readMarkersNamespace, rev)
  assert.deepEqual(a.store.markers().mavu, mk(12, false, { pos: at('s1', 900), at: 2000 }), 'reading on B clears it on A')
  assert.deepEqual(server.get('mavu')?.value, { ...mk(12, false, { pos: at('s1', 900), at: 2000 }), updated: 2000 })
  clock = 3000
  a.store.apply({ mavu: mk(12, false, { pos: at('s1', 600), at: 3000 }) })
  await flush()
  await b.sync.stateChanged(readMarkersNamespace, rev)
  await flush()
  await a.sync.stateChanged(readMarkersNamespace, rev)
  assert.deepEqual(a.store.markers().mavu?.pos, at('s1', 900), 'a stale read on A does not rewind B')
  assert.deepEqual(b.store.markers().mavu?.pos, at('s1', 900))
  assert.deepEqual((server.get('mavu')?.value as ReadMarker).pos, at('s1', 900), 'the repair converges the server')
})

test('a manual unread is held while viewed and arms once the agent has been left', () => {
  const unread = { mavu: mk(3, true), ziru: mk(3) }
  const empty = new Set<string>()
  assert.deepEqual([...nextArmed(empty, unread, ['mavu'], true)], [], 'staying on it holds the mark')
  assert.deepEqual([...nextArmed(empty, unread, ['mavu'], false)], [], 'an unready dock never counts as leaving')
  const left = nextArmed(empty, unread, ['ziru'], true)
  assert.deepEqual([...left], ['mavu'], 'leaving arms it')
  assert.equal(nextArmed(left, unread, ['mavu'], true), left, 'coming back keeps it armed (and the same set)')
  assert.deepEqual([...nextArmed(left, { mavu: mk(3) }, [], true)], [], 'a cleared mark disarms')
})
