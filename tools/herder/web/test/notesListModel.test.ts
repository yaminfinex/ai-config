import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

import type { Note } from '../src/features/notes/notesStore.ts'
import {
  dragNoteIDs,
  handOffRoute,
  noteListAction,
  selectionAfterRemoval,
  selectionAfterArrow,
  selectionAfterClick,
  shouldPreventNoteCardMouseDown,
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

test('only shift-modified note-card mousedown suppresses the browser default', () => {
  assert.equal(shouldPreventNoteCardMouseDown({ shiftKey: true }), true)
  assert.equal(shouldPreventNoteCardMouseDown({ shiftKey: false }), false)
})

test('removing a selected note moves selection, anchor, and cursor to its successor', () => {
  const state = selectionAfterClick(empty(), ['a', 'b', 'c'], 'b', {})
  const next = selectionAfterRemoval(state, ['a', 'b', 'c'], ['b'])
  assert.deepEqual([...next.selected], ['c'])
  assert.equal(next.anchor, 'c')
  assert.equal(next.cursor, 'c')
})

test('removing the final selected note falls back to the previous note', () => {
  const state = selectionAfterClick(empty(), ['a', 'b', 'c'], 'c', {})
  const next = selectionAfterRemoval(state, ['a', 'b', 'c'], ['c'])
  assert.deepEqual([...next.selected], ['b'])
  assert.equal(next.anchor, 'b')
  assert.equal(next.cursor, 'b')
})

test('batch removal chooses the survivor after the last removed position', () => {
  const state = { selected: new Set(['b', 'd']), anchor: 'b', cursor: 'd' }
  const middle = selectionAfterRemoval(state, ['a', 'b', 'c', 'd', 'e'], ['b', 'd'])
  assert.deepEqual([...middle.selected], ['e'])

  const tail = selectionAfterRemoval({ ...state, selected: new Set(['b', 'd', 'e']) }, ['a', 'b', 'c', 'd', 'e'], ['b', 'd', 'e'])
  assert.deepEqual([...tail.selected], ['c'])
  assert.equal(tail.anchor, 'c')
  assert.equal(tail.cursor, 'c')
})

test('removal crosses rendered group boundaries and empties only with the list', () => {
  const ids = ['group-one-a', 'group-one-b', 'group-two-a']
  const boundary = selectionAfterRemoval(selectionAfterClick(empty(), ids, 'group-one-b', {}), ids, ['group-one-b'])
  assert.deepEqual([...boundary.selected], ['group-two-a'])

  const emptied = selectionAfterRemoval(selectionAfterClick(empty(), ['only'], 'only', {}), ['only'], ['only'])
  assert.deepEqual([...emptied.selected], [])
  assert.equal(emptied.anchor, undefined)
  assert.equal(emptied.cursor, undefined)
})

test('shift-arrow extends from the successor after removal', () => {
  const ids = ['a', 'b', 'c', 'd']
  const removed = selectionAfterRemoval(selectionAfterClick(empty(), ids, 'b', {}), ids, ['b'])
  const remaining = ids.filter((id) => id !== 'b')
  const extended = selectionAfterArrow(removed, remaining, 1, true)
  assert.deepEqual([...extended.selected], ['c', 'd'])
  assert.equal(extended.anchor, 'c')
  assert.equal(extended.cursor, 'd')
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
