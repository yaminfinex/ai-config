import assert from 'node:assert/strict'
import test from 'node:test'
import { visibleQueuedMessages } from '../src/features/transcript/queuedMessages.ts'
import type { QueuedMessage, TranscriptEntry } from '../src/types.ts'

const queued: QueuedMessage[] = [
  { id: 731, sender: 'web-owner', intent: 'request', preview: 'operator question', sent_at: '2026-08-27T04:00:00Z', operator: true },
  { id: 732, sender: 'vile', intent: 'inform', preview: 'agent note', sent_at: '2026-08-27T04:00:01Z' },
]

test('queued messages disappear as soon as an entry carries the same delivery id', () => {
  const entries: TranscriptEntry[] = [{
    kind: 'hcom_delivery', line: 1, byteOffset: 0,
    payload: { subtype: 'developer_message', deliveries: [{ message_id: '731', sender: 'web-owner', recipient: 'dore', text: 'operator question' }] },
  }]
  assert.deepEqual(visibleQueuedMessages(queued, entries).map((message) => message.id), [732])
})

test('operator emphasis is based on server-proven envelope attribution', () => {
  assert.equal(queued[0].operator, true)
  assert.equal(queued[1].operator, undefined)
})
