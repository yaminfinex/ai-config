import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

import { dockTabMenuFocusAction, dockTabMenuItems, dockTabMenuKeyAction, dockTabMenuNavigationIndex, isDockTabMenuKey } from '../src/features/workspace/dockTabMenuModel.ts'

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

test('dock tab menu arrow navigation wraps and Home and End jump', () => {
  assert.equal(dockTabMenuNavigationIndex('ArrowDown', 0, 3), 1)
  assert.equal(dockTabMenuNavigationIndex('ArrowDown', 2, 3), 0)
  assert.equal(dockTabMenuNavigationIndex('ArrowUp', 0, 3), 2)
  assert.equal(dockTabMenuNavigationIndex('Home', 2, 3), 0)
  assert.equal(dockTabMenuNavigationIndex('End', 0, 3), 2)
  assert.equal(dockTabMenuNavigationIndex('x', 1, 3), null)
})

test('dock tab menu keys outside the menu dismiss it and are never consumed', () => {
  assert.deepEqual(dockTabMenuKeyAction({ key: 'ArrowDown', insideMenu: false, current: -1, count: 3 }), { kind: 'dismiss' })
  assert.deepEqual(dockTabMenuKeyAction({ key: 'Escape', insideMenu: false, current: 0, count: 3 }), { kind: 'dismiss' })
  assert.deepEqual(dockTabMenuKeyAction({ key: 'k', insideMenu: false, current: 0, count: 3 }), { kind: 'dismiss' })
})

test('dock tab menu keys inside the menu navigate, Escape closes, other keys are ignored', () => {
  assert.deepEqual(dockTabMenuKeyAction({ key: 'ArrowDown', insideMenu: true, current: 0, count: 3 }), { kind: 'focus', index: 1 })
  assert.deepEqual(dockTabMenuKeyAction({ key: 'End', insideMenu: true, current: 0, count: 3 }), { kind: 'focus', index: 2 })
  assert.deepEqual(dockTabMenuKeyAction({ key: 'Escape', insideMenu: true, current: 0, count: 3 }), { kind: 'close' })
  assert.equal(dockTabMenuKeyAction({ key: 'k', insideMenu: true, current: 0, count: 3 }), null)
})

test('dock tab menu stays open while focus is on or moves within the menu, and closes only when focus lands outside', () => {
  assert.equal(dockTabMenuFocusAction(true), 'keep')
  assert.equal(dockTabMenuFocusAction(false), 'dismiss')
  const source = readFileSync(new URL('../src/features/workspace/DockTabMenu.tsx', import.meta.url), 'utf8')
  // Dismissal is driven by focusin only: a freshly opened menu whose tab keeps focus raises no event and stays open.
  assert.match(source, /document\.addEventListener\('focusin', onFocusIn, true\)/)
  assert.match(source, /dockTabMenuFocusAction\(Boolean\(menuRef\.current\?\.contains\(event\.target as Node\)\)\) === 'dismiss'\) close\(false\)/)
})
