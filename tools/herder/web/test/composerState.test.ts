import assert from 'node:assert/strict'
import test from 'node:test'
import {
  composerDraftKey,
  isComposerSendShortcut,
  persistComposerDraft,
  readComposerDraft,
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
  persistComposerDraft(storage, 'agent/one', 'first draft')
  persistComposerDraft(storage, 'agent two', 'second draft')
  assert.equal(readComposerDraft(storage, 'agent/one'), 'first draft')
  assert.equal(readComposerDraft(storage, 'agent two'), 'second draft')
  assert.notEqual(composerDraftKey('agent/one'), composerDraftKey('agent two'))
  persistComposerDraft(storage, 'agent/one', '')
  assert.equal(readComposerDraft(storage, 'agent/one'), '')
  assert.equal(readComposerDraft(storage, 'agent two'), 'second draft')
})

test('only Ctrl+Enter and Cmd+Enter are send shortcuts', () => {
  assert.equal(isComposerSendShortcut({ key: 'Enter', ctrlKey: true, metaKey: false }), true)
  assert.equal(isComposerSendShortcut({ key: 'Enter', ctrlKey: false, metaKey: true }), true)
  assert.equal(isComposerSendShortcut({ key: 'Enter', ctrlKey: false, metaKey: false }), false)
  assert.equal(isComposerSendShortcut({ key: 'a', ctrlKey: true, metaKey: false }), false)
})
