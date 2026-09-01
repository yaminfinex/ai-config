import assert from 'node:assert/strict'
import test from 'node:test'

import { visibleSpaceIDs } from '../src/features/spaces/spaceOverflowModel.ts'

const ids = ['a', 'b', 'c', 'd']
const widths = { a: 60, b: 60, c: 60, d: 60 }

test('space overflow shows all chips when they fit', () => {
  assert.deepEqual(visibleSpaceIDs(ids, 'c', widths, 300, 24, 50), { visible: ids, hidden: [] })
})

test('space overflow always retains the active chip and reports hidden spaces', () => {
  const result = visibleSpaceIDs(ids, 'd', widths, 150, 24, 50)
  assert.ok(result.visible.includes('d'))
  assert.deepEqual([...result.visible, ...result.hidden].sort(), [...ids].sort())
  assert.equal(result.hidden.length, 3)
})
