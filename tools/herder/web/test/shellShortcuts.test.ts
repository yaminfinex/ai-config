import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { bindShellShortcuts, bindSpaceSwitcher, isEditableShortcutTarget, shortcutLabels, switcherKeyIntent, type ShellShortcutActions } from '../src/features/layout/shellShortcuts.ts'
import { idleSwitcher, reduceSwitcher, type SwitcherEvent, type SwitcherState } from '../src/features/spaces/spaceSwitcherModel.ts'

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

test('Cmd and Ctrl arrows remain unclaimed for browser back and forward', () => {
  const target = new EventTarget()
  const calls: string[] = []
  const unsubscribe = bindShellShortcuts(target as unknown as Window, actions(calls), 'Macintosh')
  try {
    assert.equal(dispatch(target, { key: 'ArrowLeft', code: 'ArrowLeft', ctrlKey: true }).defaultPrevented, false)
    assert.equal(dispatch(target, { key: 'ArrowRight', code: 'ArrowRight', ctrlKey: true }).defaultPrevented, false)
    assert.equal(dispatch(target, { key: 'ArrowLeft', code: 'ArrowLeft', metaKey: true }).defaultPrevented, false)
    assert.equal(dispatch(target, { key: 'ArrowRight', code: 'ArrowRight', metaKey: true }).defaultPrevented, false)
    assert.deepEqual(calls, [])
  } finally { unsubscribe() }
})

test('shell shortcut source cannot reintroduce Meta or Control arrow bindings', () => {
  const source = readFileSync(new URL('../src/features/layout/shellShortcuts.ts', import.meta.url), 'utf8')
  assert.doesNotMatch(source, /['"](?:Meta|Control)\+Arrow(?:Left|Right)['"]\s*:/)
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
  assert.equal(shortcutLabels('Macintosh').spaceSwitcher, '⌥Tab / ⇧⌥Tab')
  assert.equal(shortcutLabels('Linux').spaceSwitcher, 'Alt+Tab / Shift+Alt+Tab')
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

// Node's EventTarget keeps a capture listener after removeEventListener(..,
// true); browsers remove it. This target drops the phase flag so disposal
// is tested the way a browser window behaves.
class WindowLikeTarget extends EventTarget {
  override addEventListener(type: string, listener: EventListenerOrEventListenerObject | null) { super.addEventListener(type, listener) }
  override removeEventListener(type: string, listener: EventListenerOrEventListenerObject | null) { super.removeEventListener(type, listener) }
}

// A held switcher wired to the real reducer, as useSpaceSwitcher wires it.
function heldSwitcher(target: EventTarget, order: string[], enabled = true) {
  let state: SwitcherState = idleSwitcher
  const commits: string[] = []
  const apply = (event: SwitcherEvent) => {
    const result = reduceSwitcher(state, event)
    state = result.state
    if (result.commit) commits.push(result.commit)
    return state
  }
  const dispose = bindSpaceSwitcher(target as unknown as Window, {
    cycle: (direction) => enabled && apply({ type: 'cycle', direction, order }).phase === 'holding',
    holding: () => state.phase === 'holding',
    intent: (intent) => { apply(intent === 'forward' || intent === 'backward' ? { type: 'step', direction: intent } : { type: intent }) },
  })
  return { dispose, commits, state: () => state }
}

function keyup(target: EventTarget, init: KeyboardInit) {
  const event = new TestKeyboardEvent('keyup', init)
  target.dispatchEvent(event)
  return event
}

const altTab = { key: 'Tab', code: 'Tab', altKey: true }

test('Option-Tab is claimed so focus does not cycle, and a quick tap flips to the last space on Option release', () => {
  const target = new WindowLikeTarget()
  const switcher = heldSwitcher(target, ['now', 'last', 'older'])
  try {
    assert.equal(dispatch(target, altTab).defaultPrevented, true)
    assert.equal(switcher.state().phase, 'holding')
    keyup(target, { key: 'Alt', code: 'AltLeft' })
    assert.deepEqual(switcher.commits, ['last'])
    assert.equal(switcher.state().phase, 'idle')
  } finally {
    switcher.dispose()
  }
})

test('Tab and Shift-Tab cycle the held list; Option release commits the highlighted space', () => {
  const target = new WindowLikeTarget()
  const switcher = heldSwitcher(target, ['now', 'last', 'older'])
  try {
    dispatch(target, altTab)
    assert.equal(dispatch(target, altTab).defaultPrevented, true)
    assert.equal(dispatch(target, altTab).defaultPrevented, true)
    assert.equal(dispatch(target, { ...altTab, shiftKey: true }).defaultPrevented, true)
    assert.deepEqual(switcher.state(), { phase: 'holding', order: ['now', 'last', 'older'], index: 2 })
    keyup(target, { key: 'Alt', code: 'AltRight' })
    assert.deepEqual(switcher.commits, ['older'])
  } finally {
    switcher.dispose()
  }
})

test('Shift-Option-Tab opens on the oldest space and arrows step while held', () => {
  const target = new WindowLikeTarget()
  const switcher = heldSwitcher(target, ['now', 'last', 'older'])
  try {
    assert.equal(dispatch(target, { ...altTab, shiftKey: true }).defaultPrevented, true)
    assert.equal(switcher.state().phase === 'holding' && switcher.state().index, 2)
    assert.equal(dispatch(target, { key: 'ArrowDown', code: 'ArrowDown', altKey: true }).defaultPrevented, true)
    assert.equal(dispatch(target, { key: 'ArrowDown', code: 'ArrowDown', altKey: true }).defaultPrevented, true)
    keyup(target, { key: 'Alt', code: 'AltLeft' })
    assert.deepEqual(switcher.commits, ['last'])
  } finally {
    switcher.dispose()
  }
})

test('Escape cancels the held switcher and swallows the key; the Option release then changes nothing', () => {
  const target = new WindowLikeTarget()
  const switcher = heldSwitcher(target, ['now', 'last'])
  try {
    dispatch(target, altTab)
    assert.equal(dispatch(target, { key: 'Escape', code: 'Escape', altKey: true }).defaultPrevented, true)
    assert.equal(switcher.state().phase, 'idle')
    keyup(target, { key: 'Alt', code: 'AltLeft' })
    assert.deepEqual(switcher.commits, [])
  } finally {
    switcher.dispose()
  }
})

test('losing window focus while held cancels rather than commits', () => {
  const target = new WindowLikeTarget()
  const switcher = heldSwitcher(target, ['now', 'last'])
  try {
    dispatch(target, altTab)
    target.dispatchEvent(new Event('blur'))
    assert.equal(switcher.state().phase, 'idle')
    keyup(target, { key: 'Alt', code: 'AltLeft' })
    assert.deepEqual(switcher.commits, [])
  } finally {
    switcher.dispose()
  }
})

test('a key without Option while held means the release was missed, so it commits', () => {
  const target = new WindowLikeTarget()
  const switcher = heldSwitcher(target, ['now', 'last'])
  try {
    dispatch(target, altTab)
    dispatch(target, { key: 'a', code: 'KeyA' })
    assert.deepEqual(switcher.commits, ['last'])
    assert.equal(switcher.state().phase, 'idle')
  } finally {
    switcher.dispose()
  }
})

test('while held, every other key is consumed before any shell shortcut: Alt+W, Shift-Option-arrows and Option-arrows change nothing', () => {
  const target = new WindowLikeTarget()
  const calls: string[] = []
  // The switcher binds in the capture phase, so it runs before the bubble
  // phase shell shortcuts; this flat target runs listeners in bind order.
  const switcher = heldSwitcher(target, ['now', 'last', 'older'])
  const unsubscribe = bindShellShortcuts(target as unknown as Window, actions(calls), 'Macintosh')
  try {
    dispatch(target, altTab)
    for (const init of [
      { key: '∑', code: 'KeyW', altKey: true },
      { key: 'ArrowLeft', code: 'ArrowLeft', altKey: true, shiftKey: true },
      { key: 'ArrowRight', code: 'ArrowRight', altKey: true, shiftKey: true },
      { key: 'ArrowLeft', code: 'ArrowLeft', altKey: true },
      { key: 'Dead', code: 'Digit3', altKey: true },
    ]) {
      assert.equal(dispatch(target, init).defaultPrevented, true, init.code)
    }
    assert.deepEqual(calls, [], 'no shell shortcut ran under the held switcher')
    assert.deepEqual(switcher.state(), { phase: 'holding', order: ['now', 'last', 'older'], index: 1 })
    keyup(target, { key: 'Alt', code: 'AltLeft' })
    assert.deepEqual(switcher.commits, ['last'])
    dispatch(target, { key: '∑', code: 'KeyW', altKey: true })
    assert.deepEqual(calls, ['close'], 'released, shortcuts work again')
  } finally {
    unsubscribe()
    switcher.dispose()
  }
})

test('Option-Tab is left to the browser with fewer than two spaces or while spaces are unavailable', () => {
  const single = new WindowLikeTarget()
  const one = heldSwitcher(single, ['only'])
  const disabledTarget = new WindowLikeTarget()
  const disabled = heldSwitcher(disabledTarget, ['now', 'last'], false)
  try {
    assert.equal(dispatch(single, altTab).defaultPrevented, false)
    assert.equal(one.state().phase, 'idle')
    assert.equal(dispatch(disabledTarget, altTab).defaultPrevented, false)
  } finally {
    one.dispose()
    disabled.dispose()
  }
})

test('plain Tab and keys outside a hold stay untouched, and disposal unbinds everything', () => {
  const target = new WindowLikeTarget()
  const switcher = heldSwitcher(target, ['now', 'last'])
  assert.equal(dispatch(target, { key: 'Tab', code: 'Tab' }).defaultPrevented, false)
  assert.equal(dispatch(target, { key: 'Escape', code: 'Escape' }).defaultPrevented, false)
  assert.equal(switcher.state().phase, 'idle')
  switcher.dispose()
  assert.equal(dispatch(target, altTab).defaultPrevented, false)
  assert.equal(switcher.state().phase, 'idle')
})

test('held-key intents: arrows step, Enter commits, Escape cancels, bare modifiers wait', () => {
  assert.equal(switcherKeyIntent({ key: 'Escape', altKey: true }), 'cancel')
  assert.equal(switcherKeyIntent({ key: 'ArrowDown', altKey: true }), 'forward')
  assert.equal(switcherKeyIntent({ key: 'ArrowUp', altKey: true }), 'backward')
  assert.equal(switcherKeyIntent({ key: 'Enter', altKey: true }), 'release')
  assert.equal(switcherKeyIntent({ key: 'Shift', altKey: true }), null)
  assert.equal(switcherKeyIntent({ key: 'Alt', altKey: true }), null)
  assert.equal(switcherKeyIntent({ key: 'x', altKey: true }), null)
  assert.equal(switcherKeyIntent({ key: 'x', altKey: false }), 'release')
})
