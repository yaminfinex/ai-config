import assert from 'node:assert/strict'
import test from 'node:test'

import { SnapshotPainter, snapshotRepaint, snapshotUpdate, terminalReset } from '../src/features/screen/terminalPainter.ts'
import { busyPaneANSI } from './fixtures/busyPaneANSI.ts'

test('a captured busy-pane ANSI snapshot is reset and repainted as one write', () => {
  const repaint = snapshotRepaint(busyPaneANSI)
  assert.equal(repaint, `\x1bc${busyPaneANSI}`)
  assert.ok(repaint.includes('\x1b['))
})

test('snapshot painter keeps one in-flight repaint and coalesces to the latest frame', () => {
  const writes: Array<{ data: string, done: () => void }> = []
  const painter = new SnapshotPainter((data, done) => writes.push({ data, done }))
  painter.paint('first')
  painter.paint('superseded')
  painter.paint('latest')
  assert.deepEqual(writes.map(({ data }) => data), ['\x1bcfirst'])
  writes[0].done()
  assert.deepEqual(writes.map(({ data }) => data), ['\x1bcfirst', '\x1bclatest'])
  writes[1].done()
  assert.equal(painter.pending, false)
})

test('snapshot painter removes the capture protocol trailing line delimiter', () => {
  const writes: Array<{ data: string, done: () => void }> = []
  const painter = new SnapshotPainter((data, done) => writes.push({ data, done }))
  painter.paint('first\r\nsecond\r\n')
  assert.equal(writes[0].data, '\x1bcfirst\r\nsecond\r')
})

test('a small snapshot change repaints only its dirty terminal rows', () => {
  const previous = ['first\r', 'old second\r', 'third\r'].join('\n')
  const next = ['first\r', 'new second\r', 'third\r'].join('\n')
  const update = snapshotUpdate(previous, next)
  assert.equal(update, '\x1b7\x1b[2;1H\x1b[0m\x1b[2Knew second\r\x1b8')
  assert.ok(!update?.includes(terminalReset))
})

test('a shape-wide snapshot change falls back to one full repaint', () => {
  const previous = ['one\r', 'two\r', 'three\r'].join('\n')
  const next = ['changed one\r', 'changed two\r', 'three\r'].join('\n')
  assert.equal(snapshotUpdate(previous, next), snapshotRepaint(next))
})

test('a snapshot row-count change falls back to one full repaint', () => {
  const previous = ['one\r', 'two\r'].join('\n')
  const next = ['one\r', 'two\r', 'three\r'].join('\n')
  assert.equal(snapshotUpdate(previous, next), snapshotRepaint(next))
})

test('snapshot painter skips raster while inactive and catches up once visible', () => {
  const writes: Array<{ data: string, done: () => void }> = []
  const painter = new SnapshotPainter((data, done) => writes.push({ data, done }))
  painter.paint('first', false)
  painter.paint('latest', false)
  assert.deepEqual(writes, [])
  assert.equal(painter.pending, false)
  painter.paint('latest', true)
  assert.deepEqual(writes.map(({ data }) => data), ['\x1bclatest'])
})

test('queued snapshots diff from the last completed terminal paint', () => {
  const writes: Array<{ data: string, done: () => void }> = []
  const painter = new SnapshotPainter((data, done) => writes.push({ data, done }))
  painter.paint('one\r\ntwo\r\nthree\r')
  painter.paint('one\r\nchanged\r\nthree\r')
  writes[0].done()
  assert.deepEqual(writes.map(({ data }) => data), [
    '\x1bcone\r\ntwo\r\nthree\r',
    '\x1b7\x1b[2;1H\x1b[0m\x1b[2Kchanged\r\x1b8',
  ])
})

test('reset forces a full repaint after terminal geometry changes', () => {
  const writes: Array<{ data: string, done: () => void }> = []
  const painter = new SnapshotPainter((data, done) => writes.push({ data, done }))
  painter.paint('one\r\ntwo\r\nthree\r')
  writes[0].done()
  painter.reset()
  painter.paint('one\r\nchanged\r\nthree\r')
  assert.equal(writes[1].data, '\x1bcone\r\nchanged\r\nthree\r')
})

test('an in-flight write cannot restore a baseline invalidated by resize', () => {
  const writes: Array<{ data: string, done: () => void }> = []
  const painter = new SnapshotPainter((data, done) => writes.push({ data, done }))
  painter.paint('old')
  painter.reset()
  painter.paint('latest')
  writes[0].done()
  assert.equal(writes[1].data, '\x1bclatest')
})

test('non-incremental painters fully repaint history snapshots', () => {
  const writes: Array<{ data: string, done: () => void }> = []
  const painter = new SnapshotPainter((data, done) => writes.push({ data, done }), false)
  painter.paint('old')
  writes[0].done()
  painter.paint('new')
  assert.deepEqual(writes.map(({ data }) => data), ['\x1bcold', '\x1bcnew'])
})

test('hiding during an in-flight write drops queued raster and reactivates from the completed baseline', () => {
  const writes: Array<{ data: string, done: () => void }> = []
  const painter = new SnapshotPainter((data, done) => writes.push({ data, done }))
  painter.paint('one\r\ntwo\r\nthree\r')
  painter.paint('one\r\nqueued\r\nthree\r')
  painter.paint('one\r\nhidden\r\nthree\r', false)
  writes[0].done()
  assert.equal(writes.length, 1)
  painter.paint('one\r\nlatest\r\nthree\r', true)
  assert.equal(writes[1].data, '\x1b7\x1b[2;1H\x1b[0m\x1b[2Klatest\r\x1b8')
})
