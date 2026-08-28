import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { agentContextPresentation, hasRightOverflow, middleEllipsis, repoNameFromRemote } from '../src/shared/agentContext.ts'
import type { AgentDetail } from '../src/types.ts'

function detail(overrides: Partial<AgentDetail> = {}): AgentDetail {
  return {
    name: 'probe-dore',
    tool: 'codex',
    herdr_status: 'active',
    bus_status: 'listening',
    gap: '-',
    pane: { workspace_id: 'w1', tab_id: 't1', pane_id: 'p1' },
    launch_context: {},
    ...overrides,
  }
}

test('middle ellipsis preserves both ends and never changes a short cwd', () => {
  assert.equal(middleEllipsis('/short/repo'), '/short/repo')
  const shortened = middleEllipsis('/home/operator/very/long/worktree/path/to/the/repository', 30)
  assert.equal(shortened.length, 30)
  assert.match(shortened, /^\/home\/oper.*….*\/repository$/)
})

test('repo names come only from recognizable remote URL paths', () => {
  assert.equal(repoNameFromRemote('https://github.com/example/ai-config.git'), 'ai-config')
  assert.equal(repoNameFromRemote('git@github.com:example/fleet.git'), 'fleet')
  assert.equal(repoNameFromRemote('ssh://git@example.com/example/with-space.git'), 'with-space')
  assert.equal(repoNameFromRemote('just-a-label'), undefined)
  assert.equal(repoNameFromRemote(undefined), undefined)
})

test('context presentation relocates every former header fact', () => {
  const presentation = agentContextPresentation(detail({
    cwd: '/home/operator/Coding/ai-config',
    git: { branch: 'agent-context-strip', remote_url: 'git@github.com:example/ai-config.git' },
    gap: 'no visible pane',
    pane: null,
    model: 'gpt-5-codex',
    context_usage: { used_tokens: 100, input_tokens: 100, window_tokens: 1000, used_percent: 10 },
  }), 'active')

  assert.equal(presentation.status, 'active')
  assert.deepEqual(presentation.cwd, { display: '/home/operator/Coding/ai-config', full: '/home/operator/Coding/ai-config' })
  assert.deepEqual(presentation.repository, { display: 'ai-config · agent-context-strip', remote: 'git@github.com:example/ai-config.git' })
  assert.deepEqual(presentation.details, ['unplaced', 'herdr active', 'no pane', 'codex'])
  assert.deepEqual(presentation.vitals, ['gpt-5-codex', '100 tokens · 90% left'])
})

test('retired and absent facts remain honest', () => {
  const presentation = agentContextPresentation(detail({
    bus_status: 'retired',
    herdr_status: '-',
    gap: 'no visible pane',
    pane: null,
    cwd: undefined,
    git: { branch: 'retained-branch' },
  }), '-')

  assert.equal(presentation.status, 'retired')
  assert.equal(presentation.cwd, undefined)
  assert.deepEqual(presentation.repository, { display: 'retained-branch', remote: undefined })
  assert.deepEqual(presentation.details, ['read-only', 'codex'])
  assert.deepEqual(presentation.vitals, [])
})

test('the reserved strip sits between the queued dock and composer while the header stays minimal', () => {
  const panel = readFileSync(new URL('../src/features/transcript/AgentPanel.tsx', import.meta.url), 'utf8')
  const header = panel.slice(panel.indexOf('<header className="agent-header">'), panel.indexOf('</header>'))
  assert.doesNotMatch(header, /pane-chip|agent-status|gap-badge|tool-chip|agent-vitals/)
  assert.ok(panel.indexOf('<AgentContextStrip') > panel.indexOf('className="queued-dock"'))
  assert.ok(panel.indexOf('<AgentContextStrip') < panel.indexOf('<Composer name='))

  const css = readFileSync(new URL('../src/styles.css', import.meta.url), 'utf8')
  assert.match(css, /\.agent-context-strip \{[^}]*flex: 0 0 32px;/)
})

test('narrow panes show the fade only while facts remain offscreen to the right', () => {
  assert.equal(hasRightOverflow(500, 240, 0), true)
  assert.equal(hasRightOverflow(500, 240, 260), false)
  assert.equal(hasRightOverflow(240, 240, 0), false)
})
