import assert from 'node:assert/strict'
import test from 'node:test'

import { appendComposerDraft } from '../src/composerState.ts'
import { createNoteHandOffGuard, handOffSelectedNotes } from '../src/features/notes/noteHandOff.ts'
import { selectionAfterClick, selectionAfterRemoval, selectionAll, type NoteSelection } from '../src/features/notes/notesListModel.ts'
import { noteTransferText } from '../src/features/notes/notesPresentation.ts'
import { createNotesStore, type Note, type NotesStatus } from '../src/features/notes/notesStore.ts'

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

test('⌘↵ hands notes to the draft top to bottom, oldest first, as the list shows them (reddens: newest first)', () => {
  let now = 1_000
  let sequence = 0
  const store = createNotesStore({ storage: null, events: null, now: () => now, randomID: () => `id-${++sequence}`, schedule: () => 0, cancel: () => {} })
  const first = store.add({ group: 'kilo', text: 'first' })
  now = 2_000
  store.add({ group: 'kilo', text: 'second' })
  now = 3_000
  store.add({ group: 'kilo', text: 'third' })
  now = 4_000
  assert.ok(first.ok)
  if (!first.ok) return
  store.edit(first.value.id, { text: 'first, edited' })

  // As NotesList does: select every note in list order, hand off the selected ones in list order.
  const listed = store.listGroup('kilo')
  const selection = selectionAll({ selected: new Set() }, listed.map((entry) => entry.id))
  const drafts = new Map<string, string>()
  const draftStorage = { getItem: (key: string) => drafts.get(key) ?? null, setItem: (key: string, value: string) => { drafts.set(key, value) } }
  const result = handOffSelectedNotes({
    target: 'kilo', notes: listed.filter((entry) => selection.selected.has(entry.id)), guard: createNoteHandOffGuard(),
    append: (target, handed) => appendComposerDraft(target, handed.map(noteTransferText), draftStorage),
    remove: store.delete, flush: store.flush, status: () => ({ persistent: true, recovered: false, problem: '' }),
  })
  assert.equal(result.ok, true)
  assert.deepEqual([...drafts.values()], ['first, edited\n\nsecond\n\nthird'])
})
