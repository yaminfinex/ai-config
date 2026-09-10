import assert from 'node:assert/strict'
import test from 'node:test'

import { beginRename, cancelRename, prepareRename, renameRefused, renameValue } from '../src/features/sidebar/renameModel.ts'

test('begin pre-fills the current title and changed commits trim their payload', () => {
  const begun = beginRename('impl-pini', 'display names')
  assert.equal(begun.value, 'display names')
  assert.deepEqual(prepareRename(renameValue(begun, '  better name  ')), { title: 'better name' })
})

test('empty and unchanged values cancel without a request', () => {
  assert.deepEqual(prepareRename(renameValue(beginRename('impl-pini', 'display names'), ' display names ')), {})
  assert.deepEqual(prepareRename(renameValue(beginRename('impl-pini'), '   ')), {})
})

test('Escape discards state and refusals keep the edited value open', () => {
  const editing = renameValue(beginRename('impl-pini'), 'new title')
  assert.equal(cancelRename(), null)
  assert.deepEqual(renameRefused(editing, { inline: 'refused' }), { ...editing, problem: { inline: 'refused' } })
})
