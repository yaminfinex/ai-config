import assert from 'node:assert/strict'
import test from 'node:test'
import {
  applyRoute,
  autoPinPreview,
  createTabState,
  pinAgent,
  previewAgent,
  storedPinnedAgents,
} from '../src/previewTabs.ts'

test('mounting an agent URL absent from persisted layout opens a preview', () => {
  const mounted = applyRoute(createTabState([], 'board'), { page: 'agent', name: 'zira' })
  assert.deepEqual(mounted, {
    tabs: [{ name: 'zira', preview: true }],
    activeTab: 'agent:zira',
  })
})

test('popstate to an agent outside the open tabs uses the preview slot', () => {
  const current = previewAgent(createTabState(['mavu'], 'agent:mavu'), 'zira')
  const popped = applyRoute(current, { page: 'agent', name: 'vile' })
  assert.deepEqual(popped, {
    tabs: [{ name: 'mavu', preview: false }, { name: 'vile', preview: true }],
    activeTab: 'agent:vile',
  })
})

test('an agent URL already present in persisted layout stays pinned', () => {
  const mounted = applyRoute(createTabState(['mavu'], 'board'), { page: 'agent', name: 'mavu' })
  assert.deepEqual(mounted, {
    tabs: [{ name: 'mavu', preview: false }],
    activeTab: 'agent:mavu',
  })
})

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
