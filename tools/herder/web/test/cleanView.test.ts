import assert from 'node:assert/strict'
import test from 'node:test'

import {
  cleanViewDisposition,
  cleanViewPreferenceKey,
  isCleanConversationDelivery,
  persistCleanView,
  readCleanView,
} from '../src/features/transcript/cleanView.ts'

function memoryStorage() {
  const values = new Map<string, string>()
  return {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => { values.set(key, value) },
    removeItem: (key: string) => { values.delete(key) },
  }
}

test('fixture law classifies every transcript kind explicitly', () => {
  assert.deepEqual(cleanViewDisposition, {
    human_prompt: 'show',
    hcom_delivery_stub: 'delivery',
    hcom_delivery: 'delivery',
    task_notification: 'hide',
    injected_system: 'hide',
    command_stdout: 'hide',
    compact_divider: 'show',
    assistant_text: 'show',
    thinking: 'hide',
    tool_use: 'hide',
    tool_result: 'hide',
    turn_duration: 'hide',
    system_chip: 'hide',
    unknown: 'hide',
  })
})

test('clean conversation delivery policy hides lifecycle traffic without guessing from text', () => {
  assert.equal(isCleanConversationDelivery({ sender: '[hcom-launcher]', intent: 'new message', text: 'agent ready' }), false)
  assert.equal(isCleanConversationDelivery({ sender: 'impl-vile', intent: 'ack', text: 'acknowledged' }), false)
  assert.equal(isCleanConversationDelivery({ sender: 'impl-vile', intent: 'request', text: 'please inspect this' }), true)
  assert.equal(isCleanConversationDelivery({ sender: 'impl-vile', intent: 'inform', text: 'work is done' }), true)
  assert.equal(isCleanConversationDelivery({ sender: 'web-owner', intent: 'new message', text: 'operator question' }), true)
  assert.equal(isCleanConversationDelivery({ sender: 'impl-vile', intent: 'new message', text: 'ordinary delivery' }), true)
})

test('clean view preference is persisted per agent tab and defaults off', () => {
  const storage = memoryStorage()
  assert.equal(readCleanView('agent one', storage), false)
  persistCleanView('agent one', true, storage)
  persistCleanView('agent two', true, storage)
  assert.equal(readCleanView('agent one', storage), true)
  assert.equal(readCleanView('agent two', storage), true)
  assert.notEqual(cleanViewPreferenceKey('agent one'), cleanViewPreferenceKey('agent two'))
  persistCleanView('agent one', false, storage)
  assert.equal(readCleanView('agent one', storage), false)
  assert.equal(readCleanView('agent two', storage), true)
})

test('blocked browser storage degrades to clean view off', () => {
  const blocked = {
    getItem: () => { throw new Error('blocked') },
    setItem: () => { throw new Error('blocked') },
    removeItem: () => { throw new Error('blocked') },
  }
  assert.equal(readCleanView('agent', blocked), false)
  assert.doesNotThrow(() => persistCleanView('agent', true, blocked))
})
