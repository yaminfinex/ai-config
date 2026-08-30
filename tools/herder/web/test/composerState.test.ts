import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import {
  focusComposerWhenReady,
  composerFieldId,
  composerDraftKey,
  composerShouldRemeasureFromZero,
  blurComposerOnEscape,
  isComposerSendShortcut,
  persistComposerDraft,
  readComposerDraft,
  resolveComposerStorage,
} from '../src/composerState.ts'

function memoryStorage() {
  const values = new Map<string, string>()
  return {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => { values.set(key, value) },
    removeItem: (key: string) => { values.delete(key) },
  }
}

test('draft keys are versioned and isolated per agent', () => {
  const storage = memoryStorage()
  persistComposerDraft('agent/one', 'first draft', storage)
  persistComposerDraft('agent two', 'second draft', storage)
  assert.equal(readComposerDraft('agent/one', storage), 'first draft')
  assert.equal(readComposerDraft('agent two', storage), 'second draft')
  assert.notEqual(composerDraftKey('agent/one'), composerDraftKey('agent two'))
  persistComposerDraft('agent/one', '', storage)
  assert.equal(readComposerDraft('agent/one', storage), '')
  assert.equal(readComposerDraft('agent two', storage), 'second draft')
})

test('blocked browser storage degrades to a working non-persistent composer', () => {
  const blocked = resolveComposerStorage(() => {
    const error = new Error('Access is denied for this document.')
    error.name = 'SecurityError'
    throw error
  })

  assert.equal(blocked, null)
  assert.equal(readComposerDraft('agent/blocked', blocked), '')
  assert.doesNotThrow(() => persistComposerDraft('agent/blocked', 'usable draft', blocked))
  assert.equal(isComposerSendShortcut({ key: 'Enter', ctrlKey: true, metaKey: false }), true)
})

test('only Ctrl+Enter and Cmd+Enter are send shortcuts', () => {
  assert.equal(isComposerSendShortcut({ key: 'Enter', ctrlKey: true, metaKey: false }), true)
  assert.equal(isComposerSendShortcut({ key: 'Enter', ctrlKey: false, metaKey: true }), true)
  assert.equal(isComposerSendShortcut({ key: 'Enter', ctrlKey: true, metaKey: false, shiftKey: true }), false)
  assert.equal(isComposerSendShortcut({ key: 'Enter', ctrlKey: false, metaKey: false }), false)
  assert.equal(isComposerSendShortcut({ key: 'a', ctrlKey: true, metaKey: false }), false)
})

test('each open agent composer gets a unique DOM id', () => {
  assert.equal(composerFieldId('agent one'), 'message-agent%20one')
  assert.notEqual(composerFieldId('agent one'), composerFieldId('agent two'))
})

test('Escape blurs the composer without claiming the event', () => {
  let blurred = false
  assert.equal(blurComposerOnEscape({ key: 'Escape', currentTarget: { blur: () => { blurred = true } } }), true)
  assert.equal(blurred, true)
  blurred = false
  assert.equal(blurComposerOnEscape({ key: 'Enter', currentTarget: { blur: () => { blurred = true } } }), false)
  assert.equal(blurred, false)
})

test('send success refetches the transcript immediately, not just agent status', () => {
  const composer = readFileSync(new URL('../src/features/composer/Composer.tsx', import.meta.url), 'utf8')
  const success = composer.slice(composer.indexOf('setSendNotice'), composer.indexOf('} catch'))
  assert.match(success, /queryKeys\.agent\(name\)/)
  assert.match(success, /queryKeys\.entries\(name\)/)
})

test('focusComposerWhenReady retries until the composer mounts, then stops quietly', () => {
  let focused = 0
  const pending: Array<() => void> = []
  const schedule = (callback: () => void) => { pending.push(callback) }
  let composer: { focus: () => void } | null = null
  focusComposerWhenReady(() => composer, schedule, 5)
  assert.equal(focused, 0)
  assert.equal(pending.length, 1)
  pending.shift()?.()
  composer = { focus: () => { focused += 1 } }
  pending.shift()?.()
  assert.equal(focused, 1)
  assert.equal(pending.length, 0)

  composer = null
  focusComposerWhenReady(() => composer, schedule, 2)
  pending.shift()?.()
  pending.shift()?.()
  assert.equal(pending.length, 0)
  assert.equal(focused, 1)
})

test('focusComposerWhenReady cancels its outstanding frame', () => {
  const callbacks = new Map<number, () => void>()
  const cancelled: number[] = []
  let nextHandle = 1
  const cancel = focusComposerWhenReady(
    () => null,
    (callback) => {
      const handle = nextHandle++
      callbacks.set(handle, callback)
      return handle
    },
    20,
    (handle) => {
      cancelled.push(handle)
      callbacks.delete(handle)
    },
  )
  cancel()
  assert.deepEqual(cancelled, [1])
  assert.equal(callbacks.size, 0)
})

test('selecting an agent focuses its composer only on user-driven opens', () => {
  const app = readFileSync(new URL('../src/App.tsx', import.meta.url), 'utf8')
  const actions = readFileSync(new URL('../src/features/workspace/useWorkspaceActions.ts', import.meta.url), 'utf8')
  const registry = readFileSync(new URL('../src/features/workspace/panelRegistry.tsx', import.meta.url), 'utf8')
  const controller = readFileSync(new URL('../src/features/workspace/useWorkspaceController.ts', import.meta.url), 'utf8')
  assert.match(app, /onPreviewAgent=\{\(name, placement\) => openAgent\(name, true, placement, true\)\}/)
  assert.match(app, /onPinAgent=\{\(name, placement\) => openAgent\(name, false, placement, true\)\}/)
  assert.match(registry, /onOpenAgent=\{\(name, placement\) => workspace\.openAgent\(name, true, placementInGroup\(placement, api\.group\.id\), true\)\}/)
  const applyRoute = controller.slice(controller.indexOf('const applyRoute'), controller.indexOf('const onDockReady'))
  assert.match(applyRoute, /openPanel\(\{ \.\.\.route\.params, preview: true \}, undefined, false\)/)
  assert.doesNotMatch(applyRoute, /openPanel\([^\n]*true\)/)
  assert.match(actions, /focusComposerWhenReady/)
})

test('the auto-resizing field skips the collapse only for an appended draft', () => {
  // Typing or pasting at the end (a pure extension) never collapses: scrollHeight
  // already reflects the taller content, so the transcript is not jittered off
  // the bottom.
  assert.equal(composerShouldRemeasureFromZero({ name: 'kera', value: 'abc' }, { name: 'kera', value: 'abcd' }), false)
  assert.equal(composerShouldRemeasureFromZero({ name: 'kera', value: 'line1' }, { name: 'kera', value: 'line1\nline2' }), false)
  assert.equal(composerShouldRemeasureFromZero({ name: 'kera', value: 'abc' }, { name: 'kera', value: 'abc' }), false)
  // First measurement grows from the empty baseline.
  assert.equal(composerShouldRemeasureFromZero({ name: 'kera', value: '' }, { name: 'kera', value: 'hello' }), false)
  // Deleting or editing in the middle can shorten the field, so collapse.
  assert.equal(composerShouldRemeasureFromZero({ name: 'kera', value: 'abcd' }, { name: 'kera', value: 'abc' }), true)
  assert.equal(composerShouldRemeasureFromZero({ name: 'kera', value: 'aXbc' }, { name: 'kera', value: 'abc' }), true)
  // Switching agents swaps the whole draft: always remeasure from zero.
  assert.equal(composerShouldRemeasureFromZero({ name: 'kera', value: 'shared' }, { name: 'ziru', value: 'shared' }), true)
})

test('a selection-replacing paste that adds characters but removes lines still collapses', () => {
  // Edge (a): a length-based check would see more characters and skip the
  // collapse, leaving the field stuck too tall. The paste is not an append of
  // the old draft, so the prefix check collapses and measures the shorter,
  // single-line content honestly.
  const previous = { name: 'kera', value: 'a\nb\nc' } // three rendered lines
  const next = { name: 'kera', value: 'aaaaaaaaaaaaaaaaaaaa' } // more characters, one line
  assert.ok(next.value.length > previous.value.length)
  assert.equal(composerShouldRemeasureFromZero(previous, next), true)
})

test('a wrap-width change never jitters the transcript and self-heals on the next shrink', () => {
  // Edge (b): a panel resize rewraps the same text to fewer lines. The auto-size
  // effect keys on the draft, so identical text never collapses — there is no
  // per-keystroke reflow, the transcript stays pinned, and the jump button never
  // appears. At worst the field is left cosmetically too tall; the next deletion
  // collapses and re-measures, restoring the correct height.
  const draft = { name: 'kera', value: 'a wide line that rewraps when the panel narrows' }
  assert.equal(composerShouldRemeasureFromZero(draft, draft), false) // width change alone: no jitter
  const afterDeletion = { name: 'kera', value: 'a wide line that rewraps when the panel narrow' }
  assert.equal(composerShouldRemeasureFromZero(draft, afterDeletion), true) // self-heals on next shrink
})

test('the composer uses the shared remeasure predicate rather than an inline height reset', () => {
  const composer = readFileSync(new URL('../src/features/composer/Composer.tsx', import.meta.url), 'utf8')
  assert.match(composer, /composerShouldRemeasureFromZero\(measuredRef\.current, next\)/)
  // The collapse is guarded, never unconditional on every keystroke.
  assert.doesNotMatch(composer, /^\s*composer\.style\.height = '0px'\s*$/m)
})
