import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

import { cardClip, easeOutQuint, morphInFrames, morphInMs, morphOutFrames, morphOutMs, swipeFrames, swipeMs } from '../src/features/lens/lensMotionModel.ts'

const host = { top: 40, right: 1000, bottom: 800, left: 200 }
const card = { top: 100, right: 584, bottom: 220, left: 300 }

test('zoom in grows from the card bounds in 280 ms, radius 8 → 0, opacity 0.3 → 1; zoom out reverses in 200 ms', () => {
  assert.equal(easeOutQuint, 'cubic-bezier(0.22, 1, 0.36, 1)')
  assert.equal(morphInMs, 280)
  assert.equal(morphOutMs, 200)
  assert.equal(cardClip(host, card, 8), 'inset(60px 416px 580px 100px round 8px)')
  assert.equal(cardClip(host, { top: 0, right: 2000, bottom: 900, left: 0 }, 8), 'inset(0px 0px 0px 0px round 8px)', 'a card past the host clamps to it')
  assert.deepEqual(morphInFrames(host, card), [
    { clipPath: 'inset(60px 416px 580px 100px round 8px)', opacity: 0.3 },
    { clipPath: 'inset(0px 0px 0px 0px round 0px)', opacity: 1 },
  ])
  assert.deepEqual(morphOutFrames(host, card), [...morphInFrames(host, card)].reverse())
})

test('n, N and ⇧⌥←/→ swipe in from 30% to the side the step goes, with a fade, in 240 ms', () => {
  assert.equal(swipeMs, 240)
  assert.deepEqual(swipeFrames(1), [{ transform: 'translateX(30%)', opacity: 0 }, { transform: 'translateX(0)', opacity: 1 }])
  assert.deepEqual(swipeFrames(-1)[0], { transform: 'translateX(-30%)', opacity: 0 })
})

test('motion never runs under reduced motion and never holds the view or focus back', () => {
  const motion = readFileSync(new URL('../src/features/lens/lensMotion.ts', import.meta.url), 'utf8')
  assert.match(motion, /matchMedia\('\(prefers-reduced-motion: reduce\)'\)/)
  for (const name of ['morphIn', 'morphOut', 'swipe']) assert.match(motion, new RegExp(`export function ${name}\\([^)]*\\) \\{\\n  const target = host\\(\\)\\n  if \\([^)]*reduced\\(\\)\\) return`), `${name} bails under reduced motion`)
  assert.doesNotMatch(motion, /\.finished|await /, 'nothing waits on an animation')
  const controller = readFileSync(new URL('../src/features/workspace/useWorkspaceController.ts', import.meta.url), 'utf8')
  // The view and the arrival come first; the motion only draws over them.
  assert.match(controller, /if \(focus\) arrive\(\)\n {4}if \(fromLens\) morphIn\(card\)\n {4}else if \(moving\) swipe\(by\)/)
  assert.match(controller, /setView\(\{ kind: 'lens' \}\)[\s\S]{0,300}\.lens'\)\?\.focus\(\{ preventScroll: true \}\)\)\n {4}if \(current\.kind === 'zoom' && activeSpaceIDRef\.current\) morphOut\(activeSpaceIDRef\.current\)/)
  const styles = readFileSync(new URL('../src/styles.css', import.meta.url), 'utf8')
  assert.match(styles, /\.dock-host\[data-morph='out'\] > :not\(\.lens, \.alone-agent\) \{[^}]*pointer-events: none;/, 'the shrinking dock never takes a pointer')
})
