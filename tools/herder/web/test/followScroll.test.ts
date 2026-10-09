import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

import {
  createFollowScrollState,
  forgetFollowScroll,
  isAtScrollBottom,
  recordFollowScroll,
  rememberedFollowScroll,
  resizeFollowScroll,
  restoredFollowScroll,
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

test('a remounted transcript takes up its remembered position until the panel closes', () => {
  const held = rememberedFollowScroll('agent:fixture-held')
  assert.deepEqual(held, createFollowScrollState())
  recordFollowScroll(held, { scrollHeight: 1_000, scrollTop: 320, clientHeight: 400 })
  // A space switch remounts the panel: the same state comes back.
  assert.equal(rememberedFollowScroll('agent:fixture-held'), held)
  assert.deepEqual(rememberedFollowScroll('agent:fixture-other'), createFollowScrollState())

  forgetFollowScroll('agent:fixture-held')
  assert.deepEqual(rememberedFollowScroll('agent:fixture-held'), createFollowScrollState())
})

test('remembered positions are bounded, least recently used first', () => {
  const first = rememberedFollowScroll('agent:fixture-bound-0')
  first.following = false
  for (let index = 1; index <= 200; index++) rememberedFollowScroll(`agent:fixture-bound-${index}`)
  assert.notEqual(rememberedFollowScroll('agent:fixture-bound-0'), first)
  assert.equal(rememberedFollowScroll('agent:fixture-bound-200').following, true)
})

test('a held position restores only once the transcript can scroll to it', () => {
  const state = { following: false, scrollTop: 320 }
  const empty = { scrollHeight: 400, scrollTop: 0, clientHeight: 400 }
  assert.equal(restoredFollowScroll(state, empty), false)
  const loaded = { ...empty, scrollHeight: 1_000 }
  assert.equal(restoredFollowScroll(state, loaded), true)
  assert.equal(loaded.scrollTop, 320)
})

test('a transcript remembers its scroll by panel and a close forgets it', () => {
  const agentPanel = readFileSync(new URL('../src/features/transcript/AgentPanel.tsx', import.meta.url), 'utf8')
  const registry = readFileSync(new URL('../src/features/workspace/panelRegistry.tsx', import.meta.url), 'utf8')
  const controller = readFileSync(new URL('../src/features/workspace/useWorkspaceController.ts', import.meta.url), 'utf8')
  assert.match(agentPanel, /useFollowScroll<HTMLElement>\(entries, viewMode, active && !screenMode, panelID\)/)
  assert.match(registry, /<AgentPanel name=\{name\} panelID=\{api\.id\}/)
  assert.match(controller, /if \(!historySuppressor\.active\(\)\) forgetFollowScroll\(panel\.id\)/)
})

test('transcript and screen use the centered jump-to-bottom control (top is shortcut-only)', () => {
  const agentPanel = readFileSync(new URL('../src/features/transcript/AgentPanel.tsx', import.meta.url), 'utf8')
  const screenPanel = readFileSync(new URL('../src/features/screen/ScreenPanel.tsx', import.meta.url), 'utf8')
  const css = readFileSync(new URL('../src/styles.css', import.meta.url), 'utf8')

  const jumpControls = readFileSync(new URL('../src/shared/useFollowScroll.tsx', import.meta.url), 'utf8')
  assert.doesNotMatch(agentPanel, /follow-chip/)
  assert.match(agentPanel, /<ScrollJumpButtons/)
  assert.match(screenPanel, /<ScrollJumpButtons/)
  assert.doesNotMatch(jumpControls, /Go to top/)
  assert.match(css, /\.scroll-jump-buttons \{[^}]*position: absolute;[^}]*left: 50%;[^}]*translateX\(-50%\)/s)
})
