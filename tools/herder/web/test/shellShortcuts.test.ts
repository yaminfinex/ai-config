import assert from 'node:assert/strict'
import test from 'node:test'
import { isClosePanelShortcut, isEditableShortcutTarget, isShortcutReferenceShortcut } from '../src/features/layout/shellShortcuts.ts'

test('Alt+W is the pane-close shortcut and browser Ctrl/Cmd+W is never claimed', () => {
  assert.equal(isClosePanelShortcut({ key: 'w', altKey: true, ctrlKey: false, metaKey: false, shiftKey: false }), true)
  assert.equal(isClosePanelShortcut({ key: 'W', altKey: true, ctrlKey: false, metaKey: false, shiftKey: false }), true)
  assert.equal(isClosePanelShortcut({ key: 'w', altKey: false, ctrlKey: true, metaKey: false, shiftKey: false }), false)
  assert.equal(isClosePanelShortcut({ key: 'w', altKey: false, ctrlKey: false, metaKey: true, shiftKey: false }), false)
})

test('Alt+W with an editable target does not invoke panel close', () => {
  const previousHTMLElement = globalThis.HTMLElement
  class TestHTMLElement {
    isContentEditable = false
    closest() { return this }
  }
  globalThis.HTMLElement = TestHTMLElement as unknown as typeof HTMLElement
  try {
    const event = { key: 'w', altKey: true, ctrlKey: false, metaKey: false, shiftKey: false, target: new TestHTMLElement() as unknown as EventTarget }
    let closeCalls = 0
    if (isClosePanelShortcut(event) && !isEditableShortcutTarget(event.target)) closeCalls += 1
    assert.equal(closeCalls, 0)
  } finally {
    globalThis.HTMLElement = previousHTMLElement
  }
})

test('? opens the reference only without browser modifiers', () => {
  assert.equal(isShortcutReferenceShortcut({ key: '?', altKey: false, ctrlKey: false, metaKey: false }), true)
  assert.equal(isShortcutReferenceShortcut({ key: '?', altKey: false, ctrlKey: true, metaKey: false }), false)
})
