import assert from 'node:assert/strict'
import test from 'node:test'

import { dockTabMenuItems, isDockTabMenuKey } from '../src/features/workspace/dockTabMenuModel.ts'

const spaces = [
  { id: 'main', name: 'main', order: 0, created: 0, updated: 0 },
  { id: 'review', name: 'review', order: 1, created: 0, updated: 0 },
]

test('dock tab menu contains only other spaces and send-to-new', () => {
  assert.deepEqual(dockTabMenuItems(spaces, 'main'), [
    { id: 'review', label: 'Send to review', kind: 'space' },
    { id: 'new', label: 'Send to new space', kind: 'new' },
  ])
})

test('dock tab menu recognizes the platform context-menu keys only', () => {
  assert.equal(isDockTabMenuKey({ key: 'ContextMenu', shiftKey: false }), true)
  assert.equal(isDockTabMenuKey({ key: 'F10', shiftKey: true }), true)
  assert.equal(isDockTabMenuKey({ key: 'F10', shiftKey: false }), false)
  assert.equal(isDockTabMenuKey({ key: 'Enter', shiftKey: false }), false)
})
