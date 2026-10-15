import assert from 'node:assert/strict'
import test from 'node:test'

import { cellAt, metaKey, mouseModes, mouseReport, wantsMove, type MouseModes } from '../src/features/herdr/terminalInput.ts'

const modesOf = (...on: number[]) => mouseModes((mode) => on.includes(mode))
const at = { col: 12, row: 5, shift: false, alt: false, ctrl: false }
const sgr: MouseModes = { tracking: 'normal', encoding: 'sgr' }

test('the client\'s mouse modes: the widest tracking and its encoding', () => {
  assert.deepEqual(modesOf(), { tracking: 'off', encoding: 'default' })
  assert.deepEqual(modesOf(1000, 1006), { tracking: 'normal', encoding: 'sgr' }, 'herdr\'s own choice')
  assert.deepEqual(modesOf(1000, 1002), { tracking: 'button', encoding: 'default' })
  assert.deepEqual(modesOf(1000, 1003, 1015), { tracking: 'any', encoding: 'urxvt' })
  assert.deepEqual(modesOf(9, 1005), { tracking: 'x10', encoding: 'utf8' })
})

test('SGR reports say the button on press and release, M then m', () => {
  assert.equal(mouseReport(sgr, { kind: 'press', button: 2 }, at), '\x1b[<2;12;5M', 'a right-click is button 2')
  assert.equal(mouseReport(sgr, { kind: 'release', button: 2 }, at), '\x1b[<2;12;5m')
  assert.equal(mouseReport(sgr, { kind: 'press', button: 0 }, { ...at, shift: true, alt: true, ctrl: true }), '\x1b[<28;12;5M', 'modifiers add 4, 8 and 16')
  assert.equal(mouseReport(sgr, { kind: 'wheel', direction: 'up' }, at), '\x1b[<64;12;5M')
  assert.equal(mouseReport(sgr, { kind: 'wheel', direction: 'down' }, at), '\x1b[<65;12;5M')
  assert.equal(mouseReport(sgr, { kind: 'press', button: 3 }, at), null, 'back and forward are not terminal buttons')
  assert.equal(mouseReport({ tracking: 'off', encoding: 'sgr' }, { kind: 'press', button: 0 }, at), null, 'nothing while the client does not track')
})

test('moves are reported only as the tracking asks', () => {
  const drag = { kind: 'move', button: 0 } as const
  const hover = { kind: 'move', button: null } as const
  assert.equal(mouseReport(sgr, drag, at), null, 'normal tracking hears no moves')
  assert.equal(mouseReport({ ...sgr, tracking: 'button' }, drag, at), '\x1b[<32;12;5M', 'a drag is 32 plus the button')
  assert.equal(mouseReport({ ...sgr, tracking: 'button' }, hover, at), null)
  assert.equal(mouseReport({ ...sgr, tracking: 'any' }, hover, at), '\x1b[<35;12;5M', 'a bare move is 35')
  assert.equal(wantsMove({ ...sgr, tracking: 'button' }, true), true)
  assert.equal(wantsMove({ ...sgr, tracking: 'button' }, false), false)
  assert.equal(wantsMove({ ...sgr, tracking: 'any' }, false), true)
})

test('the older encodings: raw bytes, UTF-8 and urxvt; a release is button 3; X10 reports presses only', () => {
  const raw: MouseModes = { tracking: 'normal', encoding: 'default' }
  assert.deepEqual(mouseReport(raw, { kind: 'press', button: 2 }, at), Uint8Array.of(0x1b, 0x5b, 0x4d, 34, 44, 37))
  assert.deepEqual(mouseReport(raw, { kind: 'release', button: 2 }, at), Uint8Array.of(0x1b, 0x5b, 0x4d, 35, 44, 37))
  assert.deepEqual(mouseReport(raw, { kind: 'press', button: 0 }, { ...at, col: 200 }), Uint8Array.of(0x1b, 0x5b, 0x4d, 32, 232, 37), 'a byte past 127 stays one byte')
  assert.equal(mouseReport(raw, { kind: 'press', button: 0 }, { ...at, col: 224 }), null, 'past column 223 the raw encoding cannot say it')
  assert.equal(mouseReport({ tracking: 'normal', encoding: 'utf8' }, { kind: 'press', button: 0 }, { ...at, col: 300 }), `\x1b[M ${String.fromCodePoint(332)}%`)
  assert.equal(mouseReport({ tracking: 'normal', encoding: 'urxvt' }, { kind: 'release', button: 0 }, at), '\x1b[35;12;5M')
  const x10: MouseModes = { tracking: 'x10', encoding: 'default' }
  assert.deepEqual(mouseReport(x10, { kind: 'press', button: 0 }, { ...at, ctrl: true }), Uint8Array.of(0x1b, 0x5b, 0x4d, 32, 44, 37), 'no modifiers in X10')
  assert.equal(mouseReport(x10, { kind: 'release', button: 0 }, at), null)
})

test('a point maps to its 1-based cell, clamped to the grid', () => {
  const box = { left: 10, top: 20, width: 800, height: 280 }
  const grid = { cols: 100, rows: 20 }
  assert.deepEqual(cellAt({ x: 10, y: 20 }, box, grid), { col: 1, row: 1 })
  assert.deepEqual(cellAt({ x: 17.9, y: 33.9 }, box, grid), { col: 1, row: 1 })
  assert.deepEqual(cellAt({ x: 18, y: 34 }, box, grid), { col: 2, row: 2 })
  assert.deepEqual(cellAt({ x: 5000, y: -40 }, box, grid), { col: 100, row: 1 })
})

test('⌥ and a printable key is ESC and the key\'s own character, whatever the layout makes of it', () => {
  const key = (code: string, more: Partial<KeyboardEvent> = {}) => metaKey({ code, altKey: true, ctrlKey: false, metaKey: false, shiftKey: false, ...more })
  assert.equal(key('KeyW'), '\x1bw', '⌥W is herdr\'s close-pane')
  assert.equal(key('KeyW', { shiftKey: true }), '\x1bW')
  assert.equal(key('Digit1'), '\x1b1')
  assert.equal(key('Digit1', { shiftKey: true }), '\x1b!')
  assert.equal(key('Slash', { shiftKey: true }), '\x1b?')
  assert.equal(key('Space'), '\x1b ')
  assert.equal(key('ArrowLeft'), null, 'keys with no character are the encoder\'s')
  assert.equal(key('KeyW', { ctrlKey: true }), null, 'another modifier is the encoder\'s')
  assert.equal(key('KeyW', { metaKey: true }), null)
  assert.equal(metaKey({ code: 'KeyW', altKey: false, ctrlKey: false, metaKey: false, shiftKey: false }), null)
})
