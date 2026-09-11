import assert from 'node:assert/strict'
import test from 'node:test'
import { dropAssignment, reparentDrop } from '../src/features/sidebar/reparentModel.ts'
import type { SidebarNode } from '../src/features/sidebar/sidebarNodes.ts'
import type { Row } from '../src/types.ts'

const row = (agent: string, manager_state: Row['manager_state'] = 'live'): Row => ({ pane_id: '-', agent, tool: 'codex', herdr_status: '-', bus_status: 'active', gap: 'no visible pane', manager_state })
const nodes = new Map<string, SidebarNode>([
  ['tree-root', { id: 'tree-root', kind: 'root', name: 'Fleet', children: ['agent:a', 'agent:d', 'tombstone:x', 'terminals'] }],
  ['agent:a', { id: 'agent:a', kind: 'agent', name: 'a', children: ['agent:b'], pane: row('a') }],
  ['agent:b', { id: 'agent:b', kind: 'subagent', name: 'b', children: [], pane: row('b') }],
  ['agent:d', { id: 'agent:d', kind: 'agent', name: 'd', children: [], pane: row('d') }],
  ['agent:ended', { id: 'agent:ended', kind: 'agent', name: 'ended', children: [], pane: row('ended', 'ended') }],
  ['tombstone:x', { id: 'tombstone:x', kind: 'tombstone', name: 'x', children: [] }],
  ['terminal:p1', { id: 'terminal:p1', kind: 'pane', name: 'shell', children: [], pane: { ...row('-'), bus_status: '-' } }],
])

test('supervision drops produce one assignment intent', () => {
  assert.deepEqual(reparentDrop('supervision', 'agent:a', 'agent:d', nodes), { name: 'a', assignment: { manager: 'd' } })
  assert.deepEqual(reparentDrop('supervision', 'agent:a', null, nodes), { name: 'a', assignment: { manager: 'human' } })
})

test('one drop submits exactly one assignment', () => {
  const submitted: unknown[] = []
  const accepted = dropAssignment('supervision', 'agent:a', 'agent:d', nodes, (name, assignment) => { submitted.push({ name, assignment }) })
  assert.equal(accepted, true)
  assert.deepEqual(submitted, [{ name: 'a', assignment: { manager: 'd' } }])
})

test('drop refusal table protects supervision topology', () => {
  for (const [view, source, target, refusal] of [
    ['placement', 'agent:a', 'agent:d', 'placement view'],
    ['supervision', 'agent:a', 'agent:a', 'self'],
    ['supervision', 'agent:a', 'agent:b', 'descendant'],
    ['supervision', 'agent:a', 'tombstone:x', 'tombstone'],
    ['supervision', 'agent:a', 'terminal:p1', 'terminal'],
    ['supervision', 'agent:a', 'agent:ended', 'target is not a live agent'],
  ] as const) assert.deepEqual(reparentDrop(view, source, target, nodes), { refusal })
})
