import assert from 'node:assert/strict'
import test from 'node:test'
import { updatePanelRecord } from '../src/features/workspace/usePanelRecords.ts'

test('panel records copy on change and retain identity for no-ops', () => {
  const initial = { agent: 'listening' }
  assert.equal(updatePanelRecord(initial, 'agent', 'listening'), initial)

  const changed = updatePanelRecord(initial, 'agent', 'active')
  assert.deepEqual(changed, { agent: 'active' })
  assert.notEqual(changed, initial)

  assert.equal(updatePanelRecord(initial, 'missing', undefined), initial)
  assert.deepEqual(updatePanelRecord(initial, 'agent', undefined), {})
})

test('panel records support updates from the previous keyed value', () => {
  const initial = { file: { mode: 'current', line: 4 } }
  const changed = updatePanelRecord(initial, 'file', (previous) => ({ ...previous!, line: previous!.line + 1 }))
  assert.deepEqual(changed, { file: { mode: 'current', line: 5 } })
})

test('panel records accept domain equality without replacing equal values', () => {
  const initial = { file: { mode: 'diff', base: 'branch' } }
  const equal = (left: typeof initial.file, right: typeof initial.file) => left.mode === right.mode && left.base === right.base
  assert.equal(updatePanelRecord(initial, 'file', { mode: 'diff', base: 'branch' }, equal), initial)
})
