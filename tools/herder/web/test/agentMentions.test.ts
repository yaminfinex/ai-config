import assert from 'node:assert/strict'
import test from 'node:test'

import { agentMentionMatcher } from '../src/shared/agentMentions.ts'
import type { Board } from '../src/types.ts'

const liveBoard: Board = {
  workspaces: [{
    workspace_id: 'workspace-1', number: 1, label: 'mentions', focused: true,
    pane_count: 2, tab_count: 1, active_tab_id: 'tab-1', agent_status: 'active',
    tabs: [{
      tab_id: 'tab-1', number: 1, label: 'agents', focused: true,
      pane_count: 2, agent_status: 'active',
      panes: [
        { pane_id: 'pane-1', agent: 'impl-kima', tool: 'codex', herdr_status: 'active', bus_status: 'active', gap: '-' },
        {
          pane_id: 'pane-2', agent: 'review-zani', tool: 'claude', herdr_status: 'active', bus_status: 'listening', gap: '-',
          subagents: [{ pane_id: '-', agent: 'probe-fame', tool: 'claude', herdr_status: '-', bus_status: 'active', gap: 'no visible pane', parent_agent: 'review-zani' }],
        },
      ],
    }],
  }],
  unplaced: [{ pane_id: '-', agent: 'luge', tool: 'codex', herdr_status: '-', bus_status: 'blocked', gap: 'no visible pane' }],
}

function mentions(text: string, board: Board = liveBoard) {
  return agentMentionMatcher(board).tokenize(text).flatMap((token) => typeof token === 'string' ? [] : [token])
}

test('agent mentions resolve canonical, derived base, tagged, and @ forms from a real Board shape', () => {
  assert.deepEqual(mentions('kima @kima impl-kima @impl-kima; zani, @review-zani and @luge.'), [
    { text: 'kima', name: 'impl-kima' },
    { text: '@kima', name: 'impl-kima' },
    { text: 'impl-kima', name: 'impl-kima' },
    { text: '@impl-kima', name: 'impl-kima' },
    { text: 'zani', name: 'review-zani' },
    { text: '@review-zani', name: 'review-zani' },
    { text: '@luge', name: 'luge' },
  ])
  assert.deepEqual(mentions('nested fame and @probe-fame'), [
    { text: 'fame', name: 'probe-fame' },
    { text: '@probe-fame', name: 'probe-fame' },
  ])
})

test('agent mentions reject lookalikes, substrings, email fragments, retired names, and ambiguous bases', () => {
  assert.deepEqual(mentions('kimono xkima kima_suffix pre-impl-kima-post mail@kima.dev ékima kimaé retired-nelo nelo'), [])
  const ambiguous: Board = {
    ...liveBoard,
    unplaced: [
      ...liveBoard.unplaced,
      { pane_id: '-', agent: 'other-kima', tool: 'claude', herdr_status: '-', bus_status: 'active', gap: 'no visible pane' },
    ],
  }
  assert.deepEqual(mentions('kima impl-kima other-kima', ambiguous), [
    { text: 'impl-kima', name: 'impl-kima' },
    { text: 'other-kima', name: 'other-kima' },
  ])
})

test('canonical names outrank colliding derived aliases and paths stay intact', () => {
  const collision: Board = {
    ...liveBoard,
    unplaced: [
      ...liveBoard.unplaced,
      { pane_id: '-', agent: 'kima', tool: 'claude', herdr_status: '-', bus_status: 'active', gap: 'no visible pane' },
    ],
  }
  assert.deepEqual(mentions('kima impl-kima', collision), [
    { text: 'kima', name: 'kima' },
    { text: 'impl-kima', name: 'impl-kima' },
  ])
  assert.deepEqual(mentions('src/kima.ts /worktrees/impl-kima/App.tsx'), [])
  assert.deepEqual(agentMentionMatcher(liveBoard).tokenize('src/kima.ts /worktrees/impl-kima/App.tsx'), ['src/kima.ts /worktrees/impl-kima/App.tsx'])
})

test('a retired agent stops resolving when it leaves the live Board', () => {
  const withNelo: Board = {
    ...liveBoard,
    unplaced: [...liveBoard.unplaced, { pane_id: '-', agent: 'review-nelo', tool: 'codex', herdr_status: '-', bus_status: 'listening', gap: 'no visible pane' }],
  }
  assert.equal(mentions('@nelo', withNelo)[0]?.name, 'review-nelo')
  assert.deepEqual(mentions('@nelo', liveBoard), [])
})

test('an unchanged roster version reuses its matcher and per-message token cache across Board snapshots', () => {
  const first = agentMentionMatcher(liveBoard)
  const second = agentMentionMatcher(structuredClone(liveBoard))
  assert.equal(second, first)
  assert.equal(second.tokenize('ask @kima'), first.tokenize('ask @kima'))
})
