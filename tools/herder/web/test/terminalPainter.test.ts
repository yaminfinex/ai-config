import assert from 'node:assert/strict'
import test from 'node:test'

import { SnapshotPainter, snapshotRepaint } from '../src/features/screen/terminalPainter.ts'
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
