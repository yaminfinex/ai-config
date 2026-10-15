import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

import { parseLensPrefs } from '../src/features/lens/lensPrefs.ts'
import { clampPeekWidth, maxPeekWidth, minPeekWidth, peekTail, peekWidth, peekWidthFromKey, peekWindow, resizedPeekWidth } from '../src/features/lens/peekModel.ts'
import type { TranscriptEntry } from '../src/types.ts'

const entry = (index: number): TranscriptEntry => ({ line: index + 1, byteOffset: index * 10, kind: 'assistant_text', payload: {} })

test('the peek takes 30% of the lens; its edge and keys resize it within 20%–60%', () => {
  assert.equal(peekWidth, 0.3)
  assert.deepEqual([minPeekWidth, maxPeekWidth], [0.2, 0.6])
  assert.equal(clampPeekWidth(0.05), 0.2)
  assert.equal(clampPeekWidth(0.9), 0.6)
  assert.equal(resizedPeekWidth(0.3, 1000, -100), 0.4, 'dragging the edge left widens it')
  assert.equal(resizedPeekWidth(0.3, 1000, 50), 0.25)
  assert.equal(resizedPeekWidth(0.3, 0, 50), 0.3, 'an unmeasured lens leaves it')
  assert.equal(peekWidthFromKey(0.3, 'ArrowLeft'), 0.32)
  assert.equal(peekWidthFromKey(0.3, 'ArrowRight'), 0.28)
  assert.equal(peekWidthFromKey(0.6, 'ArrowLeft'), 0.6)
})

test('the peek width persists in herder.web.lens.v1; a bad one costs only itself', () => {
  assert.deepEqual(parseLensPrefs(JSON.stringify({ version: 1, rows: {}, visible: {}, peek: 0.42 })), { rows: {}, visible: {}, peek: 0.42 })
  for (const peek of [0.1, 0.7, '0.3', null]) assert.deepEqual(parseLensPrefs(JSON.stringify({ version: 1, rows: {}, visible: {}, size: 1, peek })), { rows: {}, visible: {}, size: 1 })
  const source = readFileSync(new URL('../src/features/lens/lensPrefs.ts', import.meta.url), 'utf8')
  assert.match(source, /peek: prefs\.peek \}\)/)
})

test('the peek draws the newest entries, its unread line moved into the window', () => {
  const all = Array.from({ length: peekTail + 20 }, (_, index) => entry(index))
  const window = peekWindow(all, peekTail + 5)
  assert.equal(window.entries.length, peekTail)
  assert.equal(window.entries[0], all[20])
  assert.equal(window.dividerAt, peekTail - 15)
  assert.equal(peekWindow(all, 3).dividerAt, 0, 'a line before the window: everything shown is unread')
  assert.equal(peekWindow(all, -1).dividerAt, -1)
  assert.deepEqual(peekWindow(all.slice(0, 3), 1), { entries: all.slice(0, 3), dividerAt: 1 })
})

test('peeking is not viewing: no read, no block viewed, no composer, the stream it already has, the keys left with the lens', () => {
  const peek = readFileSync(new URL('../src/features/lens/LensPeek.tsx', import.meta.url), 'utf8')
  assert.match(peek, /useQuery\(entriesQueryOptions\(queryClient, agent\)\)/, 'the shared entries cache')
  assert.doesNotMatch(peek, /EventSource|useFleetStream|markRead|readThrough|viewBlocks|useSpaceAttention|Composer|DockviewReact|\.focus\(\)/)
  assert.doesNotMatch(peek, /autoFocus|tabIndex=\{-1\}/, 'the peek never takes focus on its own')
  const lens = readFileSync(new URL('../src/features/lens/LensView.tsx', import.meta.url), 'utf8')
  assert.match(lens, /<section className="lens"[^>]*onClick=\{focusLens\}>[\s\S]*<LensPeek /, 'a click in the peek leaves the keys with the lens')
  const hook = readFileSync(new URL('../src/features/lens/useLens.ts', import.meta.url), 'utf8')
  assert.match(hook, /zoomOut: \(\) => !enabled \? false : view\.kind !== 'lens' \? zoomOut\(\) : leavePeek\(\)/, 'esc in the peek returns to the cards')
  const controller = readFileSync(new URL('../src/features/workspace/useWorkspaceController.ts', import.meta.url), 'utf8')
  assert.match(controller, /const peeked = view\.kind === 'lens' && lens\.peek\?\.agent \? \[lens\.peek\.agent\] : \[\]\n {2}useFleetStream\(\{ agents: spaceStreamAgents\(\[\.\.\.agentNames, \.\.\.peeked\], openBySpace\)/, 'the one stream follows the peeked agent')
  const attention = readFileSync(new URL('../src/features/workspace/useSpaceAttention.ts', import.meta.url), 'utf8')
  assert.match(attention, /const viewedKey = view\.kind === 'lens' \? ''/, 'nothing on the lens is viewed')
})
