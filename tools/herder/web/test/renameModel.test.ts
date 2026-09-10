import assert from 'node:assert/strict'
import test from 'node:test'

import { beginRename, prepareRename, renameValue } from '../src/features/sidebar/renameModel.ts'

test('begin pre-fills the current title and changed commits trim their payload', () => {
  const begun = beginRename('impl-pini', 'display names')
  assert.equal(begun.value, 'display names')
  assert.equal(prepareRename(renameValue(begun, '  better name  ')), 'better name')
})

test('empty and unchanged values cancel without a request', () => {
  assert.equal(prepareRename(renameValue(beginRename('impl-pini', 'display names'), ' display names ')), null)
  assert.equal(prepareRename(renameValue(beginRename('impl-pini', 'display names'), '  ')), null)
  assert.equal(prepareRename(renameValue(beginRename('impl-pini'), '   ')), null)
})
