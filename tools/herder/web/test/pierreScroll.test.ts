import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { isLineCentered, LINE_CENTER_TOLERANCE_PX, MAX_LINE_SCROLL_ATTEMPTS, planLineScroll, USER_SCROLL_EVENTS, userScrolled } from '../src/features/git/pierreScroll.ts'

test('a deep selected line is not settled until its post-highlight rect is centered', () => {
  const scrollableFile = Array.from({ length: 300 }, (_, index) => `line ${index + 1}`)
  assert.equal(scrollableFile[199], 'line 200')

  const container = { top: 97.5, bottom: 876 }
  const beforeHighlightSettles = { top: 4093.5, bottom: 4113.5 }
  const centeredAfterHighlight = { top: 476.75, bottom: 496.75 }

  assert.equal(isLineCentered(beforeHighlightSettles, container), false)
  assert.equal(isLineCentered(centeredAfterHighlight, container), true)
  assert.equal(LINE_CENTER_TOLERANCE_PX, 24)
  assert.equal(MAX_LINE_SCROLL_ATTEMPTS, 8)
})

const selection = { path: 'a.ts', content: 'x', line: 200 }

test('an off-centre line keeps re-centring after each render until it is centered, then stops', () => {
  const first = planLineScroll(undefined, selection, 'off-centre')
  assert.equal(first.scroll, true)
  assert.equal(first.next.done, false)
  const highlightMovedIt = planLineScroll(first.next, selection, 'off-centre')
  assert.equal(highlightMovedIt.scroll, true)
  assert.equal(highlightMovedIt.next.attempts, 2)
  const settled = planLineScroll(highlightMovedIt.next, selection, 'centered')
  assert.equal(settled.scroll, false)
  assert.equal(settled.next.done, true)
  assert.equal(planLineScroll(settled.next, selection, 'off-centre').scroll, false)
})

test('an already-centered line is done, so scrolling away is never pulled back', () => {
  const centered = planLineScroll(undefined, selection, 'centered')
  assert.equal(centered.scroll, false)
  assert.equal(centered.next.done, true)
  const scrolledAway = planLineScroll(centered.next, selection, 'off-centre')
  assert.equal(scrolledAway.scroll, false)
  assert.equal(scrolledAway.next, centered.next)
})

test('a user scroll during the settle window stops re-centring', () => {
  const settling = planLineScroll(undefined, selection, 'off-centre').next
  const taken = userScrolled(settling)
  assert.equal(taken?.done, true)
  assert.equal(planLineScroll(taken, selection, 'off-centre').scroll, false)
  assert.equal(userScrolled(undefined), undefined)
  assert.deepEqual([...USER_SCROLL_EVENTS], ['wheel', 'touchstart', 'pointerdown', 'keydown'])
})

test('a missing line retries up to the cap, then gives up', () => {
  let state = planLineScroll(undefined, selection, 'missing').next
  for (let attempt = 1; attempt < MAX_LINE_SCROLL_ATTEMPTS; attempt += 1) state = planLineScroll(state, selection, 'missing').next
  assert.equal(state.attempts, MAX_LINE_SCROLL_ATTEMPTS)
  assert.equal(state.done, false)
  assert.equal(planLineScroll(state, selection, 'off-centre').scroll, false)
  const lateButInTime = planLineScroll({ ...state, attempts: MAX_LINE_SCROLL_ATTEMPTS - 1 }, selection, 'off-centre')
  assert.equal(lateButInTime.scroll, true)
  assert.equal(lateButInTime.next.attempts, MAX_LINE_SCROLL_ATTEMPTS)
})

test('a new path, content or start line scrolls again', () => {
  const done = planLineScroll(undefined, selection, 'centered').next
  for (const next of [{ ...selection, path: 'b.ts' }, { ...selection, content: 'y' }, { ...selection, line: 12 }]) {
    const plan = planLineScroll(done, next, 'off-centre')
    assert.equal(plan.scroll, true)
    assert.deepEqual(plan.next, { ...next, attempts: 1, done: false })
  }
})

test('PierreView only reads the DOM and defers the scroll decision to planLineScroll', () => {
  const source = readFileSync(new URL('../src/features/git/PierreView.tsx', import.meta.url), 'utf8')
  assert.match(source, /\(node\.shadowRoot \?\? node\)/)
  assert.match(source, /isLineCentered\(line\.getBoundingClientRect\(\), container\.getBoundingClientRect\(\)\)/)
  assert.match(source, /planLineScroll\(scrollState\.current,/)
  assert.match(source, /if \(plan\.scroll\) line\?\.scrollIntoView/)
  assert.doesNotMatch(source, /attempts/)
  assert.match(source, /scrollState\.current = userScrolled\(scrollState\.current\)/)
  assert.match(source, /addEventListener\(type, settle, \{ passive: true \}\)/)
  assert.match(source, /userScrollTarget\.current\?\.dispose\(\)\n {2}\}, \[\]\)/)
})
