import assert from 'node:assert/strict'
import test from 'node:test'

import {
  aggregateActivityPills,
  cleanViewDisposition,
  cleanViewPreferenceKey,
  isCleanConversationDelivery,
  persistCleanView,
  persistShowSystem,
  readCleanView,
  readShowSystem,
  showSystemPreferenceKey,
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
    task_notification: 'activity',
    injected_system: 'system',
    command_stdout: 'activity',
    compact_divider: 'show',
    assistant_text: 'show',
    thinking: 'activity',
    tool_use: 'activity',
    tool_result: 'activity',
    turn_duration: 'hide',
    system_chip: 'system',
    unknown: 'activity',
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

test('system entry preference uses the same per-agent v1 persistence pattern', () => {
  const storage = memoryStorage()
  assert.equal(readShowSystem('agent one', storage), false)
  persistShowSystem('agent one', true, storage)
  assert.equal(readShowSystem('agent one', storage), true)
  assert.equal(readShowSystem('agent two', storage), false)
  assert.notEqual(showSystemPreferenceKey('agent one'), cleanViewPreferenceKey('agent one'))
  persistShowSystem('agent one', false, storage)
  assert.equal(readShowSystem('agent one', storage), false)
})

test('pill aggregation combines only consecutive repeatable activities', () => {
  assert.deepEqual(aggregateActivityPills([
    { key: 'read-1', label: 'Read', kind: 'tool_use' },
    { key: 'read-2', label: 'Read', kind: 'tool_use' },
    { key: 'message-1', label: '✉ nero', kind: 'message' },
    { key: 'message-2', label: '✉ nero', kind: 'message' },
    { key: 'read-3', label: 'Read', kind: 'tool_use' },
  ], (activity) => activity.kind === 'tool_use'), [
    { key: 'read-1', label: 'Read', count: 2 },
    { key: 'message-1', label: '✉ nero', count: 1 },
    { key: 'message-2', label: '✉ nero', count: 1 },
    { key: 'read-3', label: 'Read', count: 1 },
  ])
})
