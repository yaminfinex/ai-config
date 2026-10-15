import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

import {
  agentFolder,
  answerText,
  cardAnswer,
  cardReadCap,
  cardTail,
  dueCardReads,
  flattenAnswer,
  keepCardReads,
  landCardRead,
  lastAnswer,
  nextCardSize,
  nextCardText,
  noCardReads,
  startCardReads,
  type CardTurn,
} from '../src/features/lens/cardTextModel.ts'
import { cardShape, focusSizes } from '../src/features/lens/lensModel.ts'
import { parseLensPrefs } from '../src/features/lens/lensPrefs.ts'
import type { Board, TranscriptEntry } from '../src/types.ts'

const said = (text: string, extra: Record<string, unknown> = {}): TranscriptEntry =>
  ({ line: 1, byteOffset: 0, kind: 'assistant_text', payload: { message: { content: [{ type: 'text', text }] }, ...extra } })
const prompt = (text: string): TranscriptEntry => ({ line: 1, byteOffset: 0, kind: 'human_prompt', payload: { message: { content: text } } })
const cards = (...pairs: [string, CardTurn][]) => new Map(pairs)

test('card reads: at most four in flight, one per agent, in card order', () => {
  assert.equal(cardTail, 12)
  assert.equal(cardReadCap, 4)
  const carded = cards(['a', 1], ['b', 2], ['c', 3], ['d', 4], ['e', 5], ['f', 6])
  const due = dueCardReads(noCardReads, carded)
  assert.deepEqual(due.map((read) => read.agent), ['a', 'b', 'c', 'd'])
  const flying = startCardReads(noCardReads, due)
  assert.deepEqual(dueCardReads(flying, carded), [], 'no slot is free')
  // One lands: one slot frees, and only e (not a again) is asked.
  const landed = landCardRead(flying, { agent: 'a', turn: 1 }, [said('done')], carded)
  assert.deepEqual(dueCardReads(landed, carded), [{ agent: 'e', turn: 5 }])
  assert.equal(cardAnswer(landed, 'a'), 'done')
})

test('card reads: keyed on the turn; a turn end reads again, the same turn never', () => {
  const read = landCardRead(startCardReads(noCardReads, [{ agent: 'a', turn: 7 }]), { agent: 'a', turn: 7 }, [said('first')], cards(['a', 7]))
  assert.deepEqual(dueCardReads(read, cards(['a', 7])), [], 'the same turn is not read again')
  assert.deepEqual(dueCardReads(read, cards(['a', 8])), [{ agent: 'a', turn: 8 }], 'a new turn end reads again')
  // A read that finds no answer (a tail of tools) keeps the text it had.
  const quiet = landCardRead(startCardReads(read, [{ agent: 'a', turn: 8 }]), { agent: 'a', turn: 8 }, [prompt('go on')], cards(['a', 8]))
  assert.equal(cardAnswer(quiet, 'a'), 'first')
  assert.deepEqual(dueCardReads(quiet, cards(['a', 8])), [])
  // No turn yet still reads once.
  assert.deepEqual(dueCardReads(noCardReads, cards(['z', null])), [{ agent: 'z', turn: null }])
})

test('card reads: a stale turn or an agent off the text cards is dropped, and the current turn read', () => {
  const flying = startCardReads(noCardReads, [{ agent: 'a', turn: 1 }, { agent: 'b', turn: 1 }])
  // a's turn moved on while its read flew: dropped, the slot freed, the new turn due.
  const stale = landCardRead(flying, { agent: 'a', turn: 1 }, [said('old answer')], cards(['a', 2], ['b', 1]))
  assert.equal(cardAnswer(stale, 'a'), null)
  assert.deepEqual(dueCardReads(stale, cards(['a', 2], ['b', 1])), [{ agent: 'a', turn: 2 }])
  // b left the text cards (its space went to background): dropped.
  const gone = landCardRead(stale, { agent: 'b', turn: 1 }, [said('b answer')], cards(['a', 2]))
  assert.equal(cardAnswer(gone, 'b'), null)
  assert.deepEqual(gone.flight, {})
  // A failure is dropped the same way when stale, else held for its turn.
  const failed = landCardRead(startCardReads(noCardReads, [{ agent: 'c', turn: 4 }]), { agent: 'c', turn: 4 }, null, cards(['c', 4]))
  assert.deepEqual(dueCardReads(failed, cards(['c', 4])), [], 'a failed turn is not asked again')
  assert.deepEqual(dueCardReads(failed, cards(['c', 5])), [{ agent: 'c', turn: 5 }], 'a newer turn reads at once')
  // Agents off the board let their answers go.
  const answered = landCardRead(startCardReads(noCardReads, [{ agent: 'd', turn: 1 }]), { agent: 'd', turn: 1 }, [said('d')], cards(['d', 1]))
  assert.equal(keepCardReads(answered, () => true), answered)
  assert.equal(cardAnswer(keepCardReads(answered, () => false), 'd'), null)
})

test('card text: the last assistant answer, fences stripped, one plain paragraph', () => {
  assert.equal(lastAnswer([said('older'), said('newer'), prompt('a prompt after')]), 'newer')
  assert.equal(lastAnswer([said('kept'), said('<status>working on it</status>')]), 'kept', 'a status-only answer is not an answer')
  assert.equal(lastAnswer([said('kept'), said('<internal>notes to self</internal>')]), 'kept')
  assert.equal(answerText(said('<status>step 2 of 3</status>\nThe fix is in.\n<internal>check the gate</internal>\nShip it.')), 'The fix is in. Ship it.')
  assert.equal(answerText(said('a quota error', { isApiErrorMessage: true })), null)
  assert.equal(answerText({ line: 1, byteOffset: 0, kind: 'assistant_text', payload: { message: { content: [{ type: 'thinking', thinking: 'hmm' }, { type: 'text', text: 'said' }] } } }), 'said', 'thinking is not the answer')
  assert.equal(answerText(prompt('not assistant')), null)
  // A malformed fence is shown literally, as the transcript shows it.
  assert.equal(answerText(said('<status>open\nline</status> rest')), '<status>open line</status> rest')
})

test('card text: flattening drops headings, emphasis, ticks and link targets', () => {
  assert.equal(flattenAnswer('## Result\n\nThe **gate** is _green_ and `npm test` passes.'), 'Result The gate is green and npm test passes.')
  assert.equal(flattenAnswer('See [the brief](https://example.test/brief.md) and ![a shot](shot.png).'), 'See the brief and a shot.')
  assert.equal(flattenAnswer('- one\n- [x] two\n1. three\n> quoted *stress*'), 'one two three quoted stress')
  assert.equal(flattenAnswer('```ts\nconst a = 1\n```\n---\nafter'), 'const a = 1 after')
  assert.equal(flattenAnswer('| name | state |\n| --- | :-: |\n| lead | ok |'), 'name · state; lead · ok;')
  assert.equal(flattenAnswer('snake_case_name and 2*3*4 stay'), 'snake_case_name and 2*3*4 stay')
  assert.equal(flattenAnswer('  spaced\n\n\n   out  '), 'spaced out')
})

test('t cycles answer → status → cwd; s cycles the four focus sizes; watch keeps the focus width', () => {
  assert.equal(nextCardText('answer'), 'status')
  assert.equal(nextCardText('status'), 'cwd')
  assert.equal(nextCardText('cwd'), 'answer')
  assert.deepEqual(focusSizes, [[284, 5], [360, 8], [440, 12], [240, 3]])
  assert.deepEqual([0, 1, 2, 3].map((size) => nextCardSize(size, focusSizes.length)), [1, 2, 3, 0])
  assert.deepEqual([0, 1, 2, 3].map((size) => cardShape('focus', size)), [{ width: 284, lines: 5 }, { width: 360, lines: 8 }, { width: 440, lines: 12 }, { width: 240, lines: 3 }])
  assert.deepEqual([0, 1, 2, 3].map((size) => cardShape('watch', size).width), [284, 360, 440, 240])
  assert.deepEqual(cardShape('watch', 2), { width: 440, lines: 2 })
  assert.deepEqual(cardShape('background', 2), { width: 240, lines: 0 })
})

test('t and s persist in herder.web.lens.v1; a bad value costs only itself', () => {
  assert.deepEqual(parseLensPrefs(JSON.stringify({ version: 1, rows: {}, visible: {}, text: 'cwd', size: 2 })), { rows: {}, visible: {}, text: 'cwd', size: 2 })
  assert.deepEqual(parseLensPrefs(JSON.stringify({ version: 1, rows: { a: 'focus' }, visible: {}, text: 'poem', size: 9 })), { rows: { a: 'focus' }, visible: {} })
  assert.deepEqual(parseLensPrefs(JSON.stringify({ version: 1, rows: {}, visible: {}, size: 1.5 })), { rows: {}, visible: {} })
  const source = readFileSync(new URL('../src/features/lens/lensPrefs.ts', import.meta.url), 'utf8')
  assert.match(source, /text: prefs\.text, size: prefs\.size/)
})

test('cwd text: the agent workspace folder, else its name', () => {
  const pane = (agent: string) => ({ pane_id: agent, agent, tool: 'claude', herdr_status: 'idle', bus_status: 'listening', gap: '' })
  const board = {
    workspaces: [
      { workspace_id: 'w1', cwd: '/src/one', number: 1, label: 'one', focused: false, pane_count: 1, tab_count: 1, active_tab_id: 't', agent_status: '', tabs: [{ tab_id: 't', number: 1, label: '', focused: false, pane_count: 1, agent_status: '', panes: [pane('a')] }] },
      { workspace_id: 'w2', number: 2, label: 'two', focused: false, pane_count: 1, tab_count: 1, active_tab_id: 't', agent_status: '', tabs: [{ tab_id: 't', number: 1, label: '', focused: false, pane_count: 1, agent_status: '', panes: [{ ...pane('b'), subagents: [pane('sub')] }] }] },
    ],
    unplaced: [pane('loose')],
  } as Board
  assert.equal(agentFolder(board, 'a'), '/src/one')
  assert.equal(agentFolder(board, 'b'), 'two')
  assert.equal(agentFolder(board, 'sub'), 'two')
  assert.equal(agentFolder(board, 'loose'), 'no folder')
})

test('the lens reads card text from the entries cache or a tail read, never a second stream', () => {
  const hook = readFileSync(new URL('../src/features/lens/useCardText.ts', import.meta.url), 'utf8')
  assert.match(hook, /getQueryData<EntriesPage>\(queryKeys\.entries\(agent\)\)/, 'a cached transcript answers first')
  assert.match(hook, /getEntries\(read\.agent, \{ limit: cardTail \}\)/, 'else one tail read')
  assert.match(hook, /dueCardReads\(readsRef\.current/)
  assert.match(hook, /landCardRead\(readsRef\.current, read, entries, wantedRef\.current\)/, 'a read lands against the cards as they are then')
  assert.doesNotMatch(hook, /EventSource|useFleetStream/)
  const lens = readFileSync(new URL('../src/features/lens/useLens.ts', import.meta.url), 'utf8')
  assert.match(lens, /rowOf\(prefs, space\.id\) === 'background'\) return \[\]/, 'background cards are never read')
  assert.match(lens, /text === 'answer' \? answer\(agent\) \?\? fallback/, 'the status shows until the answer loads')
})
