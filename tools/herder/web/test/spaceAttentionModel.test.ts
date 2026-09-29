import assert from 'node:assert/strict'
import test from 'node:test'
import {
  agentAttention,
  agentsInDock,
  attentionLabel,
  markViewedRead,
  seedReadMarkers,
  spaceAttention,
  storedSpaceAgents,
  totalAttention,
  turnFingerprint,
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

test('the turn fingerprint is session plus context while listening, and nothing otherwise', () => {
  assert.equal(turnFingerprint(pane('a', 'listening', { agent_session: 's1', context_used: 1200 })), 's1:1200')
  assert.equal(turnFingerprint(pane('a', 'listening', { context_used: 1200 })), ':1200')
  assert.equal(turnFingerprint(pane('a', 'listening')), null, 'no vitals yet')
  assert.equal(turnFingerprint(pane('a', 'active', { agent_session: 's1', context_used: 1200 })), null, 'mid-turn')
  assert.equal(turnFingerprint(pane('a', 'blocked', { agent_session: 's1', context_used: 1200 })), null)
  assert.equal(turnFingerprint(undefined), null)
})

test('an agent is unread only when a finished turn differs from what this browser saw', () => {
  const listening = pane('a', 'listening', { agent_session: 's1', context_used: 900 })
  assert.equal(agentAttention(listening, 's1:500'), 'unread')
  assert.equal(agentAttention(listening, ''), 'unread', 'seeded before any turn ended, then one did')
  assert.equal(agentAttention(listening, 's1:900'), null)
  assert.equal(agentAttention(listening, undefined), null, 'never seen: seeding decides, not the first render')
  assert.equal(agentAttention(pane('a', 'listening', { agent_session: 's2', context_used: 900 }), 's1:900'), 'unread', 'a new session is a new turn')
  assert.equal(agentAttention(pane('a', 'active', { agent_session: 's1', context_used: 1400 }), 's1:500'), null, 'still working')
})

test('blocked always shows, read or not; retired and unknown agents never count', () => {
  assert.equal(agentAttention(pane('a', 'blocked'), undefined), 'blocked')
  assert.equal(agentAttention(pane('a', 'blocked', { agent_session: 's1', context_used: 5 }), 's1:5'), 'blocked')
  assert.equal(agentAttention(pane('a', 'stopped', { agent_session: 's1', context_used: 5 }), 's1:1'), null)
  assert.equal(agentAttention(pane('a', 'retired', { agent_session: 's1', context_used: 5 }), 's1:1'), null)
  assert.equal(agentAttention(undefined, 's1:1'), null)
})

test('space attention collects unread and blocked open agents from the board, subagents included', () => {
  const board = boardOf([
    pane('mavu', 'listening', { agent_session: 's', context_used: 10 }),
    pane('ziru', 'blocked'),
    pane('kobe', 'listening', { agent_session: 's', context_used: 3, subagents: [pane('kobe-sub', 'blocked')] }),
  ], [pane('lone', 'listening', { agent_session: 'u', context_used: 8 })])
  const markers = { mavu: 's:1', ziru: 's:1', kobe: 's:3', lone: 'u:1' }
  assert.deepEqual(spaceAttention(board, ['mavu', 'ziru', 'kobe', 'kobe-sub', 'lone', 'ghost'], markers), {
    unread: ['mavu', 'lone'], blocked: ['ziru', 'kobe-sub'],
  })
  assert.deepEqual(spaceAttention(undefined, ['mavu'], markers), { unread: [], blocked: [] })
})

test('seeding marks never-seen open agents at their current turn and waits for a board', () => {
  const board = boardOf([pane('mavu', 'listening', { agent_session: 's', context_used: 10 }), pane('ziru', 'active')])
  const empty = {}
  assert.equal(seedReadMarkers(empty, undefined, ['mavu']), empty, 'no board: nothing to seed against')
  const seeded = seedReadMarkers(empty, board, ['mavu', 'ziru', 'ghost'])
  assert.deepEqual(seeded, { mavu: 's:10', ziru: '' })
  assert.equal(seedReadMarkers(seeded, board, ['mavu', 'ziru']), seeded, 'existing markers are kept and identity preserved')
  assert.deepEqual(seedReadMarkers({ mavu: 's:1' }, board, ['mavu']), { mavu: 's:1' }, 'a seen agent is never re-seeded over its unread turn')
})

test('viewing records the current finished turn; a mid-turn view keeps the old marker', () => {
  const board = boardOf([pane('mavu', 'listening', { agent_session: 's', context_used: 10 }), pane('ziru', 'active')])
  const markers = { mavu: 's:1', ziru: 's:1' }
  const read = markViewedRead(markers, board, ['mavu', 'ziru'])
  assert.deepEqual(read, { ziru: 's:1', mavu: 's:10' })
  assert.equal(spaceAttention(board, ['mavu'], read).unread.length, 0)
  assert.equal(markViewedRead(read, board, ['mavu']), read)
  assert.equal(markViewedRead(markers, board, []), markers)
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
