import assert from 'node:assert/strict'
import test from 'node:test'
import {
  autoPinPreview,
  createTabState,
  pinAgent,
  previewAgent,
  storedPinnedAgents,
} from '../src/previewTabs.ts'

test('single-click previews one fixture agent and replaces it with the next', () => {
  const first = previewAgent(createTabState(['mavu'], 'agent:mavu'), 'zira')
  assert.deepEqual(first, {
    tabs: [{ name: 'mavu', preview: false }, { name: 'zira', preview: true }],
    activeTab: 'agent:zira',
  })

  const replaced = previewAgent(first, 'vile')
  assert.deepEqual(replaced, {
    tabs: [{ name: 'mavu', preview: false }, { name: 'vile', preview: true }],
    activeTab: 'agent:vile',
  })
})

test('double-click pins the fixture preview and frees the preview slot', () => {
  const previewed = previewAgent(createTabState([], 'board'), 'mavu')
  const pinned = pinAgent(previewed, 'mavu')
  const next = previewAgent(pinned, 'zira')

  assert.deepEqual(next.tabs, [
    { name: 'mavu', preview: false },
    { name: 'zira', preview: true },
  ])
})

test('sending from a fixture preview auto-pins it', () => {
  const previewed = previewAgent(createTabState([], 'board'), 'vile')
  assert.deepEqual(autoPinPreview(previewed, 'vile'), {
    tabs: [{ name: 'vile', preview: false }],
    activeTab: 'agent:vile',
  })
})

test('single-click focuses an existing pinned fixture agent without duplicating it', () => {
  const state = previewAgent(createTabState(['mavu', 'zira'], 'agent:mavu'), 'zira')
  assert.deepEqual(state, {
    tabs: [{ name: 'mavu', preview: false }, { name: 'zira', preview: false }],
    activeTab: 'agent:zira',
  })
})

test('only pinned fixture agents are selected for layout persistence', () => {
  const state = previewAgent(createTabState(['mavu'], 'agent:mavu'), 'vile')
  assert.deepEqual(storedPinnedAgents(state), ['mavu'])
})
