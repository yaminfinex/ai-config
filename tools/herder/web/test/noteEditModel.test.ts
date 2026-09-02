import assert from 'node:assert/strict'
import test from 'node:test'

import { beginNoteEdit, noteEditDisplay, updateNoteEdit } from '../src/features/notes/noteEditModel.ts'
import { createNotesStore, type StoredNoteRecord } from '../src/features/notes/notesStore.ts'

const original = { id: 'note-1', group: 'general', text: 'original', created: 1, updated: 1 }

function harness() {
  let now = 10
  let id = 0
  const store = createNotesStore({
    storage: null,
    events: null,
    now: () => now,
    randomID: () => `write-${++id}`,
    schedule: () => undefined,
    cancel: () => undefined,
  })
  const seed: StoredNoteRecord = { version: 1, writeID: 'seed', record: original }
  store.merge([seed])
  return { store, setNow: (value: number) => { now = value } }
}

test('a pulled edit updates the record without overwriting the local draft; cancel reveals it', () => {
  const subject = harness()
  const draft = updateNoteEdit(beginNoteEdit(original), 'my local words')
  subject.store.merge([{ version: 1, writeID: 'remote', record: { ...original, text: 'remote words', updated: 20 } }])
  assert.equal(draft.text, 'my local words')
  assert.equal(noteEditDisplay(draft, subject.store.list().find(({ id }) => id === original.id)).text, 'remote words')
  assert.equal(subject.store.list()[0]?.text, 'remote words')
})

test('committing a draft after a pulled edit stamps newer and wins', () => {
  const subject = harness()
  const draft = updateNoteEdit(beginNoteEdit(original), 'my local words')
  subject.store.merge([{ version: 1, writeID: 'remote', record: { ...original, text: 'remote words', updated: 20 } }])
  subject.setNow(15)
  const committed = subject.store.edit(original.id, { text: draft.text }, draft.original)
  assert.equal(committed.ok, true)
  assert.equal(subject.store.list()[0]?.text, 'my local words')
  assert.equal(subject.store.list()[0]?.updated, 21)
})

test('a pulled tombstone keeps the editor display; commit resurrects and cancel yields', () => {
  const subject = harness()
  const draft = updateNoteEdit(beginNoteEdit(original), 'resurrected words')
  subject.store.merge([{ version: 1, writeID: 'remote-delete', record: { id: original.id, deleted: true, updated: 30 } }])
  assert.deepEqual(subject.store.list(), [])
  assert.equal(noteEditDisplay(draft, undefined).text, 'original')

  subject.setNow(25)
  const committed = subject.store.edit(original.id, { text: draft.text }, draft.original)
  assert.equal(committed.ok, true)
  assert.equal(subject.store.list()[0]?.text, 'resurrected words')
  assert.equal(subject.store.list()[0]?.updated, 31)

  subject.store.merge([{ version: 1, writeID: 'remote-delete-2', record: { id: original.id, deleted: true, updated: 40 } }])
  assert.deepEqual(subject.store.list(), [], 'cancel removes the editor-only fallback and lets the tombstone stand')
})
