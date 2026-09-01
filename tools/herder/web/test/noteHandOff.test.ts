import assert from 'node:assert/strict'
import test from 'node:test'

import { createNoteHandOffGuard, handOffSelectedNotes } from '../src/features/notes/noteHandOff.ts'
import { selectionAfterClick, selectionAfterRemoval, type NoteSelection } from '../src/features/notes/notesListModel.ts'
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

test('successful removal moves selection after append even when durability later fails', () => {
  const order: string[] = []
  const ids = ['before', note.id, 'after']
  let selection: NoteSelection = selectionAfterClick({ selected: new Set() }, ids, note.id, {})
  const result = handOffSelectedNotes({
    target: 'kilo', notes: [note], guard: createNoteHandOffGuard(),
    append: () => { order.push('append'); return { ok: true } },
    remove: (removedIDs) => {
      order.push('remove')
      selection = selectionAfterRemoval(selection, ids, removedIDs)
      return { ok: true, value: removedIDs.length }
    },
    flush: () => { order.push('flush'); return false },
    status: () => { order.push('status'); return unavailable },
  })
  assert.equal(result.ok, false)
  assert.deepEqual(order, ['append', 'remove', 'flush', 'status'])
  assert.deepEqual([...selection.selected], ['after'])
  assert.equal(selection.anchor, 'after')
  assert.equal(selection.cursor, 'after')
})

test('append or removal refusal leaves selection in place', () => {
  const ids = ['before', note.id, 'after']
  const initial = selectionAfterClick({ selected: new Set() }, ids, note.id, {})
  for (const refusal of ['append', 'remove'] as const) {
    let selection = initial
    handOffSelectedNotes({
      target: 'kilo', notes: [note], guard: createNoteHandOffGuard(),
      append: () => refusal === 'append' ? { ok: false, reason: 'no append' } : { ok: true },
      remove: (removedIDs) => {
        if (refusal === 'remove') return { ok: false, reason: 'no removal' }
        selection = selectionAfterRemoval(selection, ids, removedIDs)
        return { ok: true, value: removedIDs.length }
      },
      flush: () => true,
      status: () => ({ ...unavailable, persistent: true }),
    })
    assert.deepEqual([...selection.selected], [note.id])
    assert.equal(selection.anchor, note.id)
    assert.equal(selection.cursor, note.id)
  }
})
