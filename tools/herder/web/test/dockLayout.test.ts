import assert from 'node:assert/strict'
import test from 'node:test'
import {
  parseLegacyLayout,
  parseStoredLayout,
  persistableDockLayout,
  restoreDockLayout,
  screenIdentityState,
  type ScreenPanelParams,
} from '../src/features/layout/dockLayout.ts'
import type { Board } from '../src/types.ts'

const board: Board = {
  workspaces: [{
    workspace_id: 'workspace-1', number: 1, label: 'fixture', focused: true,
    pane_count: 1, tab_count: 1, active_tab_id: 'tab-1', agent_status: 'active',
    tabs: [{
      tab_id: 'tab-1', number: 1, label: 'agents', focused: true,
      pane_count: 1, agent_status: 'active',
      panes: [{ pane_id: 'pane-1', agent: 'mavu', tool: 'codex', herdr_status: 'active', bus_status: 'active', gap: '', agent_session: 'session-1' }],
    }],
  }],
  unplaced: [],
}

const singleGroupDock = {
  grid: { root: { type: 'branch', data: [{
    type: 'leaf', data: { id: 'group-main', views: ['agent:mavu', 'file:%2Frepo:README.md'], activeView: 'file:%2Frepo:README.md' },
  }] } },
  panels: {
    'agent:mavu': { id: 'agent:mavu', contentComponent: 'agent', params: { kind: 'agent', name: 'mavu', preview: false } },
    'file:%2Frepo:README.md': { id: 'file:%2Frepo:README.md', contentComponent: 'file', params: { kind: 'file', root: '/repo', path: 'README.md', preview: false, viewMode: 'rendered' } },
  },
  activeGroup: 'group-main',
}

test('screen restore waits for the fleet and rejects reused pane identities', () => {
  const params: ScreenPanelParams = {
    kind: 'screen', preview: false,
    pane: board.workspaces[0].tabs[0].panes[0],
    identity: { paneID: 'pane-1', workspaceID: 'workspace-1', tabID: 'tab-1', agent: 'mavu', sessionID: 'session-1' },
  }
  assert.equal(screenIdentityState(params), 'checking')
  assert.equal(screenIdentityState(params, board), 'ready')
  assert.equal(screenIdentityState({ ...params, identity: { ...params.identity, sessionID: 'other-session' } }, board), 'mismatch')
  assert.equal(screenIdentityState({ ...params, identity: { ...params.identity, paneID: 'vanished' } }, board), 'mismatch')
})

test('single-group pinned agent and file panels round-trip with a branch root', () => {
  const saved = persistableDockLayout(singleGroupDock)
  assert.ok(saved)
  assert.equal(saved.grid.root.type, 'branch')
  assert.equal(saved.grid.root.data.length, 1)
  const restored = parseStoredLayout(JSON.stringify({ version: 2, dock: saved, sidebarWidth: 250 }))
  assert.deepEqual(restored?.dock, saved)
  assert.deepEqual(Object.keys(restored?.dock?.panels ?? {}), ['agent:mavu', 'file:%2Frepo:README.md'])
  let loaded: unknown
  assert.equal(restoreDockLayout({ fromJSON: (value) => { loaded = value } }, restored.dock), true)
  assert.deepEqual(loaded, saved)
})

test('dock restore failures are visible before the shell falls back', () => {
  const dock = persistableDockLayout(singleGroupDock)
  assert.ok(dock)
  const errors: unknown[][] = []
  const originalError = console.error
  console.error = (...values: unknown[]) => { errors.push(values) }
  try {
    assert.equal(restoreDockLayout({ fromJSON: () => { throw new Error('root must be of type branch') } }, dock), false)
  } finally {
    console.error = originalError
  }
  assert.equal(errors.length, 1)
  assert.match(String(errors[0][0]), /Failed to restore persisted dock layout/)
  assert.match(String(errors[0][1]), /root must be of type branch/)
})

test('malformed and stale layout storage never blocks shell mount', () => {
  assert.equal(parseStoredLayout(null), null)
  assert.equal(parseStoredLayout('{broken'), null)
  assert.equal(parseStoredLayout(JSON.stringify({ version: 2, dock: { panels: [] } })), null)
  assert.equal(parseStoredLayout(JSON.stringify({ version: 99, dock: {} })), null)
  assert.equal(parseLegacyLayout('{broken'), null)
  assert.equal(parseLegacyLayout(JSON.stringify({ openTabs: ['mavu'], activeTab: 'agent:other', sidebarWidth: 250 })), null)
  assert.deepEqual(parseLegacyLayout(JSON.stringify({ openTabs: ['mavu', 'mavu'], activeTab: 'agent:mavu', sidebarWidth: 250 })), {
    openTabs: ['mavu'], activeTab: 'agent:mavu', sidebarWidth: 250,
  })

  const boardDock = {
    grid: { root: { type: 'branch', data: [{ type: 'leaf', data: { id: 'group-board', views: ['board'], activeView: 'board' } }] } },
    panels: { board: { id: 'board', contentComponent: 'board', params: { kind: 'board', preview: false } } },
    activeGroup: 'group-board',
  }
  assert.ok(parseStoredLayout(JSON.stringify({ version: 2, dock: boardDock, sidebarWidth: 250 })))
  assert.equal(parseStoredLayout(JSON.stringify({ version: 2, dock: {
    ...boardDock, panels: { board: { ...boardDock.panels.board, contentComponent: 'agent' } },
  }, sidebarWidth: 250 })), null)
  assert.equal(parseStoredLayout(JSON.stringify({ version: 2, dock: {
    ...boardDock, panels: { board: { ...boardDock.panels.board, id: 'agent:mavu' } },
  }, sidebarWidth: 250 })), null)
})

test('persisted dock JSON removes preview panels and empty groups', () => {
  const dock = {
    grid: {
      width: 900, height: 600, orientation: 0,
      root: {
        type: 'branch', size: 900, data: [
          { type: 'leaf', size: 450, data: { id: 'group-board', views: ['board'], activeView: 'board' } },
          { type: 'leaf', size: 450, data: { id: 'group-preview', views: ['file:%2Frepo:README.md'], activeView: 'file:%2Frepo:README.md' } },
        ],
      },
    },
    panels: {
      board: { id: 'board', contentComponent: 'board', params: { kind: 'board', preview: false } },
      'file:%2Frepo:README.md': { id: 'file:%2Frepo:README.md', contentComponent: 'file', params: { kind: 'file', root: '/repo', path: 'README.md', preview: true, viewMode: 'rendered' } },
    },
    activeGroup: 'group-preview',
  }
  const persisted = persistableDockLayout(dock)
  assert.ok(persisted)
  assert.deepEqual(Object.keys(persisted.panels), ['board'])
  assert.equal(persisted.grid.root.type, 'branch')
  assert.equal(persisted.grid.root.data[0].data.id, 'group-board')
  assert.equal(persisted.activeGroup, 'group-board')
})
