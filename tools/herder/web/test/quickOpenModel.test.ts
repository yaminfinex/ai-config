import assert from 'node:assert/strict'
import test from 'node:test'

import { quickOpenActionRows, quickOpenEnterTarget } from '../src/features/files/quickOpenModel.ts'

const spaces = [
  { id: 'main', name: 'main', order: 0, created: 0, updated: 0 },
  { id: 'review', name: 'review queue', order: 1, created: 0, updated: 0 },
  { id: 'notes', name: 'weekly review', order: 2, created: 0, updated: 0 },
]

test('quick open ranks exact, prefix, then substring within spaces before agents', () => {
  assert.deepEqual(quickOpenActionRows('review', spaces, ['reviewer', 'my-review-agent', 'review'], false), [
    { kind: 'space', id: 'review', label: 'review queue' },
    { kind: 'space', id: 'notes', label: 'weekly review' },
    { kind: 'create', name: 'review', label: 'Create space “review”' },
    { kind: 'agent', name: 'review', label: 'review' },
    { kind: 'agent', name: 'reviewer', label: 'reviewer' },
    { kind: 'agent', name: 'my-review-agent', label: 'my-review-agent' },
  ])
})

test('an exact space suppresses create and the cap suppresses it deterministically', () => {
  assert.equal(quickOpenActionRows('main', spaces, [], false).some((row) => row.kind === 'create'), false)
  assert.equal(quickOpenActionRows('new place', spaces, [], true).some((row) => row.kind === 'create'), false)
  assert.equal(quickOpenActionRows('', spaces, [], false).some((row) => row.kind === 'create'), false)
})

test('Enter prefers an exact live agent over the synthetic create command', () => {
  const rows = quickOpenActionRows('podi', spaces, ['podi'], false)
  const target = quickOpenEnterTarget(rows, 'podi', -1, true)
  assert.deepEqual(target, { kind: 'action', index: rows.findIndex((row) => row.kind === 'agent') })
})

test('reflexive Enter falls through substring action matches to the file candidate', () => {
  const rows = quickOpenActionRows('review', spaces, ['my-review-agent'], false)
  assert.deepEqual(quickOpenEnterTarget(rows, 'review', -1, true), { kind: 'file' })
})

test('reflexive Enter switches only an exact space or opens only an exact agent', () => {
  const spaceRows = quickOpenActionRows('main', spaces, ['main-agent'], false)
  assert.deepEqual(quickOpenEnterTarget(spaceRows, 'main', -1, false), { kind: 'action', index: 0 })

  const agentRows = quickOpenActionRows('podi', spaces, ['podi'], false)
  assert.deepEqual(quickOpenEnterTarget(agentRows, 'podi', -1, false), {
    kind: 'action', index: agentRows.findIndex((row) => row.kind === 'agent'),
  })
})

test('create is arrow-select only and an empty reflexive Enter does nothing', () => {
  const rows = quickOpenActionRows('new place', spaces, [], false)
  const createIndex = rows.findIndex((row) => row.kind === 'create')
  assert.equal(quickOpenEnterTarget(rows, 'new place', -1, false), null)
  assert.deepEqual(quickOpenEnterTarget(rows, 'new place', createIndex, false), { kind: 'action', index: createIndex })
  assert.equal(quickOpenEnterTarget(quickOpenActionRows('', spaces, [], false), '', -1, false), null)
})

test('pane-send rows require an active pane, exclude the current space, and preserve reflexive Enter', () => {
  assert.equal(quickOpenActionRows('send', spaces, [], false).some((row) => row.kind.startsWith('send')), false)
  const rows = quickOpenActionRows('send', spaces, [], false, true, 'main')
  assert.deepEqual(rows.filter((row) => row.kind === 'send-space').map((row) => row.label), [
    'Send this pane to review queue', 'Send this pane to weekly review',
  ])
  assert.equal(rows.some((row) => row.kind === 'send-new'), true)
  assert.deepEqual(quickOpenEnterTarget(rows, 'send', -1, true), { kind: 'file' })
})
