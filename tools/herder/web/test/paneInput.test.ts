import assert from 'node:assert/strict'
import test from 'node:test'
import { encodeTerminalInput, PaneInputQueue, type PaneInput } from '../src/features/screen/paneInput.ts'

test('only proved exact key chunks become named keys', () => {
  assert.deepEqual(encodeTerminalInput('\r'), { keys: ['enter'] })
  assert.deepEqual(encodeTerminalInput('\x7f'), { keys: ['backspace'] })
  assert.deepEqual(encodeTerminalInput('\t'), { keys: ['tab'] })
  assert.deepEqual(encodeTerminalInput('\x03'), { keys: ['ctrl+c'] })
  assert.deepEqual(encodeTerminalInput('\x04'), { keys: ['ctrl+d'] })
  assert.deepEqual(encodeTerminalInput('\x1b'), { keys: ['escape'] })
  assert.deepEqual(encodeTerminalInput('\x1b[A'), { keys: ['up'] })
  assert.deepEqual(encodeTerminalInput('\x1b[B'), { keys: ['down'] })
  assert.deepEqual(encodeTerminalInput('\x1b[C'), { keys: ['right'] })
  assert.deepEqual(encodeTerminalInput('\x1b[D'), { keys: ['left'] })
  assert.deepEqual(encodeTerminalInput('\x1bb'), { keys: ['alt+b'] })
  assert.deepEqual(encodeTerminalInput('\x1bf'), { keys: ['alt+f'] })
})

test('unsupported substrate keys remain unchanged instead of being emulated', () => {
  assert.deepEqual(encodeTerminalInput('\x1b[H'), { text: '\x1b[H' })
  assert.deepEqual(encodeTerminalInput('\x1b[F'), { text: '\x1b[F' })
  assert.deepEqual(encodeTerminalInput('\x1b[3~'), { text: '\x1b[3~' })
})

test('content chunks stay one raw text request even when they contain carriage returns', () => {
  assert.deepEqual(encodeTerminalInput('first\rsecond'), { text: 'first\rsecond' })
  assert.deepEqual(encodeTerminalInput('\x1b[200~first\rsecond\x1b[201~'), { text: '\x1b[200~first\rsecond\x1b[201~' })
})

test('pane input sends one request at a time in exact enqueue order', async () => {
  const calls: PaneInput[] = []
  const releases: Array<() => void> = []
  const queue = new PaneInputQueue(async (input) => {
    calls.push(input)
    await new Promise<void>((resolve) => releases.push(resolve))
  })
  const first = queue.send('a')
  const second = queue.send('\x03')
  const third = queue.send('\x1b[200~paste\x1b[201~')
  await Promise.resolve()
  assert.deepEqual(calls, [{ text: 'a' }])
  releases.shift()?.(); await first; await Promise.resolve()
  assert.deepEqual(calls, [{ text: 'a' }, { keys: ['ctrl+c'] }])
  releases.shift()?.(); await second; await Promise.resolve()
  assert.deepEqual(calls, [{ text: 'a' }, { keys: ['ctrl+c'] }, { text: '\x1b[200~paste\x1b[201~' }])
  releases.shift()?.(); await third
})

test('one refused write does not poison later terminal input', async () => {
  const calls: PaneInput[] = []
  const queue = new PaneInputQueue(async (input) => {
    calls.push(input)
    if ('text' in input && input.text === 'bad') throw new Error('pane moved')
  })
  await assert.rejects(queue.send('bad'), /pane moved/)
  await queue.send('good')
  assert.deepEqual(calls, [{ text: 'bad' }, { text: 'good' }])
})
