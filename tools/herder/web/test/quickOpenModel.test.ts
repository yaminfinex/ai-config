import assert from 'node:assert/strict'
import test from 'node:test'

import { quickOpenActionRows, quickOpenDefaultActionIndex } from '../src/features/files/quickOpenModel.ts'

const spaces = [
  { id: 'main', name: 'main', order: 0, created: 0, updated: 0 },
  { id: 'review', name: 'review queue', order: 1, created: 0, updated: 0 },
  { id: 'notes', name: 'weekly review', order: 2, created: 0, updated: 0 },
]

test('quick open ranks exact, prefix, then substring within spaces before agents', () => {
  assert.deepEqual(quickOpenActionRows('review', spaces, ['review', 'reviewer', 'my-review-agent'], false), [
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
  assert.equal(rows[quickOpenDefaultActionIndex(rows, 'podi')]?.kind, 'agent')
})
