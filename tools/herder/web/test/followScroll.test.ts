import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

import {
  createFollowScrollState,
  isAtScrollBottom,
  recordFollowScroll,
  resizeFollowScroll,
  restoreFollowScroll,
} from '../src/shared/followScroll.ts'

test('follow remains active only within the bottom threshold', () => {
  assert.equal(isAtScrollBottom({ scrollHeight: 1_000, scrollTop: 553, clientHeight: 400 }), true)
  assert.equal(isAtScrollBottom({ scrollHeight: 1_000, scrollTop: 552, clientHeight: 400 }), false)
})

test('a detached viewport restores its saved position or resumes following on reattach', () => {
  const viewport = { scrollHeight: 1_000, scrollTop: 320, clientHeight: 400 }
  const state = createFollowScrollState()
  recordFollowScroll(state, viewport)
  assert.equal(state.following, false)

  viewport.scrollTop = 0 // Dockview detach/reattach browser reset.
  restoreFollowScroll(state, viewport)
  assert.equal(viewport.scrollTop, 320)

  viewport.scrollTop = 590
  recordFollowScroll(state, viewport)
  assert.equal(state.following, true)
  viewport.scrollTop = 0
  restoreFollowScroll(state, viewport)
  assert.equal(viewport.scrollTop, viewport.scrollHeight)
})

test('viewport resize re-pins only while following', () => {
  const viewport = { scrollHeight: 1_200, scrollTop: 600, clientHeight: 400 }
  const following = { following: true, scrollTop: 600 }
  resizeFollowScroll(following, viewport)
  assert.equal(viewport.scrollTop, 1_200)

  const reading = { following: false, scrollTop: 275 }
  viewport.scrollTop = 275
  resizeFollowScroll(reading, viewport)
  assert.equal(viewport.scrollTop, 275)
})

test('transcript and screen use centered top and bottom jump controls', () => {
  const agentPanel = readFileSync(new URL('../src/features/transcript/AgentPanel.tsx', import.meta.url), 'utf8')
  const screenPanel = readFileSync(new URL('../src/features/screen/ScreenPanel.tsx', import.meta.url), 'utf8')
  const css = readFileSync(new URL('../src/styles.css', import.meta.url), 'utf8')

  assert.doesNotMatch(agentPanel, /follow-chip/)
  assert.match(agentPanel, /<ScrollJumpButtons/)
  assert.match(screenPanel, /<ScrollJumpButtons/)
  assert.match(css, /\.scroll-jump-buttons \{[^}]*position: absolute;[^}]*left: 50%;[^}]*translateX\(-50%\)/s)
})
