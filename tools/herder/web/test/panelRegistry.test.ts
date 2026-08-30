import assert from 'node:assert/strict'
import test from 'node:test'
import {
  panelID,
  panelParams,
  panelPresentation,
  previewPanelToReplace,
} from '../src/features/workspace/panelRegistryModel.ts'
import { parseStoredLayout } from '../src/features/layout/dockLayout.ts'

const panes = [{ pane_id: 'pane-1', agent: 'mavu', tool: 'codex', herdr_status: 'active', bus_status: 'active', gap: '' }]
const cases = [
  [{ kind: 'agent', name: 'mavu', preview: false }, 'agent:mavu', { title: 'mavu', icon: '', meta: 'unknown' }],
  [{ kind: 'screen', pane: panes[0], identity: { paneID: 'pane-1', workspaceID: 'workspace-1', tabID: 'tab-1', agent: 'mavu' }, preview: true }, 'screen:pane-1', { title: 'pane-1', icon: '▣ ', meta: 'terminal' }],
  [{ kind: 'file', root: '/repo', path: 'README.md', preview: true, viewMode: 'rendered' }, 'file:%2Frepo:README.md', { title: 'README.md', icon: '◇ ', meta: 'file · read-only' }],
  [{ kind: 'folder', root: '/repo', path: 'src', preview: true }, 'folder:%2Frepo:src', { title: 'src', icon: '▰ ', meta: 'folder · read-only' }],
  [{ kind: 'changes', root: '/repo', preview: true }, 'changes:%2Frepo', { title: 'Changes · repo', icon: '± ', meta: 'git · read-only' }],
] as const

test('the registry validates, identifies, and presents every panel kind', () => {
  for (const [input, id, presentation] of cases) {
    const params = panelParams(input)
    assert.ok(params)
    assert.equal(panelID(params), id)
    assert.deepEqual(panelPresentation(params), presentation)
  }
})

test('registry validation rejects malformed and unknown panel params', () => {
  assert.equal(panelParams({ kind: 'agent', name: '', preview: false }), null)
  assert.equal(panelParams({ kind: 'screen', pane: panes[0], identity: {}, preview: false }), null)
  assert.equal(panelParams({ kind: 'file', root: '/repo', path: 'README.md', line: 0, preview: true, viewMode: 'source' }), null)
  assert.equal(panelParams({ kind: 'folder', root: '', path: 'src', preview: true }), null)
  assert.equal(panelParams({ kind: 'changes', root: '', preview: true }), null)
  assert.equal(panelParams({ kind: 'notes', preview: true }), null)
})

test('persistence rejects panels whose registry identity or component does not match', () => {
  const stored = (id: string, contentComponent = 'agent') => JSON.stringify({
    version: 2,
    sidebarWidth: 250,
    dock: {
      grid: { root: { type: 'branch', data: [{ type: 'leaf', data: { id: 'group-main', views: [id], activeView: id } }] } },
      panels: { [id]: { id, contentComponent, params: { kind: 'agent', name: 'mavu', preview: false } } },
      activeGroup: 'group-main',
    },
  })
  assert.equal(parseStoredLayout(stored('agent:other'))?.dock, null)
  assert.equal(parseStoredLayout(stored('agent:mavu', 'file'))?.dock, null)
  assert.ok(parseStoredLayout(stored('agent:mavu'))?.dock)
})

test('a new preview replaces only the target group preview of its own kind', () => {
  const panels = [
    { id: 'agent:mavu', params: { kind: 'agent', name: 'mavu', preview: true } },
    { id: 'file:one', params: { kind: 'file', root: '/repo', path: 'one', preview: false, viewMode: 'source' } },
    { id: 'file:two', params: { kind: 'file', root: '/repo', path: 'two', preview: true, viewMode: 'source' } },
  ]
  assert.equal(previewPanelToReplace(panels, 'file')?.id, 'file:two')
  assert.equal(previewPanelToReplace(panels, 'folder'), undefined)
})
