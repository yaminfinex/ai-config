import assert from 'node:assert/strict'
import test from 'node:test'

import { reassignCandidates } from '../src/features/sidebar/reassignModel.ts'
import type { Row } from '../src/types.ts'

const row = (agent: string, extra: Partial<Row> = {}): Row => ({
  pane_id: '-', agent, tool: 'codex', herdr_status: '-', bus_status: 'active', gap: 'no visible pane', manager_state: 'live', ...extra,
})

test('reassign candidates share reparent refusals and always end with human', () => {
  const rows = [
    row('subject'), row('good'), row('subject-child'), row('ended', { manager_state: 'ended' }),
    row('off-bus', { bus_status: '-' }), row('task', { parent_agent: 'good' }),
  ]
  const candidates = reassignCandidates('subject', rows, () => new Set(['subject-child']), '')
  assert.deepEqual(candidates.map((candidate) => candidate.target), ['good', 'human'])
  assert.deepEqual(candidates.at(-1), { kind: 'reassign', subject: 'subject', target: 'human', label: 'human (adopt)' })
})

test('reassign candidates match names or titles and rank an exact name first', () => {
  const rows = [
    row('subject'),
    row('contains-needle', { title: 'builder' }),
    row('needle', { title: 'different title' }),
    row('other', { title: 'Needle reviewer' }),
  ]
  const matches = reassignCandidates('subject', rows, () => new Set(), 'needle')
  assert.deepEqual(matches.map((candidate) => candidate.target), ['needle', 'contains-needle', 'other', 'human'])
  assert.equal(matches[2].title, 'Needle reviewer')
})
