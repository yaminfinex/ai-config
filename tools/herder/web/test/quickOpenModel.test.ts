import assert from 'node:assert/strict'
import test from 'node:test'

import { quickOpenActionRows, quickOpenEnterTarget, quickOpenInitialIndex, quickOpenInitialSelection, quickOpenKeyboardRows, quickOpenMoveSelection, quickOpenSelectedIndex, quickOpenSelection, quickOpenSelectionKeys, quickOpenTopMatch } from '../src/features/files/quickOpenModel.ts'

const spaces = [
  { id: 'main', name: 'main', order: 0, created: 0, updated: 0 },
  { id: 'review', name: 'review queue', order: 1, created: 0, updated: 0 },
  { id: 'notes', name: 'weekly review', order: 2, created: 0, updated: 0 },
]

test('quick open ranks exact, prefix, then substring within spaces before agents', () => {
  assert.deepEqual(quickOpenActionRows('review', spaces, ['reviewer', 'my-review-agent', 'review']), [
    { kind: 'space', id: 'review', label: 'review queue' },
    { kind: 'space', id: 'notes', label: 'weekly review' },
    { kind: 'create', name: 'review', label: 'Create space “review”' },
    { kind: 'agent', name: 'review', label: 'review' },
    { kind: 'agent', name: 'reviewer', label: 'reviewer' },
    { kind: 'agent', name: 'my-review-agent', label: 'my-review-agent' },
    { kind: 'note', text: 'review', label: 'New note: review' },
  ])
})

test('the note row is always last, carries the trimmed text, and is absent for an empty query', () => {
  const rows = quickOpenActionRows('  todo: call back  ', spaces, ['podi'], true, 'main')
  assert.deepEqual(rows.at(-1), { kind: 'note', text: 'todo: call back', label: 'New note: todo: call back' })
  assert.equal(rows.filter((row) => row.kind === 'note').length, 1)
  assert.equal(quickOpenActionRows('', spaces, ['podi']).some((row) => row.kind === 'note'), false)
  assert.equal(quickOpenActionRows('   ', spaces, ['podi']).some((row) => row.kind === 'note'), false)
})

test('keyboard order is actions, then files, then the note row', () => {
  const rows = quickOpenActionRows('review', spaces, ['reviewer'])
  const noteIndex = rows.findIndex((row) => row.kind === 'note')
  assert.deepEqual(quickOpenKeyboardRows(rows, 2), [
    { kind: 'action', index: 0 }, { kind: 'action', index: 1 }, { kind: 'action', index: 2 }, { kind: 'action', index: 3 },
    { kind: 'file', index: 0 }, { kind: 'file', index: 1 },
    { kind: 'action', index: noteIndex },
  ])
  assert.deepEqual(quickOpenEnterTarget(rows, 2, 4), { kind: 'file', index: 0 })
  assert.deepEqual(quickOpenEnterTarget(rows, 2, 6), { kind: 'action', index: noteIndex })
  assert.equal(quickOpenEnterTarget(rows, 2, 7), null)
  assert.equal(quickOpenEnterTarget(rows, 2, -1), null)
})

test('quickOpenInitialIndex: a fresh palette starts on the first openable row; a typed query leaves it to the top match', () => {
  assert.equal(quickOpenInitialIndex(quickOpenActionRows('', spaces, ['podi'], true, 'main'), ''), 0)
  const agentsOnly = quickOpenActionRows('', [], ['podi'], true, null)
  assert.equal(agentsOnly[quickOpenInitialIndex(agentsOnly, '')].kind, 'agent')
  assert.equal(quickOpenInitialIndex(quickOpenActionRows('', [], []), ''), -1)
  assert.equal(quickOpenInitialIndex(quickOpenActionRows('review', spaces, ['podi']), 'review'), -1)
  assert.equal(quickOpenInitialIndex(quickOpenActionRows('new place', [], []), 'new place'), -1)
})

test('an exact space or an empty query suppresses create', () => {
  assert.equal(quickOpenActionRows('main', spaces, []).some((row) => row.kind === 'create'), false)
  assert.equal(quickOpenActionRows('', spaces, []).some((row) => row.kind === 'create'), false)
})

// The top match for a typed query, as the row it highlights.
const top = (query: string, spaceList = spaces, agents: string[] = [], fileKeys: string[] = [], pending = false, activePanel = false) => {
  const rows = quickOpenActionRows(query, spaceList, agents, activePanel, 'main')
  return quickOpenTopMatch(rows, fileKeys, query, pending)
}

test('top match tier 1: an exact space name beats an exact agent and every prefix', () => {
  assert.equal(top('MAIN', spaces, ['main', 'main-agent']), 'action:space:main')
})

test('top match tier 2: an exact agent beats a space prefix', () => {
  assert.equal(top('review', spaces, ['reviewer', 'review']), 'action:agent:review')
})

test('top match tier 3: a space prefix beats an agent prefix and contains matches', () => {
  assert.equal(top('rev', spaces, ['reviewer', 'my-rev']), 'action:space:review')
})

test('top match tier 4: an agent prefix beats a space contains match', () => {
  assert.equal(top('queue', spaces, ['queue-bot']), 'action:agent:queue-bot')
})

test('top match tier 5: a space contains match beats an agent contains match', () => {
  assert.equal(top('queue', spaces, ['my-queue']), 'action:space:review')
})

test('top match tier 6: an agent contains match beats files', () => {
  assert.equal(top('liha', spaces, ['test-liha'], ['r\0liha.ts']), 'action:agent:test-liha')
  // rendered order breaks a tie inside a tier
  assert.equal(top('liha', spaces, ['test-liha', 'my-liha']), 'action:agent:test-liha')
})

test('top match tier 7: the first file when no space or agent matches, and pane actions never preselect on a partial match', () => {
  assert.equal(top('missing', spaces, ['test-liha'], ['r\0a.ts', 'r\0b.ts']), 'file:r\0a.ts')
  assert.equal(top('send', spaces, [], ['r\0send.ts'], false, true), 'file:r\0send.ts')
  assert.equal(top('Send this pane to a new space', spaces, [], [], false, true), 'action:send-new')
})

test('top match tier 8: the note, then create, once the lookup finds nothing; a running lookup selects nothing yet', () => {
  assert.equal(top('new place', spaces, []), 'action:note')
  assert.equal(top('new place', spaces, [], [], true), null)
  assert.equal(top('new place', spaces, [], ['r\0a.ts'], true), 'file:r\0a.ts')
  assert.equal(quickOpenTopMatch([{ kind: 'create', name: 'x', label: 'Create space “x”' }], [], 'x'), 'action:create:x')
  // An action match does not wait for the lookup.
  assert.equal(top('main', spaces, [], [], true), 'action:space:main')
})

test('an empty query keeps the first openable row as its top match', () => {
  assert.equal(top('', spaces, ['podi']), 'action:space:main')
  assert.equal(top('   ', [], []), null)
})

test('a typed query always highlights a row Enter acts on: Enter equals the highlighted row', () => {
  for (const [query, agents, files] of [['main', [], []], ['podi', ['podi'], []], ['missing', [], ['r\0a.ts']], ['missing', [], []], ['rev', ['reviewer'], ['r\0a.ts']]] as const) {
    const rows = quickOpenActionRows(query, spaces, [...agents])
    const selection = quickOpenSelection(rows, [...files], null, quickOpenTopMatch(rows, [...files], query))
    const index = quickOpenSelectedIndex(rows, [...files], selection)
    assert.ok(index >= 0, `${query} highlights a row`)
    const target = quickOpenEnterTarget(rows, files.length, index)
    const key = target?.kind === 'action' ? `action:${rows[target.index].kind}` : `file:${target?.kind === 'file' ? files[target.index] : ''}`
    assert.ok(selection!.startsWith(key), `${query}: Enter acts on ${key}, the highlight is ${selection}`)
  }
})

test('an arrow-moved selection survives the file results settling; an unmoved one re-ranks to the first file', () => {
  const query = 'missing'
  const rows = quickOpenActionRows(query, spaces, ['test-liha'])
  // before files: the lookup is running, nothing matches, so nothing is highlighted
  assert.equal(quickOpenSelection(rows, [], null, quickOpenTopMatch(rows, [], query, true)), null)
  const moved = quickOpenMoveSelection(rows, [], null, 'up')
  assert.equal(moved, 'action:note')
  const files = ['r\0a.ts', 'r\0b.ts']
  assert.equal(quickOpenSelection(rows, files, moved, quickOpenTopMatch(rows, files, query)), 'action:note')
  assert.equal(quickOpenSelection(rows, files, null, quickOpenTopMatch(rows, files, query)), 'file:r\0a.ts')
  // a moved file that vanishes falls back to the top match
  assert.equal(quickOpenSelection(rows, ['r\0b.ts'], 'file:r\0a.ts', quickOpenTopMatch(rows, ['r\0b.ts'], query)), 'file:r\0b.ts')
})

test('a query edit re-picks the top match (the component clears the moved row on every edit)', () => {
  const first = quickOpenActionRows('rev', spaces, ['podi'])
  const moved = quickOpenMoveSelection(first, [], quickOpenTopMatch(first, [], 'rev'), 'down')
  assert.notEqual(moved, 'action:space:review')
  const edited = quickOpenActionRows('podi', spaces, ['podi'])
  assert.equal(quickOpenSelection(edited, [], null, quickOpenTopMatch(edited, [], 'podi')), 'action:agent:podi')
})

test('create is never the top match while a note row exists, but an arrow-selected create is what Enter takes', () => {
  const rows = quickOpenActionRows('new place', spaces, [])
  const createIndex = rows.findIndex((row) => row.kind === 'create')
  assert.equal(quickOpenTopMatch(rows, [], 'new place'), 'action:note')
  assert.deepEqual(quickOpenEnterTarget(rows, 0, quickOpenSelectedIndex(rows, [], 'action:create:new place')), { kind: 'action', index: createIndex })
})

test('pane-send rows require an active pane and exclude the current space', () => {
  assert.equal(quickOpenActionRows('send', spaces, []).some((row) => row.kind.startsWith('send')), false)
  const rows = quickOpenActionRows('send', spaces, [], true, 'main')
  assert.deepEqual(rows.filter((row) => row.kind === 'send-space').map((row) => row.label), [
    'Send this pane to review queue', 'Send this pane to weekly review',
  ])
  assert.equal(rows.some((row) => row.kind === 'send-new'), true)
})

test('selection keeps its identity: a selected note stays selected when files arrive, a vanished file selection is gone', () => {
  const rows = quickOpenActionRows('missing', spaces, ['missing-helper'])
  assert.deepEqual(quickOpenSelectionKeys(rows, ['r\0a.ts']), ['action:create:missing', 'action:agent:missing-helper', 'file:r\0a.ts', 'action:note'])
  const selected = quickOpenMoveSelection(rows, [], null, 'up')
  assert.equal(selected, 'action:note')
  assert.equal(quickOpenSelectedIndex(rows, [], selected), 2)
  assert.equal(quickOpenSelectedIndex(rows, ['r\0a.ts', 'r\0b.ts'], selected), 4)
  const file = quickOpenMoveSelection(rows, ['r\0a.ts', 'r\0b.ts'], 'action:agent:missing-helper', 'down')
  assert.equal(file, 'file:r\0a.ts')
  assert.equal(quickOpenSelectedIndex(rows, ['r\0a.ts', 'r\0b.ts'], file), 2)
  assert.equal(quickOpenSelectedIndex(rows, ['r\0b.ts'], file), -1)
  assert.equal(quickOpenSelectedIndex(rows, [], null), -1)
})

test('arrow moves wrap both ways and start from the ends when nothing is selected', () => {
  const rows = quickOpenActionRows('missing', spaces, ['missing-helper'])
  const keys = quickOpenSelectionKeys(rows, ['r\0a.ts'])
  assert.equal(quickOpenMoveSelection(rows, ['r\0a.ts'], null, 'down'), keys[0])
  assert.equal(quickOpenMoveSelection(rows, ['r\0a.ts'], null, 'up'), keys.at(-1))
  assert.equal(quickOpenMoveSelection(rows, ['r\0a.ts'], keys.at(-1), 'down'), keys[0])
  assert.equal(quickOpenMoveSelection(rows, ['r\0a.ts'], keys[0], 'up'), keys.at(-1))
  assert.equal(quickOpenMoveSelection(rows, ['r\0a.ts'], keys[1], 'down'), keys[2])
  assert.equal(quickOpenMoveSelection([], [], null, 'down'), null)
  // A gone selection restarts from the end the arrow points at.
  assert.equal(quickOpenMoveSelection(rows, [], 'file:r\0a.ts', 'down'), keys[0])
})

test('the initial selection is the first openable row as a key on an empty query, left to the top match on a typed one', () => {
  assert.equal(quickOpenInitialSelection(quickOpenActionRows('', spaces, ['podi'], true, 'main'), ''), 'action:space:main')
  assert.equal(quickOpenInitialSelection(quickOpenActionRows('', [], ['podi']), ''), 'action:agent:podi')
  assert.equal(quickOpenInitialSelection(quickOpenActionRows('', [], []), ''), null)
  assert.equal(quickOpenInitialSelection(quickOpenActionRows('review', spaces, ['podi']), 'review'), null)
})

test('an active agent pane contributes a searchable reassign action', () => {
  const empty = quickOpenActionRows('', spaces, ['nota'], true, 'main', 'nota')
  assert.deepEqual(empty.find((row) => row.kind === 'reassign-action'), { kind: 'reassign-action', subject: 'nota', label: 'Reassign nota…' })
  assert.equal(quickOpenActionRows('reassign', spaces, ['nota'], true, 'main', 'nota').some((row) => row.kind === 'reassign-action'), true)
  assert.equal(quickOpenActionRows('nota', spaces, ['nota'], true, 'main', 'nota').some((row) => row.kind === 'reassign-action'), true)
  assert.equal(quickOpenActionRows('unrelated', spaces, ['nota'], true, 'main', 'nota').some((row) => row.kind === 'reassign-action'), false)
})
