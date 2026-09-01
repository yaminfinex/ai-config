import assert from 'node:assert/strict'
import test from 'node:test'
import { bindShellShortcuts, isEditableShortcutTarget, shortcutLabels, type ShellShortcutActions } from '../src/features/layout/shellShortcuts.ts'

type KeyboardInit = {
  key: string
  code: string
  altKey?: boolean
  ctrlKey?: boolean
  metaKey?: boolean
  shiftKey?: boolean
  repeat?: boolean
  isComposing?: boolean
}

class TestKeyboardEvent extends Event {
  readonly key: string
  readonly code: string
  readonly altKey: boolean
  readonly ctrlKey: boolean
  readonly metaKey: boolean
  readonly shiftKey: boolean
  readonly repeat: boolean
  readonly isComposing: boolean

  constructor(type: string, init: KeyboardInit) {
    super(type, { cancelable: true, bubbles: true })
    this.key = init.key
    this.code = init.code
    this.altKey = init.altKey ?? false
    this.ctrlKey = init.ctrlKey ?? false
    this.metaKey = init.metaKey ?? false
    this.shiftKey = init.shiftKey ?? false
    this.repeat = init.repeat ?? false
    this.isComposing = init.isComposing ?? false
  }

  getModifierState(modifier: string) {
    if (modifier === 'Alt' || modifier === 'AltGraph') return this.altKey
    if (modifier === 'Control') return this.ctrlKey
    if (modifier === 'Meta') return this.metaKey
    if (modifier === 'Shift') return this.shiftKey
    return false
  }
}

function actions(calls: string[]): ShellShortcutActions {
  return {
    quickOpen: () => { calls.push('quick-open') },
    closePanel: () => { calls.push('close'); return true },
    openShortcutReference: () => { calls.push('reference') },
    closeShortcutReference: () => { calls.push('escape'); return true },
    switchTab: (direction) => { calls.push(`tab:${direction}`); return true },
    switchSpace: (direction) => { calls.push(`space:${direction}`); return true },
    focusFleet: () => { calls.push('fleet'); return true },
    toggleNotesRail: () => { calls.push('notes'); return true },
    focusComposer: () => { calls.push('composer'); return true },
    goToTop: () => { calls.push('top'); return true },
    goToBottom: () => { calls.push('bottom'); return true },
    toggleMaximize: () => { calls.push('maximize'); return true },
  }
}

function dispatch(target: EventTarget, init: KeyboardInit) {
  const event = new TestKeyboardEvent('keydown', init)
  target.dispatchEvent(event)
  return event
}

test('tinykeys dispatches Mac Option character events by physical code', () => {
  const target = new EventTarget()
  const calls: string[] = []
  const unsubscribe = bindShellShortcuts(target as unknown as Window, actions(calls), 'Macintosh')
  try {
    assert.equal(dispatch(target, { key: '∑', code: 'KeyW', altKey: true }).defaultPrevented, true)
    assert.equal(dispatch(target, { key: '¡', code: 'Digit1', altKey: true }).defaultPrevented, true)
    assert.equal(dispatch(target, { key: '™', code: 'Digit2', altKey: true }).defaultPrevented, true)
    assert.equal(dispatch(target, { key: '£', code: 'Digit3', altKey: true }).defaultPrevented, true)
    assert.equal(dispatch(target, { key: '¢', code: 'Digit4', altKey: true }).defaultPrevented, false)
    assert.deepEqual(calls, ['close', 'fleet', 'composer', 'notes'])
  } finally {
    unsubscribe()
  }
})

test('editable targets stay dead through the real tinykeys-bound handler', () => {
  const previousHTMLElement = globalThis.HTMLElement
  class TestHTMLElement extends EventTarget {
    isContentEditable = false
    closest() { return this }
  }
  globalThis.HTMLElement = TestHTMLElement as unknown as typeof HTMLElement
  const target = new TestHTMLElement()
  const calls: string[] = []
  const unsubscribe = bindShellShortcuts(target as unknown as HTMLElement, actions(calls), 'Macintosh')
  try {
    dispatch(target, { key: '∑', code: 'KeyW', altKey: true })
    dispatch(target, { key: '¡', code: 'Digit1', altKey: true })
    dispatch(target, { key: '™', code: 'Digit2', altKey: true })
    dispatch(target, { key: '£', code: 'Digit3', altKey: true })
    dispatch(target, { key: '¢', code: 'Digit4', altKey: true })
    dispatch(target, { key: 'ArrowLeft', code: 'ArrowLeft', altKey: true })
    dispatch(target, { key: 'ArrowUp', code: 'ArrowUp', altKey: true })
    dispatch(target, { key: 'ArrowDown', code: 'ArrowDown', altKey: true })
    dispatch(target, { key: 'Enter', code: 'Enter', altKey: true })
    assert.deepEqual(calls, [])
    assert.equal(isEditableShortcutTarget(target), true)
  } finally {
    unsubscribe()
    globalThis.HTMLElement = previousHTMLElement
  }
})

test('Option arrows and legacy Control Page aliases switch tabs', () => {
  const target = new EventTarget()
  const calls: string[] = []
  const unsubscribe = bindShellShortcuts(target as unknown as Window, actions(calls), 'Linux')
  try {
    dispatch(target, { key: 'ArrowLeft', code: 'ArrowLeft', altKey: true })
    dispatch(target, { key: 'ArrowRight', code: 'ArrowRight', altKey: true })
    dispatch(target, { key: 'PageUp', code: 'PageUp', ctrlKey: true })
    dispatch(target, { key: 'PageDown', code: 'PageDown', ctrlKey: true })
    assert.deepEqual(calls, ['tab:previous', 'tab:next', 'tab:previous', 'tab:next'])
  } finally {
    unsubscribe()
  }
})

test('Shift-Option arrows switch spaces without replacing tab shortcuts', () => {
  const target = new EventTarget()
  const calls: string[] = []
  const unsubscribe = bindShellShortcuts(target as unknown as Window, actions(calls), 'Macintosh')
  try {
    dispatch(target, { key: 'ArrowLeft', code: 'ArrowLeft', altKey: true, shiftKey: true })
    dispatch(target, { key: 'ArrowRight', code: 'ArrowRight', altKey: true, shiftKey: true })
    dispatch(target, { key: 'ArrowLeft', code: 'ArrowLeft', altKey: true })
    assert.deepEqual(calls, ['space:previous', 'space:next', 'tab:previous'])
  } finally { unsubscribe() }
})

test('Option up and down use physical arrow codes for transcript jumps', () => {
  const target = new EventTarget()
  const calls: string[] = []
  const unsubscribe = bindShellShortcuts(target as unknown as Window, actions(calls), 'Macintosh')
  try {
    dispatch(target, { key: 'Dead', code: 'ArrowUp', altKey: true })
    dispatch(target, { key: 'Dead', code: 'ArrowDown', altKey: true })
    assert.deepEqual(calls, ['top', 'bottom'])
  } finally {
    unsubscribe()
  }
})

test('Option Enter uses its physical code for the maximize toggle', () => {
  const target = new EventTarget()
  const calls: string[] = []
  const unsubscribe = bindShellShortcuts(target as unknown as Window, actions(calls), 'Macintosh')
  try {
    dispatch(target, { key: 'Dead', code: 'Enter', altKey: true })
    assert.deepEqual(calls, ['maximize'])
  } finally {
    unsubscribe()
  }
})

test('shortcut reference labels are platform-aware and Escape stays neutral', () => {
  assert.deepEqual(
    { top: shortcutLabels('Macintosh').goToTop, bottom: shortcutLabels('Macintosh').goToBottom, maximize: shortcutLabels('Macintosh').toggleMaximize, leave: shortcutLabels('Macintosh').leaveComposer },
    { top: '⌥↑', bottom: '⌥↓', maximize: '⌥⏎', leave: 'Esc' },
  )
  assert.deepEqual(
    { top: shortcutLabels('Linux').goToTop, bottom: shortcutLabels('Linux').goToBottom, maximize: shortcutLabels('Linux').toggleMaximize, leave: shortcutLabels('Linux').leaveComposer },
    { top: 'Alt+Up', bottom: 'Alt+Down', maximize: 'Alt+Enter', leave: 'Esc' },
  )
  assert.equal(shortcutLabels('Macintosh').toggleNotesRail, '⌥3')
  assert.equal(shortcutLabels('Linux').toggleNotesRail, 'Alt+3')
  assert.equal(shortcutLabels('Macintosh').switchSpaces, '⇧⌥← / ⇧⌥→')
  assert.equal(shortcutLabels('Linux').switchSpaces, 'Shift+Alt+Left / Shift+Alt+Right')
  assert.doesNotMatch(shortcutLabels('Macintosh').switchTabs, /legacy/i)
  assert.doesNotMatch(shortcutLabels('Linux').switchTabs, /legacy/i)
})

test('unclaimed actions do not prevent browser defaults', () => {
  const target = new EventTarget()
  const calls: string[] = []
  const handlers = actions(calls)
  handlers.closePanel = () => false
  handlers.switchTab = () => false
  const unsubscribe = bindShellShortcuts(target as unknown as Window, handlers, 'Macintosh')
  try {
    assert.equal(dispatch(target, { key: '∑', code: 'KeyW', altKey: true }).defaultPrevented, false)
    assert.equal(dispatch(target, { key: 'ArrowLeft', code: 'ArrowLeft', altKey: true }).defaultPrevented, false)
  } finally {
    unsubscribe()
  }
})
