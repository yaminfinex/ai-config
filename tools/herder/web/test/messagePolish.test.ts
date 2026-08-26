import assert from 'node:assert/strict'
import test from 'node:test'

import {
  duplicateHcomDeliveryIndices,
  stripWebOperatorNote,
  webOperatorNoteEnd,
  webOperatorNoteStart,
} from '../src/messagePolish.ts'

const legacyNote = '[This message came from a web operator named web-invented-viewer via the fleet web view. They cannot receive hcom messages; do not reply with `hcom send`. Answer in your normal chat turn; they are watching the session transcript live.]'

test('strips fenced and exact legacy web-operator notes only at the prefix', () => {
  const fenced = `${webOperatorNoteStart}\nThis message came from an invented web operator.\n${webOperatorNoteEnd}\n\nactual fenced body`
  assert.equal(stripWebOperatorNote(fenced), 'actual fenced body')
  assert.equal(stripWebOperatorNote('<<<HERDER_WEB_OPERATOR_NOTE>>>\npre-release instruction\n<<<END_HERDER_WEB_OPERATOR_NOTE>>>\n\npre-release body'), 'pre-release body')
  assert.equal(stripWebOperatorNote(`${legacyNote}\n\nactual legacy body`), 'actual legacy body')
  assert.equal(stripWebOperatorNote(`prefix ${legacyNote}\n\nkeep me`), `prefix ${legacyNote}\n\nkeep me`)
  assert.equal(stripWebOperatorNote(`${legacyNote.slice(0, -2)} changed]\n\nkeep me`), `${legacyNote.slice(0, -2)} changed]\n\nkeep me`)
  assert.equal(stripWebOperatorNote(`${webOperatorNoteStart}\nunterminated`), `${webOperatorNoteStart}\nunterminated`)
})

test('marks only repeated bus messages in adjacent delivery entries', () => {
  const entries = [
    { kind: 'hcom_delivery', payload: { deliveries: [{ message_id: '731', text: 'first rendering' }, { text: 'fallback body' }] } },
    { kind: 'hcom_delivery', payload: { deliveries: [{ message_id: '731', text: 'wrapper differs' }, { text: 'fallback body' }, { message_id: '732', text: 'new body' }] } },
    { kind: 'assistant_text', payload: {} },
    { kind: 'hcom_delivery', payload: { deliveries: [{ message_id: '731', text: 'not adjacent' }] } },
  ]
  const duplicates = duplicateHcomDeliveryIndices(entries)
  assert.deepEqual([...duplicates.entries()].map(([entry, indices]) => [entry, [...indices]]), [[1, [0, 1]]])
})
