import assert from 'node:assert/strict'
import test from 'node:test'

import { quickOpenRows, reassignCandidates, reassignSelection } from '../src/features/sidebar/reassignModel.ts'
import type { Row } from '../src/types.ts'

const row = (agent: string, extra: Partial<Row> = {}): Row => ({
  pane_id: '-', agent, tool: 'codex', herdr_status: '-', bus_status: 'active', gap: 'no visible pane', manager_state: 'live', ...extra,
})

test('reassign candidates share reparent refusals and always end with human', () => {
  const rows = [
    row('subject'), row('zulu'), row('alpha'), row('subject-child'), row('ended', { manager_state: 'ended' }),
    row('off-bus', { bus_status: '-' }), row('task', { parent_agent: 'good' }),
  ]
  const candidates = reassignCandidates('subject', rows, () => new Set(['subject-child']), '')
  assert.deepEqual(candidates.map((candidate) => candidate.target), ['alpha', 'zulu', 'human'])
  assert.deepEqual(candidates.at(-1), { kind: 'reassign', subject: 'subject', target: 'human', label: 'human (adopt)' })
})

test('reassign selection leaves a no-match query inert and selects an explicit human prefix', () => {
  const candidates = reassignCandidates('subject', [row('subject'), row('other')], () => new Set(), 'typo')
  assert.equal(reassignSelection(candidates, 'typo'), null)
  assert.equal(reassignSelection(reassignCandidates('subject', [row('subject'), row('other')], () => new Set(), 'hum'), 'hum'),
    'action:reassign-target:subject:human')
})

test('quickOpenRows returns only reassign rows in reassign mode', () => {
  const rows = [row('subject'), row('other')]
  const result = quickOpenRows({ kind: 'reassign', subject: 'subject' }, 'other', {
    spaces: [{ id: 'main', name: 'main', order: 0, created: 0, updated: 0 }],
    agents: ['subject', 'other'], atSpaceCap: false, hasActivePanel: true, activeSpaceID: 'main',
    reassignSubject: 'subject', rows, descendantsOf: () => new Set(),
  })
  assert.ok(result.length > 0)
  assert.ok(result.every((candidate) => candidate.kind === 'reassign'))
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
