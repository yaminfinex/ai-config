import assert from 'node:assert/strict'
import test from 'node:test'
import {
  agentAttention,
  agentsInDock,
  attentionLabel,
  boardAgents,
  markViewedRead,
  pruneReadMarkers,
  seedReadMarkers,
  spaceAttention,
  storedSpaceAgents,
  totalAttention,
  turnEnd,
} from '../src/features/spaces/spaceAttentionModel.ts'
import type { Board, Pane, Row } from '../src/types.ts'

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
  assert.equal(agentAttention(pane('a', 'listening', { turn_end_id: 50 }), 41), 'unread')
  assert.equal(agentAttention(pane('a', 'active', { turn_end_id: 50 }), 41), 'unread', 'woken again before it was seen')
  assert.equal(agentAttention(pane('a', 'listening', { turn_end_id: 50 }), 50), null)
  assert.equal(agentAttention(pane('a', 'listening', { turn_end_id: 41 }), 50), null, 'an older id (a reset incarnation) is not new')
  assert.equal(agentAttention(pane('a', 'listening', { turn_end_id: 50 }), undefined), null, 'no marker: seeding decides, not the first render')
  assert.equal(agentAttention(pane('a', 'listening', { context_used: 900 }), 41), null, 'no turn_end_id: never unread')
})

test('blocked always shows, read or not and with or without a turn end; retired and unknown agents never count', () => {
  assert.equal(agentAttention(pane('a', 'blocked'), undefined), 'blocked')
  assert.equal(agentAttention(pane('a', 'blocked'), 12), 'blocked')
  assert.equal(agentAttention(pane('a', 'blocked', { turn_end_id: 5 }), 5), 'blocked')
  assert.equal(agentAttention(pane('a', 'stopped', { turn_end_id: 5 }), 1), null)
  assert.equal(agentAttention(pane('a', 'retired', { turn_end_id: 5 }), 1), null)
  assert.equal(agentAttention(undefined, 1), null)
})

test('space attention collects unread and blocked open agents from the board, subagents and unplaced included', () => {
  const board = boardOf([
    pane('mavu', 'listening', { turn_end_id: 10 }),
    pane('ziru', 'blocked'),
    pane('kobe', 'listening', { turn_end_id: 3, subagents: [pane('kobe-sub', 'blocked'), pane('kobe-two', 'listening', { turn_end_id: 9 })] }),
  ], [pane('lone', 'listening', { turn_end_id: 8 })])
  const markers = { mavu: 1, ziru: 1, kobe: 3, 'kobe-two': 2, lone: 1 }
  assert.deepEqual(spaceAttention(board, ['mavu', 'ziru', 'kobe', 'kobe-sub', 'kobe-two', 'lone', 'ghost'], markers), {
    unread: ['mavu', 'kobe-two', 'lone'], blocked: ['ziru', 'kobe-sub'],
  })
  assert.deepEqual(spaceAttention(undefined, ['mavu'], markers), { unread: [], blocked: [] })
})

test('markers follow the agent name, not its placement: placed to unplaced and back keeps read and unread', () => {
  const placed = boardOf([pane('mavu', 'listening', { turn_end_id: 20 })])
  const unplaced = boardOf([], [{ ...pane('mavu', 'listening', { turn_end_id: 20 }), pane_id: '' }])
  const seeded = seedReadMarkers({}, placed, ['mavu'])
  assert.deepEqual(seeded, { mavu: 20 })
  assert.equal(seedReadMarkers(seeded, unplaced, ['mavu']), seeded, 'moving out of its pane re-seeds nothing')
  assert.deepEqual(spaceAttention(unplaced, ['mavu'], seeded), { unread: [], blocked: [] }, 'moving does not invent a completion')
  const finishedUnplaced = boardOf([], [{ ...pane('mavu', 'listening', { turn_end_id: 27 }), pane_id: '' }])
  assert.deepEqual(spaceAttention(finishedUnplaced, ['mavu'], seeded).unread, ['mavu'], 'a turn ended while unplaced')
  const backInPlace = boardOf([pane('mavu', 'listening', { turn_end_id: 27 })])
  assert.deepEqual(spaceAttention(backInPlace, ['mavu'], seeded).unread, ['mavu'], 'still unread once placed again')
  assert.deepEqual(spaceAttention(backInPlace, ['mavu'], markViewedRead(seeded, finishedUnplaced, ['mavu'])).unread, [], 'read while unplaced stays read')
})

test('seeding records never-seen open agents at their current turn end, silently, and waits for an id', () => {
  const board = boardOf([pane('mavu', 'listening', { turn_end_id: 10 }), pane('ziru', 'active')])
  const empty = {}
  assert.equal(seedReadMarkers(empty, undefined, ['mavu']), empty, 'no board: nothing to seed against')
  const seeded = seedReadMarkers(empty, board, ['mavu', 'ziru', 'ghost'])
  assert.deepEqual(seeded, { mavu: 10 }, 'no turn end yet: no marker, not a zero baseline')
  assert.equal(spaceAttention(board, ['mavu', 'ziru'], seeded).unread.length, 0)
  assert.equal(seedReadMarkers(seeded, board, ['mavu', 'ziru']), seeded, 'existing markers are kept and identity preserved')
  assert.deepEqual(seedReadMarkers({ mavu: 1 }, board, ['mavu']), { mavu: 1 }, 'a seen agent is never re-seeded over its unread turn')
})

test('a first turn end that arrives late is an unknown baseline, seeded silently rather than lit as new', () => {
  const before = boardOf([pane('ziru', 'listening')])
  const markers = seedReadMarkers({}, before, ['ziru'])
  assert.deepEqual(markers, {})
  const late = boardOf([pane('ziru', 'listening', { turn_end_id: 300 })])
  assert.equal(spaceAttention(late, ['ziru'], markers).unread.length, 0, 'unmarked is never unread')
  const seeded = seedReadMarkers(markers, late, ['ziru'])
  assert.deepEqual(seeded, { ziru: 300 })
  assert.deepEqual(spaceAttention(late, ['ziru'], seeded).unread, [])
  assert.deepEqual(spaceAttention(boardOf([pane('ziru', 'listening', { turn_end_id: 310 })]), ['ziru'], seeded).unread, ['ziru'], 'the next turn is new')
})

test('a missing turn_end_id keeps the marker and is never unread; blocked still shows', () => {
  const markers = { mavu: 40 }
  const missing = boardOf([pane('mavu', 'listening')])
  assert.deepEqual(spaceAttention(missing, ['mavu'], markers), { unread: [], blocked: [] })
  assert.equal(seedReadMarkers(markers, missing, ['mavu']), markers)
  assert.equal(markViewedRead(markers, missing, ['mavu']), markers, 'viewing without an id records nothing')
  assert.deepEqual(spaceAttention(boardOf([pane('mavu', 'blocked')]), ['mavu'], markers), { unread: [], blocked: ['mavu'] })
})

test('viewing records the latest turn end and never moves a marker backwards', () => {
  const board = boardOf([pane('mavu', 'listening', { turn_end_id: 10 }), pane('ziru', 'active', { turn_end_id: 1 })])
  const markers = { mavu: 1, ziru: 1 }
  const read = markViewedRead(markers, board, ['mavu', 'ziru'])
  assert.deepEqual(read, { mavu: 10, ziru: 1 })
  assert.equal(spaceAttention(board, ['mavu'], read).unread.length, 0)
  assert.equal(markViewedRead(read, board, ['mavu']), read)
  assert.equal(markViewedRead(markers, board, []), markers)
  assert.equal(markViewedRead({ mavu: 99 }, board, ['mavu'])['mavu'], 99, 'a reset incarnation does not rewind the marker')
})

test('pruning forgets only agents neither open nor on the board, and waits for a board', () => {
  const board = boardOf([pane('mavu', 'listening', { turn_end_id: 10, subagents: [pane('sub', 'listening')] })], [pane('lone', 'listening')])
  assert.deepEqual([...boardAgents(board)].sort(), ['lone', 'mavu', 'sub'])
  const markers = { mavu: 1, sub: 2, lone: 3, gone: 4, open: 5 }
  assert.deepEqual(pruneReadMarkers(markers, board, ['open']), { mavu: 1, sub: 2, lone: 3, open: 5 })
  assert.equal(pruneReadMarkers(markers, undefined, []), markers, 'no board: nothing is known gone')
  const kept = { mavu: 1, open: 5 }
  assert.equal(pruneReadMarkers(kept, board, ['open']), kept, 'identity preserved when nothing goes')
  const many = Object.fromEntries(Array.from({ length: 1000 }, (_, index) => [`open-${index}`, index + 1]))
  assert.equal(pruneReadMarkers(many, boardOf([]), Object.keys(many)), many, 'open agents are never evicted, however many')
})

test('labels read naturally for every count combination', () => {
  assert.equal(attentionLabel({ unread: ['a', 'b'], blocked: ['c'] }), '2 agents waiting, 1 blocked')
  assert.equal(attentionLabel({ unread: ['a'], blocked: [] }), '1 agent waiting')
  assert.equal(attentionLabel({ unread: [], blocked: ['c', 'd'] }), '2 agents blocked')
  assert.equal(attentionLabel({ unread: [], blocked: [] }), 'no agents waiting')
})

test('the folded total counts an agent open in two spaces once', () => {
  assert.deepEqual(totalAttention([{ unread: ['a', 'b'], blocked: ['c'] }, { unread: ['a'], blocked: ['c', 'd'] }]), { unread: ['a', 'b'], blocked: ['c', 'd'] })
})
