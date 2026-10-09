import assert from 'node:assert/strict'
import test from 'node:test'
import {
  agentAttention,
  agentUnread,
  agentsInDock,
  attentionLabel,
  boardAgents,
  markerKeepSet,
  markReadUpdates,
  readUpdates,
  seedUpdates,
  spaceAttention,
  spaceMenuItems,
  spaceStreamAgents,
  storedSpaceAgents,
  streamAgentLimit,
  totalAttention,
  turnEnd,
} from '../src/features/spaces/spaceAttentionModel.ts'
import { baselineMarker, type ReadMarker, type ReadMarkers } from '../src/features/spaces/readMarkerModel.ts'
import type { Board, Pane, Row } from '../src/types.ts'

const m = baselineMarker
const marks = (turns: Record<string, number>): ReadMarkers => Object.fromEntries(Object.entries(turns).map(([name, turn]) => [name, m(turn)]))
const turns = (markers: ReadMarkers) => Object.fromEntries(Object.entries(markers).map(([name, marker]) => [name, marker.turn]))
// seed and read apply seedUpdates / readUpdates the way the store does.
const seed = (markers: ReadMarkers, board: Board | undefined, open: string[]): ReadMarkers => ({ ...markers, ...seedUpdates(markers, board, open) })
const read = (markers: ReadMarkers, board: Board | undefined, viewed: string[], extra: { armed?: Set<string>, now?: number } = {}): ReadMarkers =>
  ({ ...markers, ...readUpdates({ markers, board, viewed, positions: Object.fromEntries(viewed.map((name) => [name, null])), armed: extra.armed ?? new Set(), now: extra.now ?? 1000 }) })

function pane(agent: string, bus_status: string, extra: Partial<Pane> = {}): Pane {
  return { pane_id: `pane-${agent}`, agent, tool: 'claude', herdr_status: 'idle', bus_status, gap: '', ...extra }
}

function boardOf(panes: Pane[], unplaced: Row[] = []): Board {
  return {
    workspaces: [{
      workspace_id: 'w1', number: 1, label: 'fixture', focused: true, pane_count: panes.length, tab_count: 1, active_tab_id: 't1', agent_status: 'idle',
      tabs: [{ tab_id: 't1', number: 1, label: 'agents', focused: true, pane_count: panes.length, agent_status: 'idle', panes }],
    }],
    unplaced,
  }
}

const dock = {
  grid: { root: { type: 'branch', data: [] } },
  panels: {
    'agent:mavu': { params: { kind: 'agent', name: 'mavu', preview: false } },
    'agent:mavu:2': { params: { kind: 'agent', name: 'mavu', preview: true } },
    'file:%2Frepo:README.md': { params: { kind: 'file', root: '/repo', path: 'README.md', preview: false, viewMode: 'rendered' } },
    'agent:ziru': { params: { kind: 'agent', name: 'ziru', preview: false } },
    'junk': { params: { kind: 'agent' } },
  },
}

test('the open agents of a dock are its agent panels, once each, in panel order', () => {
  assert.deepEqual(agentsInDock(dock), ['mavu', 'ziru'])
  assert.deepEqual(agentsInDock(null), [])
  assert.deepEqual(agentsInDock({}), [])
})

test('another space is read from its stored v4 layout without any storage write', () => {
  const values = new Map([
    ['herder.web.layout.v4:alpha', JSON.stringify({ version: 4, dock: {
      grid: { root: { type: 'branch', data: [{ type: 'leaf', data: { id: 'g', views: ['agent:mavu'], activeView: 'agent:mavu' } }] } },
      panels: { 'agent:mavu': { id: 'agent:mavu', contentComponent: 'agent', params: { kind: 'agent', name: 'mavu', preview: false } } },
      activeGroup: 'g',
    } })],
    ['herder.web.layout.v4:broken', '{broken'],
  ])
  const writes: string[] = []
  const storage = {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string) => { writes.push(key) },
  }
  assert.deepEqual(storedSpaceAgents(storage, 'alpha'), ['mavu'])
  assert.deepEqual(storedSpaceAgents(storage, 'broken'), [])
  assert.deepEqual(storedSpaceAgents(storage, 'missing'), [])
  assert.deepEqual(writes, [], 'a corrupt layout is left for the space restore to recover')
  assert.deepEqual(storedSpaceAgents({ getItem: () => { throw new Error('blocked') } }, 'alpha'), [])
})

test('the stream follows each space\'s open agents once, the active ones first, up to the serve limit', () => {
  assert.deepEqual(spaceStreamAgents(['kumo', 'dore'], { one: ['kumo', 'dore'], two: ['mavu', 'dore'], three: [] }), ['kumo', 'dore', 'mavu'])
  assert.deepEqual(spaceStreamAgents([], {}), [])
  const many = Array.from({ length: 150 }, (_, index) => `agent-${index}`)
  const followed = spaceStreamAgents(['active'], { other: many })
  assert.equal(followed.length, streamAgentLimit)
  assert.equal(followed[0], 'active')
})

test('the turn end is the serve-stamped id, placed or not; nothing without one, never for retired or stopped', () => {
  assert.equal(turnEnd(pane('a', 'listening', { turn_end_id: 41 })), 41)
  assert.equal(turnEnd(pane('a', 'active', { turn_end_id: 41 })), 41, 'a new turn running does not hide the last one')
  assert.equal(turnEnd({ ...pane('a', 'listening', { turn_end_id: 41 }), pane_id: '' }), 41, 'an unplaced row carries it too')
  assert.equal(turnEnd(pane('a', 'listening', { context_used: 1200, agent_session: 's1' })), null, 'context is a gauge, not a turn')
  assert.equal(turnEnd(pane('a', 'listening', { turn_end_id: 0 })), null)
  assert.equal(turnEnd(pane('a', 'retired', { turn_end_id: 41 })), null)
  assert.equal(turnEnd(pane('a', 'stopped', { turn_end_id: 41 })), null)
  assert.equal(turnEnd(undefined), null)
})

test('an agent is unread only when a turn ended after the marker this browser holds', () => {
  assert.equal(agentAttention(pane('a', 'listening', { turn_end_id: 50 }), m(41)), 'unread')
  assert.equal(agentAttention(pane('a', 'active', { turn_end_id: 50 }), m(41)), 'unread', 'woken again before it was seen')
  assert.equal(agentAttention(pane('a', 'listening', { turn_end_id: 50 }), m(50)), null)
  assert.equal(agentAttention(pane('a', 'listening', { turn_end_id: 41 }), m(50)), null, 'an older id (a reset incarnation) is not new')
  assert.equal(agentAttention(pane('a', 'listening', { turn_end_id: 50 }), undefined), null, 'no marker: seeding decides, not the first render')
  assert.equal(agentAttention(pane('a', 'listening', { context_used: 900 }), m(41)), null, 'no turn_end_id: never unread')
})

test('blocked always shows, read or not and with or without a turn end', () => {
  assert.equal(agentAttention(pane('a', 'blocked'), undefined), 'blocked')
  assert.equal(agentAttention(pane('a', 'blocked'), m(12)), 'blocked')
  assert.equal(agentAttention(pane('a', 'blocked', { turn_end_id: 5 }), m(5)), 'blocked')
  assert.equal(agentAttention(undefined, m(1)), null)
})

test('unread is allow-listed to listening and active; every other status with a newer turn never counts', () => {
  assert.equal(agentAttention(pane('a', 'listening', { turn_end_id: 110 }), m(100)), 'unread')
  assert.equal(agentAttention(pane('a', 'active', { turn_end_id: 110 }), m(100)), 'unread', 'an unviewed turn stays unread while the next one runs')
  for (const status of ['unknown', '-', 'inactive', 'pending', 'retired', 'stopped', '']) {
    assert.equal(agentAttention(pane('a', status, { turn_end_id: 110 }), m(100)), null, status)
  }
})

test('space attention collects unread and blocked open agents from the board, subagents and unplaced included', () => {
  const board = boardOf([
    pane('mavu', 'listening', { turn_end_id: 10 }),
    pane('ziru', 'blocked'),
    pane('kobe', 'listening', { turn_end_id: 3, subagents: [pane('kobe-sub', 'blocked'), pane('kobe-two', 'listening', { turn_end_id: 9 })] }),
  ], [pane('lone', 'listening', { turn_end_id: 8 })])
  const markers = marks({ mavu: 1, ziru: 1, kobe: 3, 'kobe-two': 2, lone: 1 })
  assert.deepEqual(spaceAttention(board, ['mavu', 'ziru', 'kobe', 'kobe-sub', 'kobe-two', 'lone', 'ghost'], markers), {
    unread: ['mavu', 'kobe-two', 'lone'], blocked: ['ziru', 'kobe-sub'], drafts: [],
  })
  assert.deepEqual(spaceAttention(undefined, ['mavu'], markers), { unread: [], blocked: [], drafts: [] })
})

test('a deliberate mark unread is quiet unread; blocked stays loud', () => {
  const marked: ReadMarker = { ...m(50), unread: true }
  assert.equal(agentAttention(pane('a', 'listening', { turn_end_id: 50 }), marked), 'unread')
  assert.equal(agentAttention(pane('a', 'listening'), marked), 'unread', 'with no turn end yet')
  assert.equal(agentAttention(pane('a', 'blocked', { turn_end_id: 50 }), marked), 'blocked')
  const board = boardOf([pane('mavu', 'listening', { turn_end_id: 50 }), pane('ziru', 'blocked')])
  assert.deepEqual(spaceAttention(board, ['mavu', 'ziru'], { mavu: marked, ziru: { ...m(1), unread: true } }), { unread: ['mavu'], blocked: ['ziru'], drafts: [] })
})

test('markers follow the agent name, not its placement: placed to unplaced and back keeps read and unread', () => {
  const placed = boardOf([pane('mavu', 'listening', { turn_end_id: 20 })])
  const unplaced = boardOf([], [{ ...pane('mavu', 'listening', { turn_end_id: 20 }), pane_id: '' }])
  const seeded = seed({}, placed, ['mavu'])
  assert.deepEqual(seeded, { mavu: m(20) })
  assert.deepEqual(seedUpdates(seeded, unplaced, ['mavu']), {}, 'moving out of its pane re-seeds nothing')
  assert.deepEqual(spaceAttention(unplaced, ['mavu'], seeded), { unread: [], blocked: [], drafts: [] }, 'moving does not invent a completion')
  const finishedUnplaced = boardOf([], [{ ...pane('mavu', 'listening', { turn_end_id: 27 }), pane_id: '' }])
  assert.deepEqual(spaceAttention(finishedUnplaced, ['mavu'], seeded).unread, ['mavu'], 'a turn ended while unplaced')
  const backInPlace = boardOf([pane('mavu', 'listening', { turn_end_id: 27 })])
  assert.deepEqual(spaceAttention(backInPlace, ['mavu'], seeded).unread, ['mavu'], 'still unread once placed again')
  assert.deepEqual(spaceAttention(backInPlace, ['mavu'], read(seeded, finishedUnplaced, ['mavu'])).unread, [], 'read while unplaced stays read')
})

test('seeding records never-seen open agents at their current turn end, silently, and waits for an id', () => {
  const board = boardOf([pane('mavu', 'listening', { turn_end_id: 10 }), pane('ziru', 'active')])
  assert.deepEqual(seedUpdates({}, undefined, ['mavu']), {}, 'no board: nothing to seed against')
  const seeded = seed({}, board, ['mavu', 'ziru', 'ghost'])
  assert.deepEqual(seeded, { mavu: m(10) }, 'no turn end yet: no marker, not a zero baseline')
  assert.equal(spaceAttention(board, ['mavu', 'ziru'], seeded).unread.length, 0)
  assert.deepEqual(seedUpdates(seeded, board, ['mavu', 'ziru']), {}, 'existing markers are kept')
  assert.deepEqual(seedUpdates(marks({ mavu: 1 }), board, ['mavu']), {}, 'a seen agent is never re-seeded over its unread turn')
})

test('a first turn end that arrives late is an unknown baseline, seeded silently rather than lit as new', () => {
  const before = boardOf([pane('ziru', 'listening')])
  const markers = seed({}, before, ['ziru'])
  assert.deepEqual(markers, {})
  const late = boardOf([pane('ziru', 'listening', { turn_end_id: 300 })])
  assert.equal(spaceAttention(late, ['ziru'], markers).unread.length, 0, 'unmarked is never unread')
  const seeded = seed(markers, late, ['ziru'])
  assert.deepEqual(seeded, { ziru: m(300) })
  assert.deepEqual(spaceAttention(late, ['ziru'], seeded).unread, [])
  assert.deepEqual(spaceAttention(boardOf([pane('ziru', 'listening', { turn_end_id: 310 })]), ['ziru'], seeded).unread, ['ziru'], 'the next turn is new')
})

test('a missing turn_end_id keeps the marker and is never unread; blocked still shows', () => {
  const markers = { mavu: { ...m(40), at: 1000 } }
  const missing = boardOf([pane('mavu', 'listening')])
  assert.deepEqual(spaceAttention(missing, ['mavu'], markers), { unread: [], blocked: [], drafts: [] })
  assert.deepEqual(seedUpdates(markers, missing, ['mavu']), {})
  assert.deepEqual(read(markers, missing, ['mavu']), markers, 'viewing without an id or a position records nothing')
  assert.deepEqual(spaceAttention(boardOf([pane('mavu', 'blocked')]), ['mavu'], markers), { unread: [], blocked: ['mavu'], drafts: [] })
})

test('viewing records the latest turn end and never moves a marker backwards', () => {
  const board = boardOf([pane('mavu', 'listening', { turn_end_id: 10 }), pane('ziru', 'active', { turn_end_id: 1 })])
  const markers = marks({ mavu: 1, ziru: 1 })
  const after = read(markers, board, ['mavu', 'ziru'])
  assert.deepEqual(turns(after), { mavu: 10, ziru: 1 })
  assert.equal(after.mavu?.at, 1000, 'reading stamps when')
  assert.equal(spaceAttention(board, ['mavu'], after).unread.length, 0)
  assert.deepEqual(readUpdates({ markers: after, board, viewed: ['mavu'], positions: { mavu: null }, armed: new Set(), now: 1500 }), {})
  assert.deepEqual(read(markers, board, []), markers)
  assert.equal(read(marks({ mavu: 99 }), board, ['mavu']).mavu?.turn, 99, 'a reset incarnation does not rewind the marker')
})

test('reading waits for a transcript end that is still loading', () => {
  const board = boardOf([pane('mavu', 'listening', { turn_end_id: 10 })])
  const markers = marks({ mavu: 1 })
  assert.deepEqual(readUpdates({ markers, board, viewed: ['mavu'], positions: { mavu: undefined }, armed: new Set(), now: 1000 }), {})
  const pos = { session: 's1', offset: 400, ts: 't' }
  assert.deepEqual(readUpdates({ markers, board, viewed: ['mavu'], positions: { mavu: pos }, armed: new Set(), now: 1000 }), { mavu: { turn: 10, pos, at: 1000, unread: false } })
})

test('a manual unread is held while viewed and cleared by the dwell only once armed', () => {
  const board = boardOf([pane('mavu', 'listening', { turn_end_id: 10 })])
  const markers = { mavu: { ...m(10), unread: true } }
  assert.deepEqual(read(markers, board, ['mavu']), markers, 'staying on it holds the mark')
  assert.deepEqual(read(markers, board, ['mavu'], { armed: new Set(['mavu']) }).mavu, { turn: 10, pos: null, at: 1000, unread: false }, 'left and come back: the dwell clears it')
})

test('pruning keeps agents open or on the board, and waits for a board', () => {
  const board = boardOf([pane('mavu', 'listening', { turn_end_id: 10, subagents: [pane('sub', 'listening')] })], [pane('lone', 'listening')])
  assert.deepEqual([...boardAgents(board)].sort(), ['lone', 'mavu', 'sub'])
  assert.deepEqual([...markerKeepSet(board, ['open']) ?? []].sort(), ['lone', 'mavu', 'open', 'sub'])
  assert.equal(markerKeepSet(undefined, ['open']), null, 'no board: nothing is known gone')
  const many = Array.from({ length: 1000 }, (_, index) => `open-${index}`)
  assert.equal(markerKeepSet(boardOf([]), many)?.size, 1000, 'open agents are never evicted, however many')
})

test('labels read naturally for every count combination', () => {
  assert.equal(attentionLabel({ unread: ['a', 'b'], blocked: ['c'], drafts: [] }), '2 agents waiting, 1 blocked')
  assert.equal(attentionLabel({ unread: ['a'], blocked: [], drafts: [] }), '1 agent waiting')
  assert.equal(attentionLabel({ unread: [], blocked: ['c', 'd'], drafts: [] }), '2 agents blocked')
  assert.equal(attentionLabel({ unread: [], blocked: [], drafts: [] }), 'no agents waiting')
})

test('the folded total counts an agent open in two spaces once', () => {
  assert.deepEqual(totalAttention([{ unread: ['a', 'b'], blocked: ['c'], drafts: [] }, { unread: ['a'], blocked: ['c', 'd'], drafts: [] }]), { unread: ['a', 'b'], blocked: ['c', 'd'], drafts: [] })
})

const at = (offset: number) => ({ session: 's1', offset, ts: `t${offset}` })

test('an agent can be marked read while marked unread (blocked too) or a newer turn ended, never otherwise', () => {
  const listening = pane('mavu', 'listening', { turn_end_id: 5 })
  assert.equal(agentUnread(listening, m(4)), true, 'a new turn')
  assert.equal(agentUnread(listening, m(5)), false, 'read')
  assert.equal(agentUnread(listening, { ...m(5), unread: true }), true, 'a mark unread')
  assert.equal(agentUnread(pane('mavu', 'blocked', { turn_end_id: 5 }), { ...m(5), unread: true }), true, 'a mark unread while blocked')
  assert.equal(agentUnread(pane('mavu', 'listening'), m(0)), false, 'no turn end')
  assert.equal(agentUnread(listening, undefined), false, 'no baseline yet')
})

test('mark read writes what a dwell read would, at once, and skips agents already read', () => {
  const board = boardOf([pane('mavu', 'listening', { turn_end_id: 7 }), pane('ziru', 'listening', { turn_end_id: 3 })])
  const markers: ReadMarkers = { mavu: { turn: 5, pos: at(100), at: 10, unread: false }, ziru: { turn: 3, pos: at(50), at: 10, unread: false } }
  assert.deepEqual(markReadUpdates({ markers, board, names: ['mavu', 'ziru'], positions: { mavu: at(900) }, now: 2000 }),
    { mavu: { turn: 7, pos: at(900), at: 2000, unread: false } }, 'the tail where loaded; ziru is already read')
  assert.deepEqual(markReadUpdates({ markers, board, names: ['mavu'], positions: {}, now: 2000 }),
    { mavu: { turn: 7, pos: at(100), at: 2000, unread: false } }, 'no loaded transcript: the position held')
  const held = { ...markers, ziru: { turn: 3, pos: at(20), at: 10, unread: true } }
  assert.deepEqual(markReadUpdates({ markers: held, board, names: ['ziru'], positions: { ziru: at(60) }, now: 2000 }),
    { ziru: { turn: 3, pos: at(60), at: 2000, unread: false } }, 'a held mark unread is cleared without arming')
  assert.deepEqual(markReadUpdates({ markers: { mavu: { turn: 9, pos: at(500), at: 10, unread: true } }, board, names: ['mavu'], positions: { mavu: at(400) }, now: 2000 }),
    { mavu: { turn: 9, pos: at(500), at: 2000, unread: false } }, 'reading never moves backward')
})

test('the read toggle flips an agent between read and unread', () => {
  const board = boardOf([pane('mavu', 'listening', { turn_end_id: 7 })])
  const row = board.workspaces[0]!.tabs[0]!.panes[0]
  let markers: ReadMarkers = { mavu: m(6) }
  const toggle = () => {
    if (agentUnread(row, markers.mavu)) markers = { ...markers, ...markReadUpdates({ markers, board, names: ['mavu'], positions: {}, now: 3000 }) }
    else markers = { ...markers, mavu: { ...markers.mavu!, unread: true } }
  }
  toggle()
  assert.deepEqual(markers.mavu, { turn: 7, pos: null, at: 3000, unread: false }, 'a new turn: marked read')
  toggle()
  assert.equal(markers.mavu!.unread, true, 'read: marked unread')
  toggle()
  assert.equal(agentUnread(row, markers.mavu), false, 'marked unread: read again')
})

test('a space marks read exactly the agents its badge counts, over unread, read and no-turn agents', () => {
  const board = boardOf([
    pane('mavu', 'listening', { turn_end_id: 7 }),
    pane('ziru', 'listening', { turn_end_id: 3 }),
    pane('kelo', 'listening'),
    pane('pira', 'listening'),
    pane('tano', 'blocked', { turn_end_id: 9 }),
  ])
  const markers: ReadMarkers = { mavu: m(5), ziru: m(3), kelo: m(0), pira: { ...m(0), unread: true }, tano: m(2) }
  const agents = ['mavu', 'ziru', 'kelo', 'pira', 'tano']
  const attention = spaceAttention(board, agents, markers)
  assert.deepEqual(attention.unread, ['mavu', 'pira'])
  assert.deepEqual(spaceMenuItems(attention), [{ id: 'read', label: 'Mark all read' }])
  const updates = markReadUpdates({ markers, board, names: attention.unread, positions: {}, now: 4000 })
  assert.deepEqual(updates, {
    mavu: { turn: 7, pos: null, at: 4000, unread: false },
    pira: { turn: 0, pos: null, at: 4000, unread: false },
  })
  const after = spaceAttention(board, agents, { ...markers, ...updates })
  assert.deepEqual(after, { unread: [], blocked: ['tano'], drafts: [] }, 'blocked stays loud')
  assert.deepEqual(spaceMenuItems(after), [], 'nothing left to mark: no menu')
})
