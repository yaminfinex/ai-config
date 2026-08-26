import assert from 'node:assert/strict'
import test from 'node:test'
import {
  composerFieldId,
  composerDraftKey,
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
  assert.equal(isComposerSendShortcut({ key: 'Enter', ctrlKey: false, metaKey: false }), false)
  assert.equal(isComposerSendShortcut({ key: 'a', ctrlKey: true, metaKey: false }), false)
})

test('each open agent composer gets a unique DOM id', () => {
  assert.equal(composerFieldId('agent one'), 'message-agent%20one')
  assert.notEqual(composerFieldId('agent one'), composerFieldId('agent two'))
})
