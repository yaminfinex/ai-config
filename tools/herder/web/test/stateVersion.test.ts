import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

import { compareStateVersions } from '../src/shared/stateVersion.ts'

test('one neutral comparator matches the shared client/server corpus', () => {
  const corpus = JSON.parse(readFileSync(new URL('../../testdata/state-comparator.json', import.meta.url), 'utf8')) as Array<{
    left: { updated: number, writeID: string }
    right: { updated: number, writeID: string }
    winner: 'left' | 'right' | 'equal'
  }>
  for (const item of corpus) {
    assert.equal(
      Math.sign(compareStateVersions(item.left.updated, item.left.writeID, item.right.updated, item.right.writeID)),
      { left: 1, equal: 0, right: -1 }[item.winner],
    )
  }
})

test('spaces and notes delegate to the single shared comparator definition', () => {
  const shared = readFileSync(new URL('../src/shared/stateVersion.ts', import.meta.url), 'utf8')
  const spaces = readFileSync(new URL('../src/features/spaces/spacesStore.ts', import.meta.url), 'utf8')
  const notes = readFileSync(new URL('../src/features/notes/notesStore.ts', import.meta.url), 'utf8')
  assert.equal((`${shared}\n${spaces}\n${notes}`.match(/function compareStateVersions\s*\(/g) ?? []).length, 1)
  assert.match(spaces, /compareStateVersions/)
  assert.match(notes, /compareStateVersions/)
  assert.doesNotMatch(spaces, /leftUpdated !== rightUpdated/)
  assert.doesNotMatch(notes, /left\.record\.updated !== right\.record\.updated/)
})
