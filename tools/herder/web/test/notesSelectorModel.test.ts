import assert from 'node:assert/strict'
import test from 'node:test'

import type { Note } from '../src/features/notes/notesStore.ts'
import {
  filterSelectorRows,
  selectorBackspace,
  selectorInitialValue,
  selectorMove,
  selectorRows,
} from '../src/features/notes/notesSelectorModel.ts'

const note = (id: string, group: string, updated: number): Note => ({ id, group, text: id, created: 1, updated })

test('selector pins unassigned, then noted live agents by recency, then remaining live agents A-Z', () => {
  const notes = [note('1', 'muro', 10), note('2', 'kilo', 40), note('3', 'retired', 90)]
  assert.deepEqual(selectorRows(notes, ['podi', 'muro', 'kilo', 'anta'], 'assign').map((row) => row.value), ['general', 'kilo', 'muro', 'anta', 'podi'])
  assert.deepEqual(selectorRows(notes, ['podi', 'muro', 'kilo', 'anta'], 'destination').map((row) => row.value), ['kilo', 'muro', 'anta', 'podi'])
})

test('assignment defaults to the current group only when selection agrees', () => {
  const rows = selectorRows([], ['kilo', 'muro'], 'assign')
  assert.equal(selectorInitialValue([note('1', 'kilo', 1)], rows, 'assign'), 'kilo')
  assert.equal(selectorInitialValue([note('1', 'kilo', 1), note('2', 'muro', 1)], rows, 'assign'), 'general')
  assert.equal(selectorInitialValue([note('1', 'retired', 1)], rows, 'assign'), 'general')
})

test('destination defaults to the most represented live target and normal sort breaks ties', () => {
  const rows = selectorRows([note('activity', 'kilo', 10), note('newer', 'muro', 20)], ['kilo', 'muro'], 'destination')
  assert.equal(selectorInitialValue([note('1', 'kilo', 1), note('2', 'muro', 1)], rows, 'destination'), 'muro')
  assert.equal(selectorInitialValue([note('1', 'kilo', 1), note('2', 'kilo', 1), note('3', 'muro', 1)], rows, 'destination'), 'kilo')
  assert.equal(selectorInitialValue([note('1', 'general', 1)], rows, 'destination'), 'muro')
})

test('typing filters names and empty-query Backspace highlights unassigned without closing', () => {
  const rows = selectorRows([], ['kilo', 'muro'], 'assign')
  assert.deepEqual(filterSelectorRows(rows, 'mu').map((row) => row.value), ['muro'])
  assert.deepEqual(selectorBackspace('', 'kilo', 'assign'), { handled: true, highlighted: 'general' })
  assert.deepEqual(selectorBackspace('k', 'kilo', 'assign'), { handled: false, highlighted: 'kilo' })
  assert.deepEqual(selectorBackspace('', 'kilo', 'destination'), { handled: false, highlighted: 'kilo' })
})

test('selector arrows wrap at both ends like quick-open', () => {
  const rows = selectorRows([], ['anta', 'kilo', 'muro'], 'destination')
  assert.equal(selectorMove(rows, 'muro', 1), 'anta')
  assert.equal(selectorMove(rows, 'anta', -1), 'muro')
  assert.equal(selectorMove(rows, 'kilo', 1), 'muro')
})
