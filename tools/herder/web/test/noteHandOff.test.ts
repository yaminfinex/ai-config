import assert from 'node:assert/strict'
import test from 'node:test'

import { createNoteHandOffGuard, handOffSelectedNotes } from '../src/features/notes/noteHandOff.ts'
import type { Note, NotesStatus } from '../src/features/notes/notesStore.ts'

const note: Note = { id: 'note-1', group: 'general', text: 'owner text', created: 1, updated: 1 }
const unavailable: NotesStatus = { persistent: false, recovered: false, problem: 'Notes are not saved between browser sessions.' }

test('a retry after proven append never appends the same note twice', () => {
  const guard = createNoteHandOffGuard()
  let appends = 0
  const first = handOffSelectedNotes({
    target: 'kilo', notes: [note], guard,
    append: () => { appends += 1; return { ok: true } },
    remove: () => ({ ok: true, value: 1 }),
    flush: () => false,
    status: () => unavailable,
  })
  assert.equal(first.ok, false)
  assert.equal(appends, 1)

  const retry = handOffSelectedNotes({
    target: 'kilo', notes: [note], guard,
    append: () => { appends += 1; return { ok: true } },
    remove: () => ({ ok: false, reason: 'This note could not be removed.' }),
    flush: () => false,
    status: () => unavailable,
  })
  assert.equal(retry.ok, false)
  assert.match(retry.reason, /already contains/i)
  assert.equal(appends, 1)
})

test('an append refusal does not arm the retry guard', () => {
  const guard = createNoteHandOffGuard()
  let appends = 0
  const attempt = () => handOffSelectedNotes({
    target: 'kilo', notes: [note], guard,
    append: () => { appends += 1; return { ok: false as const, reason: 'Composer unavailable.' } },
    remove: () => ({ ok: true as const, value: 1 }),
    flush: () => true,
    status: () => ({ ...unavailable, persistent: true }),
  })
  assert.equal(attempt().ok, false)
  assert.equal(attempt().ok, false)
  assert.equal(appends, 2)
})
