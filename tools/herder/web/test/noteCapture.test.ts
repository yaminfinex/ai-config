import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import {
  capturePosition,
  captureSourceWithRange,
  isRangeSelection,
  isReservedFileResolutionSelection,
  placeCaretAtEnd,
  reserveSelectionForFileResolution,
  sharedCaptureSurface,
} from '../src/features/notes/noteCaptureModel.ts'
import { fileResolveGestureEvent } from '../src/shared/selectionPopoverEvents.ts'

test('capture membership requires the same nearest allowlisted content surface', () => {
  const transcriptOne = {}
  const transcriptTwo = {}
  const surfaces = new Map<unknown, unknown>([['one', transcriptOne], ['two', transcriptTwo], ['chrome', null]])
  const resolve = (node: unknown) => surfaces.get(node) ?? null
  assert.equal(sharedCaptureSurface('one', 'one', resolve), transcriptOne)
  assert.equal(sharedCaptureSurface('one', 'two', resolve), null)
  assert.equal(sharedCaptureSurface('one', 'chrome', resolve), null)
})

test('capture position stays inside the viewport', () => {
  assert.deepEqual(capturePosition({ left: -20, bottom: 900 }, 800, 600), { left: 8, top: 272 })
  assert.deepEqual(capturePosition({ left: 120, bottom: 80 }, 800, 600), { left: 120, top: 86 })
  assert.deepEqual(capturePosition({ left: 790, bottom: 80 }, 800, 600), { left: 460, top: 86 })
})

test('line facts are added only when both ends are proved', () => {
  assert.deepEqual(captureSourceWithRange({ kind: 'file', path: 'src/App.tsx' }, 7, 9), { kind: 'file', path: 'src/App.tsx', start: 7, end: 9 })
  assert.deepEqual(captureSourceWithRange({ kind: 'diff', path: 'src/App.tsx', base: 'merge-base' }, 7), { kind: 'diff', path: 'src/App.tsx', base: 'merge-base' })
})

test('capture hook preserves prior shortcut claims and does not attribute an empty end line', () => {
  const hook = readFileSync(new URL('../src/features/notes/useNoteCapture.tsx', import.meta.url), 'utf8')
  assert.match(hook, /range\.endOffset === 0 \? undefined/)
  assert.doesNotMatch(hook, /noteCaptureShortcutEvent|captureGroup|setCaptureGroup/)
})

test('keyboard range selection waits for Shift release before focusing the chip', () => {
  const hook = readFileSync(new URL('../src/features/notes/useNoteCapture.tsx', import.meta.url), 'utf8')
  assert.match(hook, /keyboardSelecting/)
  assert.match(hook, /event\.shiftKey && \(event\.key === 'ArrowLeft' \|\| event\.key === 'ArrowRight'/)
  assert.match(hook, /event\.key === 'Shift'/)
  assert.match(hook, /if \(!active \|\| pointer\.current \|\| keyboardSelecting\.current\) return/)
})

test('drag selection stays passive and leaves native double-click routing untouched', () => {
  const hook = readFileSync(new URL('../src/features/notes/useNoteCapture.tsx', import.meta.url), 'utf8')
  const agent = readFileSync(new URL('../src/features/transcript/AgentPanel.tsx', import.meta.url), 'utf8')
  const file = readFileSync(new URL('../src/features/files/FilePanel.tsx', import.meta.url), 'utf8')
  assert.doesNotMatch(hook, /setPointerCapture|releasePointerCapture/)
  assert.match(hook, /useDOMEvent<PointerEvent>\(window, 'pointerdown'/)
  assert.match(hook, /useDOMEvent<PointerEvent>\(window, 'pointerup'/)
  assert.match(hook, /event\.composedPath\(\)/)
  assert.match(hook, /getSelection\?\.\(\)/)
  assert.doesNotMatch(`${agent}\n${file}`, /onPointerDown=\{noteCapture/)
  assert.match(agent, /onDoubleClickCapture=\{noteCapture\.onDoubleClick\}/)
  assert.match(file, /onDoubleClickCapture=\{noteCapture\.onDoubleClick\}/)
})

test('synthetic transcript double-click dispatches file resolution and cannot open the chip', () => {
  const node = {}
  const selection = { isCollapsed: false, anchorNode: node, anchorOffset: 0, focusNode: node, focusOffset: 8 }
  const events = new EventTarget()
  let fileResolutions = 0
  let chipOpens = 0
  events.addEventListener(fileResolveGestureEvent, () => { fileResolutions += 1 })
  const reserved = reserveSelectionForFileResolution(selection, () => events.dispatchEvent(new Event(fileResolveGestureEvent)))
  if (isRangeSelection(selection) && !isReservedFileResolutionSelection(selection, reserved)) chipOpens += 1
  assert.equal(fileResolutions, 1)
  assert.equal(chipOpens, 0)
})

test('reselection preserves a comment and cannot capture the chip preview itself', () => {
  const hook = readFileSync(new URL('../src/features/notes/useNoteCapture.tsx', import.meta.url), 'utf8')
  assert.match(hook, /closest\('\.note-capture-popover'\)/)
  assert.doesNotMatch(hook, /<NoteCaptureChip key=/)
})

test('typing to expand places the caret after the seeded first character', () => {
  const calls: Array<[number, number]> = []
  const field = { value: 'h', focus: () => undefined, setSelectionRange: (start: number, end: number) => calls.push([start, end]) }
  placeCaretAtEnd(field)
  assert.deepEqual(calls, [[1, 1]])
})

test('the one capture chip keeps target talk out of its minimal state', () => {
  const chip = readFileSync(new URL('../src/features/notes/NoteCaptureChip.tsx', import.meta.url), 'utf8')
  assert.match(chip, />＋ Add note</)
  const minimal = chip.slice(chip.indexOf('if (!expanded)'), chip.indexOf('return <aside'))
  assert.doesNotMatch(minimal, /capture\.group|unassigned|<NotesSelector/)
  assert.match(chip, /\(event\.key === 'Enter' \|\| event\.key === ' '\)[\s\S]*expandWith\(\)[\s\S]*preventDefault/)
  assert.match(chip, /event\.key\.length === 1 && event\.key !== ' '/)
})

test('capture resolves composed range endpoints against explicit content markers', () => {
  const hook = readFileSync(new URL('../src/features/notes/useNoteCapture.tsx', import.meta.url), 'utf8')
  assert.match(hook, /getComposedRanges/)
  assert.match(hook, /data-note-capture-content/)
  assert.match(hook, /sharedCaptureSurface/)
  assert.doesNotMatch(hook, /anchorNode\).*focusNode/)
})
