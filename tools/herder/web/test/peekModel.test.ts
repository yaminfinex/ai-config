import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

import { lensPrefsKey, parseLensPrefs, readLensPrefs, writeLensPrefs } from '../src/features/lens/lensPrefs.ts'
import { clampPeekWidth, maxPeekWidth, minPeekWidth, peekTail, peekWidth, peekWidthFromKey, peekWindow, resizedPeekWidth } from '../src/features/lens/peekModel.ts'
import type { TranscriptEntry } from '../src/types.ts'

const entry = (index: number): TranscriptEntry => ({ line: index + 1, byteOffset: index * 10, kind: 'assistant_text', payload: {} })

test('the peek takes half the lens; its edge and keys resize it within 20%–60%', () => {
  assert.equal(peekWidth, 0.5)
  assert.deepEqual([minPeekWidth, maxPeekWidth], [0.2, 0.6])
  assert.equal(clampPeekWidth(0.05), 0.2)
  assert.equal(clampPeekWidth(0.9), 0.6)
  assert.equal(resizedPeekWidth(0.5, 1000, -100), 0.6, 'dragging the edge left widens it')
  assert.equal(resizedPeekWidth(0.5, 1000, 50), 0.45)
  assert.equal(resizedPeekWidth(0.5, 0, 50), 0.5, 'an unmeasured lens leaves it')
  assert.equal(peekWidthFromKey(0.5, 'ArrowLeft'), 0.52)
  assert.equal(peekWidthFromKey(0.5, 'ArrowRight'), 0.48)
  assert.equal(peekWidthFromKey(0.6, 'ArrowLeft'), 0.6)
})

test('the peek width persists in herder.web.lens.v1 as peekWidth; a bad one costs only itself', () => {
  assert.deepEqual(parseLensPrefs(JSON.stringify({ version: 1, rows: {}, visible: {}, peekWidth: 0.42 })), { rows: {}, visible: {}, peek: 0.42 })
  for (const peek of [0.1, 0.7, '0.3', null]) assert.deepEqual(parseLensPrefs(JSON.stringify({ version: 1, rows: {}, visible: {}, size: 1, peekWidth: peek })), { rows: {}, visible: {}, size: 1 })
  const source = readFileSync(new URL('../src/features/lens/lensPrefs.ts', import.meta.url), 'utf8')
  assert.match(source, /peekWidth: prefs\.peek \}\)/)
})

test('a width stored while the default was 30% is dropped once, and the rest of the record kept', () => {
  const old = JSON.stringify({ version: 1, rows: { a: 'focus' }, visible: { a: 'mavu' }, text: 'status', size: 2, peek: 0.32 })
  assert.deepEqual(parseLensPrefs(old), { rows: { a: 'focus' }, visible: { a: 'mavu' }, text: 'status', size: 2 }, 'the old peek field is not read')
  const storage = new Map<string, string>([[lensPrefsKey, old]])
  const local = { getItem: (key: string) => storage.get(key) ?? null, setItem: (key: string, value: string) => { storage.set(key, value) } }
  const read = readLensPrefs(local)
  assert.equal(read.prefs.peek, undefined, 'the peek starts at the default')
  // The next write carries no old field, and a width set afterwards persists.
  writeLensPrefs(local, read.prefs, read.state)
  assert.equal(JSON.parse(storage.get(lensPrefsKey)!).peek, undefined)
  writeLensPrefs(local, { ...read.prefs, peek: 0.54 }, read.state)
  assert.equal(JSON.parse(storage.get(lensPrefsKey)!).peekWidth, 0.54)
  assert.equal(readLensPrefs(local).prefs.peek, 0.54, 'a width set at the new default survives a reload')
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

test('peeking is not viewing: no read, no block viewed, no composer until /, the stream it already has, the keys left with the lens', () => {
  const peek = readFileSync(new URL('../src/features/lens/LensPeek.tsx', import.meta.url), 'utf8')
  assert.match(peek, /useQuery\(entriesQueryOptions\(queryClient, agent\)\)/, 'the shared entries cache')
  assert.doesNotMatch(peek, /EventSource|useFleetStream|markRead|readThrough|viewBlocks|useSpaceAttention|DockviewReact/)
  assert.doesNotMatch(peek, /autoFocus|tabIndex=\{-1\}/, 'the peek never takes focus on its own')
  // The one composer is the reply (owner #475682): mounted only by /, and the
  // only focus it takes is the one / asks for.
  const replyStart = peek.indexOf('// ReplyComposer is')
  const replyEnd = peek.indexOf('\n}\n', replyStart)
  const reply = peek.slice(replyStart, replyEnd)
  const rest = peek.slice(0, replyStart) + peek.slice(replyEnd)
  assert.ok(replyStart > 0 && replyEnd > replyStart)
  assert.doesNotMatch(rest.replace("import { Composer } from '../composer/Composer'", ''), /\bComposer\b|\.focus\(\)/, 'no composer and no focus outside the reply')
  assert.match(rest, /\{reply\?\.agent === target\.agent && <ReplyComposer key=\{reply\.agent\}/, 'the reply only for the peeked agent, after /')
  assert.match(reply, /useEffect\(\(\) => \{ box\.current\?\.querySelector<HTMLTextAreaElement>\('textarea\[data-composer\]'\)\?\.focus\(\) \}, \[focus\]\)/, 'it takes the keys when / asks')
  assert.match(reply, /<Composer name=\{agent\}/, 'the zoom\'s own Composer')
  const lens = readFileSync(new URL('../src/features/lens/LensView.tsx', import.meta.url), 'utf8')
  assert.match(lens, /<section className="lens"[^>]*onClick=\{focusLens\}>[\s\S]*<LensPeek /, 'a click in the peek leaves the keys with the lens')
  const hook = readFileSync(new URL('../src/features/lens/useLens.ts', import.meta.url), 'utf8')
  assert.match(hook, /zoomOut: \(\) => !enabled \? false : view\.kind !== 'lens' \? zoomOut\(\) : leavePeek\(\)/, 'esc in the peek returns to the cards')
  const controller = readFileSync(new URL('../src/features/workspace/useWorkspaceController.ts', import.meta.url), 'utf8')
  assert.match(controller, /const peeked = view\.kind === 'lens' && lens\.peek\?\.agent \? \[lens\.peek\.agent\] : \[\]\n {2}useFleetStream\(\{ agents: spaceStreamAgents\(\[\.\.\.agentNames, \.\.\.peeked\], openBySpace\)/, 'the one stream follows the peeked agent')
  const attention = readFileSync(new URL('../src/features/workspace/useSpaceAttention.ts', import.meta.url), 'utf8')
  assert.match(attention, /const viewedKey = view\.kind === 'lens' \? ''/, 'nothing on the lens is viewed')
})
