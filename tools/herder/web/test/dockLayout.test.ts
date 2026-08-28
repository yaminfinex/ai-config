import assert from 'node:assert/strict'
import test from 'node:test'
import {
  fileIdentityMatches,
  parseLegacyLayout,
  parseStoredLayout,
  persistableDockLayout,
  screenIdentityState,
  type FilePanelParams,
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

test('file restore re-proves the persisted root location', () => {
  const valid: FilePanelParams = { kind: 'file', root: '/repo', rootIdentity: '/repo', path: 'README.md', preview: false, viewMode: 'rendered' }
  assert.equal(fileIdentityMatches(valid), true)
  assert.equal(fileIdentityMatches({ ...valid, root: '/different-repo' }), false)
})

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
    grid: { root: { type: 'leaf', data: { id: 'group-board', views: ['board'], activeView: 'board' } } },
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
      'file:%2Frepo:README.md': { id: 'file:%2Frepo:README.md', contentComponent: 'file', params: { kind: 'file', root: '/repo', rootIdentity: '/repo', path: 'README.md', preview: true, viewMode: 'rendered' } },
    },
    activeGroup: 'group-preview',
  }
  const persisted = persistableDockLayout(dock)
  assert.ok(persisted)
  assert.deepEqual(Object.keys(persisted.panels), ['board'])
  assert.equal(persisted.grid.root.type, 'leaf')
  assert.equal(persisted.grid.root.data.id, 'group-board')
  assert.equal(persisted.activeGroup, 'group-board')
})
