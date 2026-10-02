import assert from 'node:assert/strict'
import test from 'node:test'
import type { ReadMarker } from '../src/features/spaces/readMarkerModel.ts'
import { dividerIndex, lastTurnStart, latestPosition, markLastTurnUnread, markUnreadAt, positionBefore, positionWriteMs, readThrough, viewedAtLabel } from '../src/features/spaces/readPositionModel.ts'
import { dividerRow, type CleanRow } from '../src/features/transcript/cleanRows.ts'
import { blockMenuIndex, initialDividerSnapshot, nextDividerSnapshot } from '../src/features/transcript/transcriptReadModel.ts'
import type { EntriesPage, EntryKind, TranscriptEntry } from '../src/types.ts'

const entry = (kind: EntryKind, byteOffset: number): TranscriptEntry => ({ kind, byteOffset, line: byteOffset, timestamp: `t${byteOffset}`, payload: null })
const page = (entries: TranscriptEntry[], sessionId = 's1'): EntriesPage => ({ sessionId, window: { mode: 'tail', from: 0, limit: 200 }, entries })
const pos = (offset: number, session = 's1') => ({ session, offset, ts: `t${offset}` })
const marker = (turn: number, offset: number | null, at = 1000, unread = false): ReadMarker => ({ turn, pos: offset === null ? null : pos(offset), at, unread })

const transcript = [
  entry('human_prompt', 100),
  entry('assistant_text', 200),
  entry('hcom_delivery_stub', 300),
  entry('hcom_delivery', 400),
  entry('tool_use', 500),
  entry('assistant_text', 600),
] as TranscriptEntry[]

test('the latest position is the newest rendered entry, null for an empty window', () => {
  assert.deepEqual(latestPosition(page(transcript)), pos(600))
  assert.equal(latestPosition(page([])), null)
  assert.equal(latestPosition(undefined), null)
})

test('the latest turn starts at its opener, a delivery stub and its delivery together', () => {
  assert.equal(lastTurnStart(transcript), 2)
  assert.equal(lastTurnStart(transcript.slice(0, 2)), 0)
  assert.equal(lastTurnStart([entry('assistant_text', 1), entry('tool_use', 2)] as TranscriptEntry[]), 1, 'no opener: the last entry')
  assert.equal(lastTurnStart([]), -1)
})

test('mark unread moves the position to just before the chosen entry and keeps the turn', () => {
  assert.deepEqual(positionBefore('s1', transcript, 2), pos(200))
  assert.deepEqual(positionBefore('s1', transcript, 0), { session: 's1', offset: 99, ts: '' }, 'the window opener: one byte short of it')
  assert.equal(positionBefore('s1', transcript, 9), null)
  assert.deepEqual(markLastTurnUnread(marker(7, 600), page(transcript)), { turn: 7, pos: pos(200), at: 1000, unread: true })
  assert.deepEqual(markUnreadAt(marker(7, 600), page(transcript), 4), { turn: 7, pos: pos(400), at: 1000, unread: true }, 'mark unread from here')
  assert.deepEqual(markLastTurnUnread(marker(7, 600), undefined), { ...marker(7, 600), unread: true }, 'no window: the position stays')
  assert.deepEqual(markLastTurnUnread(undefined, undefined), { turn: 0, pos: null, at: 0, unread: true })
})

test('reading records the turn and the newest entry, never backward, and clears a mark', () => {
  assert.deepEqual(readThrough(undefined, 5, pos(600), 2000), { turn: 5, pos: pos(600), at: 2000, unread: false })
  assert.equal(readThrough(marker(5, 600), 5, pos(600), 9000), null, 'nothing new: no write')
  assert.deepEqual(readThrough(marker(9, 600), 5, pos(300), 9000), null, 'an older turn and position move nothing')
  assert.deepEqual(readThrough(marker(5, 600), 6, pos(700), 1500), { turn: 6, pos: pos(700), at: 1500, unread: false }, 'a turn end writes at once')
  assert.deepEqual(readThrough(marker(5, 600, 1000, true), 5, pos(600), 1100), { turn: 5, pos: pos(600), at: 1100, unread: false }, 'clearing a mark writes at once')
  assert.deepEqual(readThrough(marker(5, 600), null, null, 3000), null, 'no turn end and no transcript: nothing')
  assert.deepEqual(readThrough(marker(5, 600), 5, pos(40, 's2'), 1500), { turn: 5, pos: pos(40, 's2'), at: 1500, unread: false }, 'a new session writes at once')
})

test('a position creeping forward inside a read turn is throttled', () => {
  assert.equal(readThrough(marker(5, 600), 5, pos(650), 1000 + positionWriteMs - 1), null)
  assert.deepEqual(readThrough(marker(5, 600), 5, pos(650), 1000 + positionWriteMs), { turn: 5, pos: pos(650), at: 1000 + positionWriteMs, unread: false })
})

test('the divider sits above the first entry after the position, in the same session only', () => {
  assert.equal(dividerIndex(transcript, 's1', pos(200)), 2)
  assert.equal(dividerIndex(transcript, 's1', pos(99)), 0)
  assert.equal(dividerIndex(transcript, 's1', pos(600)), -1, 'all read: no divider')
  assert.equal(dividerIndex(transcript, 's2', pos(200)), -1, 'another session: no divider')
  assert.equal(dividerIndex(transcript, 's1', null), -1)
  assert.equal(dividerIndex(transcript, undefined, pos(200)), -1)
})

test('in the clean view the divider sits above the row drawing that entry', () => {
  const run = (key: string, indices: number[]): CleanRow => ({ type: 'run', key, activities: indices.map((index) => ({ key: `${index}`, label: '', tone: 'neutral', index })) as never })
  const one = (index: number): CleanRow => ({ type: 'entry', key: `${index}`, entry: transcript[index]!, index })
  const rows = [one(0), run('r', [1, 2, 3]), one(5)]
  assert.equal(dividerRow(rows, 2), 1, 'inside a run: above the run')
  assert.equal(dividerRow(rows, 4), 2, 'a hidden entry: the next row drawn')
  assert.equal(dividerRow(rows, 0), 0)
  assert.equal(dividerRow(rows, -1), -1)
  assert.equal(dividerRow(rows, 9), -1)
})

test('the divider snapshots the position on arrival and holds it while reading', () => {
  const arrived = nextDividerSnapshot(initialDividerSnapshot, true, marker(5, 200))
  assert.deepEqual(arrived.pos, pos(200))
  assert.equal(nextDividerSnapshot(arrived, true, marker(6, 600)), arrived, 'reading on does not move it')
  const left = nextDividerSnapshot(arrived, false, marker(6, 600))
  assert.deepEqual(left.pos, pos(200), 'leaving keeps it until the next arrival')
  assert.deepEqual(nextDividerSnapshot(left, true, marker(6, 600)).pos, pos(600), 'the next arrival moves it')
})

test('the divider waits for a marker that loads after arrival, and moves on a mark unread', () => {
  const early = nextDividerSnapshot(initialDividerSnapshot, true, undefined)
  assert.equal(early.pos, null)
  const loaded = nextDividerSnapshot(early, true, marker(5, 300))
  assert.deepEqual(loaded.pos, pos(300), 'the first marker settles it')
  assert.equal(nextDividerSnapshot(loaded, true, marker(5, 400)), loaded)
  const marked = nextDividerSnapshot(loaded, true, marker(5, 100, 1000, true))
  assert.deepEqual(marked.pos, pos(100), 'marked unread here or on another device: it moves')
  assert.equal(nextDividerSnapshot(marked, true, marker(5, 100, 1000, true)), marked)
  assert.equal(nextDividerSnapshot(initialDividerSnapshot, false, marker(5, 300)).pos, null, 'never taken while not active')
})

test('a repeated mark unread while already unread moves the divider again', () => {
  const arrived = nextDividerSnapshot(initialDividerSnapshot, true, marker(5, 300))
  const first = nextDividerSnapshot(arrived, true, marker(5, 200, 1000, true))
  assert.deepEqual(first.pos, pos(200))
  // Locally: the same agent stays open and the owner marks from an earlier block.
  const again = nextDividerSnapshot(first, true, marker(5, 0, 1000, true))
  assert.deepEqual(again.pos, pos(0), 'a second local mark from here moves it')
  // Remotely: the merged row arrives with a later position, still unread.
  const remote = nextDividerSnapshot(again, true, marker(5, 100, 1000, true))
  assert.deepEqual(remote.pos, pos(100), 'a second mark from another device moves it')
  assert.equal(nextDividerSnapshot(remote, true, marker(5, 100, 1000, true)), remote, 'the same mark again changes nothing')
  // Once read, advancing positions leave it frozen until the next arrival.
  const read = nextDividerSnapshot(remote, true, marker(6, 500))
  assert.deepEqual(read.pos, pos(100))
  assert.equal(nextDividerSnapshot(read, true, marker(6, 600)), read, 'ordinary reading still never moves it')
})

test('the block menu opens on a block, not over a selection, a link or a text box', () => {
  const target = (attrs: Record<string, string | null>, editable = false) => ({
    closest: (selector: string) => {
      if (selector.startsWith('a, input')) return editable ? { getAttribute: () => null } : null
      return attrs['data-entry-index'] === undefined ? null : { getAttribute: (name: string) => attrs[name] ?? null }
    },
  })
  assert.equal(blockMenuIndex(target({ 'data-entry-index': '4' }), ''), 4)
  assert.equal(blockMenuIndex(target({ 'data-entry-index': '4' }), 'some text'), null)
  assert.equal(blockMenuIndex(target({ 'data-entry-index': '4' }, true), ''), null)
  assert.equal(blockMenuIndex(target({}), ''), null)
  assert.equal(blockMenuIndex(target({ 'data-entry-index': 'x' }), ''), null)
  assert.equal(blockMenuIndex(null, ''), null)
})

test('the footer reads the local time of the last read, with the date once it is not today', () => {
  assert.equal(viewedAtLabel(0, Date.now()), '')
  const at = new Date(2026, 9, 2, 14, 5).getTime()
  assert.equal(viewedAtLabel(at, new Date(2026, 9, 2, 20, 0).getTime()), '14:05', 'HH:MM, 24-hour')
  assert.equal(viewedAtLabel(new Date(2026, 9, 2, 9, 7).getTime(), new Date(2026, 9, 2, 20, 0).getTime()), '09:07')
  assert.match(viewedAtLabel(at, new Date(2026, 9, 4, 9, 0).getTime()), /(Oct\s*2|2\s*Oct).* 14:05$/)
})
