import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

import type { Note } from '../src/features/notes/notesStore.ts'
import {
  dragNoteIDs,
  handOffRoute,
  noteListAction,
  selectionAfterArrow,
  selectionAfterClick,
  type NoteSelection,
} from '../src/features/notes/notesListModel.ts'

const empty = (): NoteSelection => ({ selected: new Set(), anchor: undefined, cursor: undefined })
const note = (id: string, group: string, updated = 1): Note => ({ id, group, text: id, created: 1, updated })

test('click, command-click, and shift-click follow Finder selection semantics', () => {
  const ids = ['a', 'b', 'c', 'd']
  const plain = selectionAfterClick(empty(), ids, 'b', {})
  assert.deepEqual([...plain.selected], ['b'])
  const toggled = selectionAfterClick(plain, ids, 'd', { command: true })
  assert.deepEqual([...toggled.selected], ['b', 'd'])
  const ranged = selectionAfterClick(toggled, ids, 'c', { shift: true })
  assert.deepEqual([...ranged.selected], ['c', 'd'])
  assert.equal(ranged.anchor, 'd')
})

test('arrows move selection and shift-arrows extend it from the anchor', () => {
  const ids = ['a', 'b', 'c', 'd']
  const first = selectionAfterClick(empty(), ids, 'b', {})
  const moved = selectionAfterArrow(first, ids, 1, false)
  assert.deepEqual([...moved.selected], ['c'])
  const extended = selectionAfterArrow(moved, ids, 1, true)
  assert.deepEqual([...extended.selected], ['c', 'd'])
  const shrunk = selectionAfterArrow(extended, ids, -1, true)
  assert.deepEqual([...shrunk.selected], ['c'])
})

test('card shortcuts never claim native keys from an editor, chip comment, or selector control', () => {
  assert.equal(noteListAction({ key: 'c', metaKey: true }, false), 'copy')
  assert.equal(noteListAction({ key: 'Backspace' }, false), 'delete')
  assert.equal(noteListAction({ key: 'Enter' }, false), 'hand-off')
  assert.equal(noteListAction({ key: 'a' }, false), 'assign')
  for (const event of [{ key: 'c', metaKey: true }, { key: 'Backspace' }, { key: 'Enter' }, { key: 'a' }]) {
    assert.equal(noteListAction(event, true), null)
  }
})

test('hand-off goes direct only when every note shares one live target', () => {
  const order = ['kilo', 'muro', 'podi']
  assert.deepEqual(handOffRoute([note('1', 'kilo'), note('2', 'kilo')], order), { kind: 'direct', target: 'kilo' })
  assert.deepEqual(handOffRoute([note('1', 'kilo'), note('2', 'general')], order), { kind: 'selector', initial: 'kilo' })
  assert.deepEqual(handOffRoute([note('1', 'retired'), note('2', 'general')], order), { kind: 'selector', initial: 'kilo' })
  assert.deepEqual(handOffRoute([note('1', 'muro'), note('2', 'kilo')], order), { kind: 'selector', initial: 'kilo' })
  assert.deepEqual(handOffRoute([note('1', 'muro'), note('2', 'muro'), note('3', 'kilo')], order), { kind: 'selector', initial: 'muro' })
})

test('hand-off destination defaults defer to the shared selector computation', () => {
  const source = readFileSync(new URL('../src/features/notes/notesListModel.ts', import.meta.url), 'utf8')
  assert.match(source, /selectorInitialValue/)
  assert.doesNotMatch(source, /const counts = new Map/)
})

test('dragging an unselected card moves only it; dragging a selected card moves the selection', () => {
  const selected = new Set(['a', 'b'])
  assert.deepEqual(dragNoteIDs('c', selected), ['c'])
  assert.deepEqual(dragNoteIDs('b', selected), ['a', 'b'])
  assert.deepEqual([...selected], ['a', 'b'], 'dragging must not disturb selection')
})
