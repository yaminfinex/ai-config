import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { captureNoteText, capturePosition, captureSourceWithRange } from '../src/features/notes/noteCaptureModel.ts'

test('capture always keeps the quote and appends an optional comment', () => {
  assert.equal(captureNoteText('  selected text  ', ''), 'selected text')
  assert.equal(captureNoteText('selected text', '  investigate this  '), 'selected text\n\ninvestigate this')
})

test('capture position stays inside the viewport', () => {
  assert.deepEqual(capturePosition({ left: -20, bottom: 900 }, 800, 600), { left: 8, top: 272 })
  assert.deepEqual(capturePosition({ left: 120, bottom: 80 }, 800, 600), { left: 120, top: 86 })
})

test('line facts are added only when both ends are proved', () => {
  assert.deepEqual(captureSourceWithRange({ kind: 'file', path: 'src/App.tsx' }, 7, 9), { kind: 'file', path: 'src/App.tsx', start: 7, end: 9 })
  assert.deepEqual(captureSourceWithRange({ kind: 'diff', path: 'src/App.tsx', base: 'merge-base' }, 7), { kind: 'diff', path: 'src/App.tsx', base: 'merge-base' })
})

test('capture hook preserves prior shortcut claims and does not attribute an empty end line', () => {
  const hook = readFileSync(new URL('../src/features/notes/useNoteCapture.tsx', import.meta.url), 'utf8')
  assert.match(hook, /if \(show\(\)\).*detail\.claimed = true/)
  assert.match(hook, /range\.endOffset === 0 \? undefined/)
})
