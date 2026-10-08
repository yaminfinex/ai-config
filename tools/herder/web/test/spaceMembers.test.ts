import assert from 'node:assert/strict'
import test from 'node:test'
import type { SerializedDockview } from 'dockview-react'

import { writePanelToStoredSpace, type DockPanelParams } from '../src/features/layout/dockLayout.ts'
import {
  memberParams,
  membersFromDock,
  parseMembersRow,
  reconcileSpaceMembers,
  storedSpaceMembers,
  type SpaceMember,
} from '../src/features/spaces/spaceMembersModel.ts'
import { createSpaceMembersStore, spaceMembersLocalKey, spaceMembersRowsKey } from '../src/features/spaces/spaceMembersStore.ts'
import { createSpaceMembersSync, spaceMembersNamespace, spaceMembersStoreSyncAdapter } from '../src/features/spaces/spaceMembersSync.ts'
import type { GenericStateRow, StateSyncPersistence } from '../src/shared/stateSync.ts'
import { panelID } from '../src/features/workspace/panelRegistryModel.ts'

class MemoryStorage {
  values = new Map<string, string>()
  get length() { return this.values.size }
  key(index: number) { return [...this.values.keys()][index] ?? null }
  getItem(key: string) { return this.values.get(key) ?? null }
  setItem(key: string, value: string) { this.values.set(key, value) }
  removeItem(key: string) { this.values.delete(key) }
}

const agent = (name: string, preview = false): DockPanelParams => ({ kind: 'agent', name, preview })
const file = (root: string, path: string, preview = false): DockPanelParams => ({ kind: 'file', root, path, preview, viewMode: 'source' })
const pane = { pane_id: 'p1', agent: 'riko', tool: 'claude', herdr_status: 'idle', bus_status: 'listening', gap: '' }

function dock(groups: DockPanelParams[][]): SerializedDockview {
  const panels: Record<string, unknown> = {}
  const leaves = groups.map((views, index) => {
    for (const params of views) panels[panelID(params)] = { id: panelID(params), contentComponent: params.kind, tabComponent: 'herder-tab', title: 'x', params }
    return { type: 'leaf', data: { id: `g${index}`, views: views.map(panelID), activeView: panelID(views[0]) } }
  })
  return { grid: { root: { type: 'branch', data: leaves }, width: 100, height: 100, orientation: 'HORIZONTAL' }, panels, activeGroup: 'g0' } as unknown as SerializedDockview
}

const names = (members: SpaceMember[]) => members.map((member) => member.kind === 'agent' ? member.name : `${member.root}:${member.path}`)

test('membersFromDock lists pinned agent and file tabs in group then tab order, excluding previews, screens, folders and changes', () => {
  const layout = dock([
    [agent('riko'), agent('mupo', true), { kind: 'folder', root: '/r', path: 'src', preview: false }],
    [file('/r', 'a.md'), { kind: 'screen', pane, identity: { paneID: 'p1', workspaceID: 'w', tabID: 't', agent: 'riko' }, preview: false }],
    [{ kind: 'changes', root: '/r', preview: false }, agent('lora'), file('/r', 'b.md', true)],
  ])
  assert.deepEqual(membersFromDock(layout), [
    { kind: 'agent', name: 'riko' },
    { kind: 'file', root: '/r', path: 'a.md' },
    { kind: 'agent', name: 'lora' },
  ])
  assert.deepEqual(membersFromDock(null), [])
})

test('memberParams opens members pinned with the file view a fresh open would choose', () => {
  assert.deepEqual(memberParams({ kind: 'agent', name: 'riko' }), { kind: 'agent', name: 'riko', preview: false })
  assert.deepEqual(memberParams({ kind: 'file', root: '/r', path: 'README.md' }), { kind: 'file', root: '/r', path: 'README.md', preview: false, viewMode: 'rendered' })
})

test('parseMembersRow skips malformed rows and drops only malformed or unknown members', () => {
  const base = { key: 's1', updated: 5, writeID: 'w', deleted: false }
  assert.equal(parseMembersRow(null), null)
  assert.equal(parseMembersRow({ ...base, value: null }), null)
  assert.equal(parseMembersRow({ ...base, value: { members: 'riko' } }), null)
  assert.equal(parseMembersRow({ ...base, key: '', value: { members: [] } }), null)
  assert.equal(parseMembersRow({ ...base, updated: 'soon', value: { members: [] } }), null)
  assert.equal(parseMembersRow({ ...base, deleted: 'no', value: { members: [] } }), null)
  const row = parseMembersRow({ ...base, value: { members: [
    { kind: 'agent', name: 'riko' }, { kind: 'agent', name: '' }, { kind: 'screen', pane: 'p1' }, 7,
    { kind: 'file', root: '/r', path: 'a.md' }, { kind: 'file', root: '/r' }, { kind: 'agent', name: 'riko' },
  ] } })
  assert.deepEqual(row?.members, [{ kind: 'agent', name: 'riko' }, { kind: 'file', root: '/r', path: 'a.md' }])
  assert.deepEqual(parseMembersRow({ ...base, deleted: true, value: null })?.deleted, true)
})

test('storedSpaceMembers reads a non-active space layout without writing, and refuses a corrupt one', () => {
  const storage = new MemoryStorage()
  assert.deepEqual(storedSpaceMembers(storage, 'empty'), [])
  assert.equal(writePanelToStoredSpace(storage, 's2', agent('riko')).ok, true)
  assert.equal(writePanelToStoredSpace(storage, 's2', file('/r', 'a.md')).ok, true)
  const before = new Map(storage.values)
  assert.deepEqual(names(storedSpaceMembers(storage, 's2') ?? []), ['riko', '/r:a.md'])
  assert.deepEqual(storage.values, before)
  storage.setItem('herder.web.layout.v4:bad', '{not json')
  assert.equal(storedSpaceMembers(storage, 'bad'), null)
})

function harness(docks: Record<string, SpaceMember[] | null>, options: { storage?: MemoryStorage, clock?: { now: number }, id?: string, refuse?: Set<string> } = {}) {
  const clock = options.clock ?? { now: 100 }
  let sequence = 0
  const store = createSpaceMembersStore({ storage: options.storage ?? new MemoryStorage(), now: () => clock.now, randomID: () => `${options.id ?? 'w'}${++sequence}` })
  const mutations: GenericStateRow[] = []
  store.subscribeMutations((rows) => mutations.push(...rows))
  const adds: Array<[string, SpaceMember[]]> = []
  const run = (live = Object.keys(docks), closed: string[] = []) => reconcileSpaceMembers(store, {
    liveSpaceIDs: live,
    closedSpaceIDs: closed,
    read: (id) => docks[id] ? [...docks[id]] : docks[id] === null ? null : [],
    add: (id, members) => {
      adds.push([id, members])
      if (options.refuse?.has(id)) return false
      docks[id] = [...(docks[id] ?? []), ...members]
      return true
    },
  })
  return { store, mutations, adds, run, docks, clock }
}

const a = (name: string): SpaceMember => ({ kind: 'agent', name })

test('first reconcile backfills every live space, including empty ones, and skips unreadable layouts', () => {
  const h = harness({ s1: [a('riko'), { kind: 'file', root: '/r', path: 'a.md' }], s2: [], bad: null })
  assert.deepEqual(h.run(), ['s1', 's2'])
  assert.deepEqual(h.mutations.map((row) => [row.key, row.value, row.deleted]), [
    ['s1', { members: [{ kind: 'agent', name: 'riko' }, { kind: 'file', root: '/r', path: 'a.md' }], updated: 100 }, false],
    ['s2', { members: [], updated: 100 }, false],
  ])
  assert.deepEqual(h.run(), [])
})

test('layout-only changes do not write; a membership or order change writes once', () => {
  const h = harness({ s1: [a('riko'), a('mupo')] })
  h.run()
  h.mutations.length = 0
  assert.deepEqual(h.run(), [])
  h.docks.s1 = [a('mupo'), a('riko')]
  assert.deepEqual(h.run(), ['s1'])
  h.docks.s1 = [a('mupo')]
  assert.deepEqual(h.run(), ['s1'])
  assert.deepEqual(h.run(), [])
  assert.deepEqual(h.mutations.map((row) => names((row.value as { members: SpaceMember[] }).members)), [['mupo', 'riko'], ['mupo']])
})

test('a remote row adds missing members once, never removes, and does not write back an unchanged dock', () => {
  const h = harness({ s1: [a('riko'), a('mupo')], s2: [] })
  h.run()
  h.mutations.length = 0
  h.store.merge([{ key: 's1', value: { members: [a('riko'), a('lora')], updated: 200 }, updated: 200, writeID: 'native', deleted: false }])
  h.store.merge([{ key: 's2', value: { members: [a('mile')], updated: 200 }, updated: 200, writeID: 'native2', deleted: false }])
  h.run()
  assert.deepEqual(h.adds, [['s1', [a('lora')]], ['s2', [a('mile')]]])
  assert.deepEqual(names(h.docks.s1 ?? []), ['riko', 'mupo', 'lora'])
  assert.deepEqual(h.mutations, [])
  // The user closes the added member: the row is not reapplied and the removal publishes.
  h.docks.s1 = [a('riko'), a('mupo')]
  assert.deepEqual(h.run(), ['s1'])
  assert.deepEqual(h.adds.length, 2)
  // A remote row missing members closes nothing.
  h.store.merge([{ key: 's1', value: { members: [], updated: 900 }, updated: 900, writeID: 'native3', deleted: false }])
  h.run()
  assert.deepEqual(names(h.docks.s1 ?? []), ['riko', 'mupo'])
})

test('a member that cannot be opened holds the space back until it lands, and never overwrites the row', () => {
  const refuse = new Set(['s2'])
  const h = harness({ s1: [a('riko')], s2: [a('dune')] }, { refuse })
  h.store.merge([{ key: 's2', value: { members: [a('dune'), a('fozi')], updated: 50 }, updated: 50, writeID: 'native', deleted: false }])
  // First run, with a local change pending too: s2 neither publishes nor records the row as applied.
  h.docks.s2 = [a('dune'), a('gino')]
  assert.deepEqual(h.run(), ['s1'])
  assert.equal(h.store.row('s2')?.writeID, 'native')
  assert.deepEqual(h.store.local('s2'), {})
  assert.deepEqual(h.run(), [])
  // Once the layout accepts the write, the member opens and the union publishes.
  refuse.clear()
  assert.deepEqual(h.run(), ['s2'])
  assert.deepEqual(names(h.docks.s2 ?? []), ['dune', 'gino', 'fozi'])
  assert.deepEqual(names(h.store.row('s2')?.members ?? []), ['dune', 'gino', 'fozi'])
  assert.equal(h.adds.filter(([id]) => id === 's2').length, 3)
})

test('a remote add while a local change is pending publishes the combined dock', () => {
  const h = harness({ s1: [a('riko')] })
  h.run()
  h.mutations.length = 0
  h.docks.s1 = [a('riko'), a('mupo')]
  h.store.merge([{ key: 's1', value: { members: [a('riko'), a('lora')], updated: 200 }, updated: 200, writeID: 'native', deleted: false }])
  h.clock.now = 300
  assert.deepEqual(h.run(), ['s1'])
  assert.deepEqual(names((h.mutations[0].value as { members: SpaceMember[] }).members), ['riko', 'mupo', 'lora'])
})

test('a first-run browser publishes the union of its dock and the remote row, then settles', () => {
  const h = harness({ s1: [a('mupo')] })
  h.store.merge([{ key: 's1', value: { members: [a('riko')], updated: 50 }, updated: 50, writeID: 'other', deleted: false }])
  assert.deepEqual(h.run(), ['s1'])
  assert.deepEqual(names(h.docks.s1 ?? []), ['mupo', 'riko'])
  assert.deepEqual(h.run(), [])
})

test('two browsers holding the same set in different orders converge without a write loop', () => {
  const left = harness({ s1: [a('riko'), a('mupo')] }, { id: 'L' })
  const right = harness({ s1: [a('mupo'), a('riko')] }, { id: 'R' })
  left.run()
  right.run()
  for (let round = 0; round < 5; round += 1) {
    left.clock.now += 10
    right.clock.now += 10
    right.store.merge(left.store.rows())
    left.store.merge(right.store.rows())
    left.mutations.length = 0
    right.mutations.length = 0
    left.run()
    right.run()
  }
  assert.deepEqual(left.mutations, [])
  assert.deepEqual(right.mutations, [])
  assert.deepEqual(names(left.docks.s1 ?? []), ['riko', 'mupo'])
  assert.deepEqual(names(right.docks.s1 ?? []), ['mupo', 'riko'])
})

test('closing a space writes one deleted row, and reopening republishes its dock', () => {
  const h = harness({ s1: [a('riko')], s2: [a('mupo')] })
  h.run()
  h.mutations.length = 0
  assert.deepEqual(h.run(['s1'], ['s2']), ['s2'])
  assert.deepEqual(h.mutations.map((row) => [row.key, row.deleted]), [['s2', true]])
  assert.deepEqual(h.run(['s1'], ['s2']), [])
  assert.deepEqual(h.run(['s1', 's2'], []), ['s2'])
  assert.deepEqual(h.mutations.at(-1)?.deleted, false)
  assert.deepEqual(h.adds, [])
})

test('store bookkeeping survives a reload so a closed tab is not re-added and nothing republishes', () => {
  const storage = new MemoryStorage()
  const first = harness({ s1: [a('riko'), a('mupo')] }, { storage })
  first.store.merge([{ key: 's1', value: { members: [a('riko'), a('mupo')], updated: 50 }, updated: 50, writeID: 'other', deleted: false }])
  first.run()
  assert.ok(storage.getItem(spaceMembersRowsKey))
  assert.ok(storage.getItem(spaceMembersLocalKey))
  const second = harness({ s1: [a('riko')] }, { storage })
  assert.deepEqual(second.run(), ['s1'])
  assert.deepEqual(second.adds, [])
  assert.deepEqual(harness({ s1: [a('riko')] }, { storage }).run(), [])
  storage.setItem(spaceMembersRowsKey, '{broken')
  storage.setItem(spaceMembersLocalKey, '[1]')
  assert.deepEqual(createSpaceMembersStore({ storage }).rows(), [])
})

test('the store keeps the last write per space and ignores malformed rows', () => {
  const store = createSpaceMembersStore({ storage: null, now: () => 10, randomID: () => 'local' })
  store.merge([
    { key: 's1', value: { members: [a('riko')], updated: 20 }, updated: 20, writeID: 'b', deleted: false },
    { key: 's1', value: { members: [a('mupo')], updated: 20 }, updated: 20, writeID: 'a', deleted: false },
    { key: 's2', value: 'garbage', updated: 20, writeID: 'c', deleted: false },
  ])
  assert.deepEqual(names(store.row('s1')?.members ?? []), ['riko'])
  assert.equal(store.row('s2'), undefined)
  // A local write always lands after the row it replaces, even on a slow clock.
  assert.equal(store.publish('s1', [a('lora')]).updated, 21)
})

test('spaces.members rides the shared state sync: backfill posts, remote rows merge, and an unchanged pull posts nothing', async () => {
  const store = createSpaceMembersStore({ storage: null, now: () => 100, randomID: () => 'local-1' })
  const persistence: StateSyncPersistence & { queue: string[] } = {
    queue: [], readCursor: () => 0, writeCursor: () => undefined,
    readQueue() { return this.queue }, writeQueue(keys) { this.queue = keys },
  }
  const posts: GenericStateRow[][] = []
  const remote: GenericStateRow = { key: 's1', value: { members: [a('lora')], updated: 50 }, updated: 50, writeID: 'native', deleted: false }
  const pulled: GenericStateRow[][] = []
  const sync = createSpaceMembersSync({
    store: spaceMembersStoreSyncAdapter(store),
    persistence,
    transport: { since: async () => ({ rows: [remote], rev: 3 }), upsert: async (rows) => { posts.push(rows); return { accepted: rows.map((row) => row.key), rev: 4 } } },
    onRows: (rows) => pulled.push(rows),
  })
  await sync.start()
  assert.deepEqual(posts, [])
  assert.deepEqual(names(store.row('s1')?.members ?? []), ['lora'])
  const docks: Record<string, SpaceMember[]> = { s1: [a('riko')] }
  reconcileSpaceMembers(store, { liveSpaceIDs: ['s1'], closedSpaceIDs: [], read: (id) => [...docks[id]], add: (id, members) => { docks[id].push(...members); return true } })
  await new Promise((resolve) => setTimeout(resolve, 0))
  assert.equal(posts.length, 1)
  assert.deepEqual(names((posts[0][0].value as { members: SpaceMember[] }).members), ['riko', 'lora'])
  assert.equal(spaceMembersNamespace, 'spaces.members')
  await sync.stateChanged('spaces', 99)
  await sync.stateChanged(spaceMembersNamespace, 2)
  assert.equal(posts.length, 1)
  sync.dispose()
})

test('the workspace wires spaces.members into the fleet stream wake and the dock', async () => {
  const { readFile } = await import('node:fs/promises')
  const controller = await readFile(new URL('../src/features/workspace/useWorkspaceController.ts', import.meta.url), 'utf8')
  assert.match(controller, /onMembersStateChanged\(namespace, rev\)/)
  const hook = await readFile(new URL('../src/features/workspace/useSpaceMembers.ts', import.meta.url), 'utf8')
  assert.match(hook, /inactive: api\.panels\.length > 0/)
  assert.doesNotMatch(hook, /removePanel|closePanel/)
})
