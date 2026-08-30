import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import {
  focusComposerWhenReady,
  composerFieldId,
  composerDraftKey,
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
