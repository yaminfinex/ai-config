import assert from 'node:assert/strict'
import test from 'node:test'
import { isClosePanelShortcut, isShortcutReferenceShortcut } from '../src/features/layout/shellShortcuts.ts'

test('Alt+W is the pane-close shortcut and browser Ctrl/Cmd+W is never claimed', () => {
  assert.equal(isClosePanelShortcut({ key: 'w', altKey: true, ctrlKey: false, metaKey: false, shiftKey: false }), true)
  assert.equal(isClosePanelShortcut({ key: 'W', altKey: true, ctrlKey: false, metaKey: false, shiftKey: false }), true)
  assert.equal(isClosePanelShortcut({ key: 'w', altKey: false, ctrlKey: true, metaKey: false, shiftKey: false }), false)
  assert.equal(isClosePanelShortcut({ key: 'w', altKey: false, ctrlKey: false, metaKey: true, shiftKey: false }), false)
})

test('? opens the reference only without browser modifiers', () => {
  assert.equal(isShortcutReferenceShortcut({ key: '?', altKey: false, ctrlKey: false, metaKey: false }), true)
  assert.equal(isShortcutReferenceShortcut({ key: '?', altKey: false, ctrlKey: true, metaKey: false }), false)
})
