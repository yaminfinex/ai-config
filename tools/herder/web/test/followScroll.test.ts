import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

import { isAtScrollBottom } from '../src/shared/followScroll.ts'

test('follow remains active only within the bottom threshold', () => {
  assert.equal(isAtScrollBottom({ scrollHeight: 1_000, scrollTop: 553, clientHeight: 400 }), true)
  assert.equal(isAtScrollBottom({ scrollHeight: 1_000, scrollTop: 552, clientHeight: 400 }), false)
})

test('transcript and screen use the conventional floating jump-to-bottom control', () => {
  const agentPanel = readFileSync(new URL('../src/features/transcript/AgentPanel.tsx', import.meta.url), 'utf8')
  const screenPanel = readFileSync(new URL('../src/features/screen/ScreenPanel.tsx', import.meta.url), 'utf8')
  const css = readFileSync(new URL('../src/styles.css', import.meta.url), 'utf8')

  assert.doesNotMatch(agentPanel, /follow-chip/)
  assert.match(agentPanel, /<JumpToBottomButton/)
  assert.match(screenPanel, /<JumpToBottomButton/)
  assert.match(css, /\.jump-to-bottom \{[^}]*position: absolute;[^}]*right:/s)
})
