import assert from 'node:assert/strict'
import test from 'node:test'
import {
  activePanelInDock,
  layoutStorageBackupKey,
  layoutStorageKey,
  parseLegacyLayout,
  parseStoredSpaceLayout,
  parseStoredLayout,
  pinMovedPreview,
  persistableDockLayout,
  readStoredLayout,
  catchUpExternalPanels,
  readStoredSpaceLayout,
  restoreDockLayout,
  writeStoredLayout,
  storedSpaceActivePanel,
  writeStoredSpaceLayout,
  writePanelToStoredSpace,
  spaceLayoutRecoveryPrefix,
  type DockPanelParams,
} from '../src/features/layout/dockLayout.ts'

const singleGroupDock = {
  grid: { root: { type: 'branch', data: [{
    type: 'leaf', data: { id: 'group-main', views: ['agent:mavu', 'file:%2Frepo:README.md', 'folder:%2Frepo:src'], activeView: 'folder:%2Frepo:src' },
  }] } },
  panels: {
    'agent:mavu': { id: 'agent:mavu', contentComponent: 'agent', params: { kind: 'agent', name: 'mavu', preview: false } },
    'file:%2Frepo:README.md': { id: 'file:%2Frepo:README.md', contentComponent: 'file', params: { kind: 'file', root: '/repo', path: 'README.md', preview: false, viewMode: 'rendered' } },
    'folder:%2Frepo:src': { id: 'folder:%2Frepo:src', contentComponent: 'folder', params: { kind: 'folder', root: '/repo', path: 'src', preview: false } },
  },
  activeGroup: 'group-main',
}

// Saved before the standalone terminal tab was retired: the herdr view (⌃`)
// replaced it. A pane herdr attributed to an agent was saved with that agent.
const screenPanel = (paneID: string, agent: string, preview = false) => ({
  id: `screen:${paneID}`, contentComponent: 'screen', tabComponent: 'herder-tab', title: paneID,
  params: {
    kind: 'screen', preview,
    pane: { pane_id: paneID, agent, tool: 'codex', herdr_status: 'active', bus_status: 'active', gap: '' },
    identity: { paneID, workspaceID: 'workspace-1', tabID: 'tab-1', agent, sessionID: 'session-1' },
  },
})

const legacyScreenDock = {
  grid: { root: { type: 'branch', data: [
    { type: 'leaf', size: 600, data: { id: 'group-main', views: ['agent:lead', 'screen:pane-1', 'screen:pane-2'], activeView: 'screen:pane-1' } },
    { type: 'leaf', size: 400, data: { id: 'group-side', views: ['screen:pane-9'], activeView: 'screen:pane-9' } },
  ] } },
  panels: {
    'agent:lead': { id: 'agent:lead', contentComponent: 'agent', tabComponent: 'herder-tab', title: 'lead', params: { kind: 'agent', name: 'lead', preview: false } },
    // mavu's terminal, the shown tab: it becomes mavu's transcript, in place.
    'screen:pane-1': screenPanel('pane-1', 'mavu'),
    // lead's terminal: lead already has a panel, so this one goes.
    'screen:pane-2': screenPanel('pane-2', 'lead', true),
    // A terminal herdr could not attribute: no agent, so it is dropped.
    'screen:pane-9': screenPanel('pane-9', '-'),
  },
  activeGroup: 'group-side',
}

test('a saved screen panel restores as its agent\'s transcript; an unattributed one is dropped', () => {
  for (const raw of [
    JSON.stringify({ version: 3, dock: legacyScreenDock, rails: { fleet: { width: 250, collapsed: false }, notes: { width: 280, collapsed: false } } }),
    JSON.stringify({ version: 4, dock: legacyScreenDock }),
  ]) {
    const dock = (JSON.parse(raw) as { version: number }).version === 3 ? parseStoredLayout(raw)?.dock : parseStoredSpaceLayout(raw)?.layout.dock
    assert.ok(dock, 'never an error, never a blank dock')
    assert.deepEqual(Object.keys(dock.panels), ['agent:lead', 'agent:mavu'])
    assert.deepEqual(dock.panels['agent:mavu'], { id: 'agent:mavu', contentComponent: 'agent', tabComponent: 'herder-tab', title: 'mavu', params: { kind: 'agent', name: 'mavu', preview: false } })
    assert.deepEqual(dock.grid.root, { type: 'branch', data: [
      { type: 'leaf', size: 600, data: { id: 'group-main', views: ['agent:lead', 'agent:mavu'], activeView: 'agent:mavu' } },
    ] })
    assert.equal(dock.activeGroup, 'group-main', 'the emptied group\'s focus falls back to a surviving group')
    assert.deepEqual(activePanelInDock(dock), { kind: 'agent', name: 'mavu', preview: false }, 'the shown terminal is now the shown transcript')
    assert.deepEqual(persistableDockLayout(dock), dock, 'the migrated dock is canonical')
    let loaded: unknown
    assert.equal(restoreDockLayout({ fromJSON: (value) => { loaded = value } }, dock), true)
    assert.deepEqual(loaded, dock)
  }
  // A migration is not a salvage: the space stays writable and its raw stays last-good.
  const values = new Map([['herder.web.layout.v4:alpha', JSON.stringify({ version: 4, dock: legacyScreenDock })]])
  const read = readStoredSpaceLayout({ getItem: (key) => values.get(key) ?? null }, 'alpha')
  assert.equal(read.problem, false)
  assert.equal(read.recovering, false)
  assert.deepEqual(Object.keys(read.stored?.dock?.panels ?? {}), ['agent:lead', 'agent:mavu'])
  // A preview terminal reopens as a preview transcript.
  const preview = parseStoredSpaceLayout(JSON.stringify({ version: 4, dock: {
    grid: { root: { type: 'branch', data: [{ type: 'leaf', data: { id: 'g', views: ['screen:pane-3'], activeView: 'screen:pane-3' } }] } },
    panels: { 'screen:pane-3': screenPanel('pane-3', 'zira', true) },
    activeGroup: 'g',
  } }))
  assert.deepEqual(preview?.layout.dock?.panels['agent:zira']?.params, { kind: 'agent', name: 'zira', preview: true })
  // A dock that held only an unattributed terminal restores empty, the default layout.
  const only = parseStoredSpaceLayout(JSON.stringify({ version: 4, dock: {
    grid: { root: { type: 'branch', data: [{ type: 'leaf', data: { id: 'g', views: ['screen:pane-9'], activeView: 'screen:pane-9' } }] } },
    panels: { 'screen:pane-9': screenPanel('pane-9', '-') },
    activeGroup: 'g',
  } }))
  assert.ok(only)
  assert.equal(only.layout.dock, null)
})

test('an agent panel saved in Screen mode restores as its transcript', () => {
  // Screen mode was session state (never a panel param), so a saved agent
  // panel restores as the transcript. A stray mode field from any build is
  // dropped rather than refusing the panel.
  const dock = {
    grid: { root: { type: 'branch', data: [{ type: 'leaf', data: { id: 'g', views: ['agent:mavu'], activeView: 'agent:mavu' } }] } },
    panels: { 'agent:mavu': { id: 'agent:mavu', contentComponent: 'agent', tabComponent: 'herder-tab', title: 'mavu', params: { kind: 'agent', name: 'mavu', preview: false, view: 'screen', screenPaneID: 'pane-1' } } },
    activeGroup: 'g',
  }
  const restored = parseStoredSpaceLayout(JSON.stringify({ version: 4, dock }))
  assert.deepEqual(restored?.layout.dock?.panels['agent:mavu']?.params, { kind: 'agent', name: 'mavu', preview: false })
  assert.deepEqual(activePanelInDock(restored?.layout.dock), { kind: 'agent', name: 'mavu', preview: false })
})

test('single-group pinned agent, file, and folder panels round-trip with a branch root', () => {
  const saved = persistableDockLayout(singleGroupDock)
  assert.ok(saved)
  assert.equal(saved.grid.root.type, 'branch')
  assert.equal(saved.grid.root.data.length, 1)
  const restored = parseStoredLayout(JSON.stringify({ version: 2, dock: saved, sidebarWidth: 250 }))
  assert.deepEqual(restored?.dock, saved)
  assert.deepEqual(Object.keys(restored?.dock?.panels ?? {}), ['agent:mavu', 'file:%2Frepo:README.md', 'folder:%2Frepo:src'])
  let loaded: unknown
  assert.equal(restoreDockLayout({ fromJSON: (value) => { loaded = value } }, restored.dock), true)
  assert.deepEqual(loaded, saved)
})

test('v2 sidebar preferences migrate into the single v3 rail preference family', () => {
  const restored = parseStoredLayout(JSON.stringify({
    version: 2,
    dock: singleGroupDock,
    sidebarWidth: 275,
    expandedItems: ['workspace:w1'],
    knownWorkspaceItems: ['workspace:w1'],
  }))
  assert.deepEqual(restored?.rails, {
    fleet: { width: 275, collapsed: false },
    notes: { width: 280, collapsed: false },
  })
  assert.equal(restored?.version, 3)
  assert.deepEqual(restored?.expandedItems, ['workspace:w1'])
})

test('v3 rail widths and collapsed states round-trip together', () => {
  const restored = parseStoredLayout(JSON.stringify({
    version: 3,
    dock: singleGroupDock,
    rails: {
      fleet: { width: 320, collapsed: true },
      notes: { width: 240, collapsed: false },
    },
  }))
  assert.deepEqual(restored?.rails, {
    fleet: { width: 320, collapsed: true },
    notes: { width: 240, collapsed: false },
  })
})

test('v4 dock-only layouts reuse the one sanitizer and isolate keys by space', () => {
  const raw = JSON.stringify({ version: 4, dock: singleGroupDock })
  const parsed = parseStoredSpaceLayout(raw)
  assert.ok(parsed?.layout.dock)
  assert.equal(parsed.layout.dock.grid.root.type, 'branch')

  const values = new Map<string, string>()
  const storage = {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => { values.set(key, value) },
  }
  const initial = { recovering: false, lastGoodRaw: null }
  assert.notEqual(writeStoredSpaceLayout(storage, 'alpha', raw, initial), initial)
  assert.notEqual(writeStoredSpaceLayout(storage, 'beta', JSON.stringify({ version: 4, dock: null }), initial), initial)
  assert.ok(readStoredSpaceLayout(storage, 'alpha').stored?.dock)
  assert.equal(readStoredSpaceLayout(storage, 'beta').stored?.dock, null)
})

test('a saved space names its focused panel: the active group\'s shown tab, which a zoom returns to', () => {
  const split = {
    grid: { root: { type: 'branch', data: [
      { type: 'leaf', data: { id: 'group-lead', views: ['agent:lead'], activeView: 'agent:lead' } },
      { type: 'leaf', data: { id: 'group-side', views: ['agent:mavu', 'agent:ziru'], activeView: 'agent:ziru' } },
    ] } },
    panels: {
      'agent:lead': { id: 'agent:lead', contentComponent: 'agent', params: { kind: 'agent', name: 'lead', preview: false } },
      'agent:mavu': { id: 'agent:mavu', contentComponent: 'agent', params: { kind: 'agent', name: 'mavu', preview: false } },
      'agent:ziru': { id: 'agent:ziru', contentComponent: 'agent', params: { kind: 'agent', name: 'ziru', preview: true } },
    },
    activeGroup: 'group-side',
  }
  assert.deepEqual(activePanelInDock(split as never), { kind: 'agent', name: 'ziru', preview: true }, 'not the first group\'s lead')
  assert.deepEqual(activePanelInDock({ ...split, activeGroup: undefined } as never), { kind: 'agent', name: 'lead', preview: false }, 'no active group: the first')
  assert.equal(activePanelInDock(singleGroupDock as never)?.kind, 'folder', 'a file or folder tab counts as the focused panel')
  assert.equal(activePanelInDock(null), undefined)

  const values = new Map<string, string>()
  const storage = { getItem: (key: string) => values.get(key) ?? null, setItem: (key: string, value: string) => { values.set(key, value) } }
  writeStoredSpaceLayout(storage, 'herder', JSON.stringify({ version: 4, dock: split }), { recovering: false, lastGoodRaw: null })
  assert.deepEqual(storedSpaceActivePanel(storage, 'herder'), { kind: 'agent', name: 'ziru', preview: true })
  assert.equal(storedSpaceActivePanel(storage, 'never-saved'), undefined)
  values.set('herder.web.layout.v4:broken', '{')
  assert.equal(storedSpaceActivePanel({ getItem: storage.getItem }, 'broken'), undefined, 'an unreadable layout has none, and is read without a recovery write')
  assert.equal(values.has('herder.web.layout.v4.recovery:broken'), false)
})

test('one corrupt v4 space preserves raw and cannot poison another space', () => {
  const values = new Map<string, string>([
    ['herder.web.layout.v4:alpha', '{broken'],
    ['herder.web.layout.v4:beta', JSON.stringify({ version: 4, dock: singleGroupDock })],
  ])
  const storage = {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => { values.set(key, value) },
  }
  assert.equal(readStoredSpaceLayout(storage, 'alpha').stored, null)
  assert.ok(values.has(`${spaceLayoutRecoveryPrefix}alpha`))
  assert.ok(readStoredSpaceLayout(storage, 'beta').stored?.dock)
})

test('a panel is appended to the target active group with its exact params', () => {
  const values = new Map<string, string>([[
    'herder.web.layout.v4:target', JSON.stringify({ version: 4, dock: singleGroupDock }),
  ]])
  const storage = {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => { values.set(key, value) },
  }
  const params: DockPanelParams = { kind: 'agent', name: 'ziru', preview: true }
  assert.deepEqual(writePanelToStoredSpace(storage, 'target', params), { ok: true, duplicate: false })
  const dock = readStoredSpaceLayout(storage, 'target').stored?.dock
  assert.deepEqual(dock?.panels['agent:ziru']?.params, params)
  assert.equal(dock?.panels['agent:ziru']?.tabComponent, 'herder-tab')
  assert.deepEqual(dock?.grid.root.data[0].data.views, [
    'agent:mavu', 'file:%2Frepo:README.md', 'folder:%2Frepo:src', 'agent:ziru',
  ])
  assert.equal(dock?.grid.root.data[0].data.activeView, 'agent:ziru')
})

test('a single-group layout as Dockview serializes it reads clean and accepts a panel', () => {
  // Dockview gives the root branch its own size and the lone leaf another.
  const raw = JSON.stringify({ version: 4, dock: {
    ...singleGroupDock,
    grid: { root: { type: 'branch', data: [{ ...singleGroupDock.grid.root.data[0], size: 900 }], size: 876 }, width: 900, height: 876, orientation: 'HORIZONTAL' },
  } })
  assert.equal(parseStoredSpaceLayout(raw)?.salvaged, false)
  const values = new Map<string, string>([['herder.web.layout.v4:target', raw], ['herder.web.layout.v4.last-good:target', raw]])
  const storage = {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => { values.set(key, value) },
  }
  assert.equal(readStoredSpaceLayout(storage, 'target').recovering, false)
  assert.deepEqual(writePanelToStoredSpace(storage, 'target', { kind: 'agent', name: 'ziru', preview: false }), { ok: true, duplicate: false })
  const root = readStoredSpaceLayout(storage, 'target').stored?.dock?.grid.root
  assert.equal(root?.size, 876)
  assert.equal(root?.data[0].size, 900)
  assert.deepEqual(root?.data[0].data.views, ['agent:mavu', 'file:%2Frepo:README.md', 'folder:%2Frepo:src', 'agent:ziru'])
})

test('an empty target receives one canonical branch-root group', () => {
  const values = new Map<string, string>([[
    'herder.web.layout.v4:empty', JSON.stringify({ version: 4, dock: null }),
  ]])
  const storage = {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => { values.set(key, value) },
  }
  const params: DockPanelParams = { kind: 'changes', root: '/repo', preview: false }
  assert.deepEqual(writePanelToStoredSpace(storage, 'empty', params), { ok: true, duplicate: false })
  const dock = readStoredSpaceLayout(storage, 'empty').stored?.dock
  assert.equal(dock?.grid.root.type, 'branch')
  assert.deepEqual(dock?.grid.root.data[0].data.views, ['changes:%2Frepo'])
  assert.equal(dock?.activeGroup, dock?.grid.root.data[0].data.id)
})

test('an existing target panel is activated without duplication', () => {
  const values = new Map<string, string>([[
    'herder.web.layout.v4:target', JSON.stringify({ version: 4, dock: singleGroupDock }),
  ]])
  const storage = {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => { values.set(key, value) },
  }
  const params: DockPanelParams = { kind: 'agent', name: 'mavu', preview: false }
  assert.deepEqual(writePanelToStoredSpace(storage, 'target', params), { ok: true, duplicate: true })
  const dock = readStoredSpaceLayout(storage, 'target').stored?.dock
  assert.equal(Object.keys(dock?.panels ?? {}).filter((id) => id === 'agent:mavu').length, 1)
  assert.equal(dock?.grid.root.data[0].data.activeView, 'agent:mavu')
})

test('corrupt, recovering, and failed-write targets are refused unchanged', () => {
  for (const entries of [
    [['herder.web.layout.v4:target', '{broken']],
    [
      ['herder.web.layout.v4:target', JSON.stringify({ version: 4, dock: { ...singleGroupDock, panels: { ...singleGroupDock.panels, broken: {} } } })],
      ['herder.web.layout.v4.last-good:target', JSON.stringify({ version: 4, dock: singleGroupDock })],
    ],
  ] as Array<Array<[string, string]>>) {
    const values = new Map<string, string>(entries)
    const before = values.get('herder.web.layout.v4:target')
    const storage = {
      getItem: (key: string) => values.get(key) ?? null,
      setItem: (key: string, value: string) => { values.set(key, value) },
    }
    assert.equal(writePanelToStoredSpace(storage, 'target', { kind: 'agent', name: 'ziru', preview: false }).ok, false)
    assert.equal(values.get('herder.web.layout.v4:target'), before)
  }

  const raw = JSON.stringify({ version: 4, dock: singleGroupDock })
  let wrote = false
  const storage = {
    getItem: (key: string) => key === 'herder.web.layout.v4:target' ? raw : null,
    setItem: () => { wrote = true },
  }
  assert.deepEqual(writePanelToStoredSpace(storage, 'target', { kind: 'agent', name: 'ziru', preview: false }), { ok: false, reason: 'write' })
  assert.equal(wrote, true, 'the write was accepted; only the mismatched read-back detects failure')
})

test('external pane catch-up survives a stale live-tab flush before its storage event', () => {
  const values = new Map<string, string>([[
    'herder.web.layout.v4:target', JSON.stringify({ version: 4, dock: singleGroupDock }),
  ]])
  const storage = {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => { values.set(key, value) },
  }
  assert.equal(writePanelToStoredSpace(storage, 'target', { kind: 'agent', name: 'ziru', preview: false }).ok, true)
  const externalEventValue = values.get('herder.web.layout.v4:target') ?? null

  const stale = JSON.stringify({ version: 4, dock: singleGroupDock })
  storage.setItem('herder.web.layout.v4:target', stale)
  const livePanels = new Map(Object.entries(singleGroupDock.panels))
  const added = catchUpExternalPanels(externalEventValue, {
    hasPanel: (id) => livePanels.has(id),
    addPanel: (id, params) => { livePanels.set(id, { id, contentComponent: params.kind, params }) },
  })
  assert.deepEqual(added, ['agent:ziru'])

  const mergedDock = {
    ...singleGroupDock,
    panels: Object.fromEntries(livePanels),
    grid: { root: { type: 'branch', data: [{
      type: 'leaf', data: { id: 'group-main', views: [...livePanels.keys()], activeView: 'agent:mavu' },
    }] } },
  }
  storage.setItem('herder.web.layout.v4:target', JSON.stringify({ version: 4, dock: mergedDock }))
  assert.ok(readStoredSpaceLayout(storage, 'target').stored?.dock?.panels['agent:ziru'])
})

test('an invalid v3 rail preference falls back to the last-good layout', () => {
  const backupRaw = JSON.stringify({
    version: 3,
    dock: singleGroupDock,
    rails: {
      fleet: { width: 300, collapsed: false },
      notes: { width: 260, collapsed: true },
    },
  })
  const values = new Map<string, string>([
    [layoutStorageKey, JSON.stringify({
      version: 3,
      dock: singleGroupDock,
      rails: { fleet: { width: 300, collapsed: 'no' }, notes: { width: 260, collapsed: false } },
    })],
    [layoutStorageBackupKey, backupRaw],
  ])
  const layouts = readStoredLayout({ getItem: (key) => values.get(key) ?? null })
  assert.equal(layouts.recovering, true)
  assert.deepEqual(layouts.stored?.rails.notes, { width: 260, collapsed: true })
})

test('saved and restored layouts cannot carry a transient folder selection hint', () => {
  const hinted = structuredClone(singleGroupDock)
  Object.assign(hinted.panels['folder:%2Frepo:src'].params, { selectionHint: { root: '/repo', path: 'src/App.tsx' } })
  const saved = persistableDockLayout(hinted)
  assert.ok(saved)
  assert.equal('selectionHint' in saved.panels['folder:%2Frepo:src'].params, false)

  const restored = parseStoredLayout(JSON.stringify({ version: 2, dock: hinted, sidebarWidth: 250 }))
  assert.ok(restored?.dock)
  assert.equal('selectionHint' in restored.dock.panels['folder:%2Frepo:src'].params, false)
})

test('an Option-right split round-trips through browser persistence with previews intact', () => {
  const splitDock = {
    grid: { width: 1200, height: 700, orientation: 0, root: { type: 'branch', data: [
      { type: 'leaf', size: 600, data: { id: 'group-agent', views: ['agent:mavu'], activeView: 'agent:mavu' } },
      { type: 'leaf', size: 600, data: { id: 'group-side', views: ['file:%2Frepo:README.md'], activeView: 'file:%2Frepo:README.md' } },
    ] } },
    panels: {
      'agent:mavu': singleGroupDock.panels['agent:mavu'],
      'file:%2Frepo:README.md': {
        ...singleGroupDock.panels['file:%2Frepo:README.md'],
        params: { ...singleGroupDock.panels['file:%2Frepo:README.md'].params, preview: true },
      },
    },
    activeGroup: 'group-side',
  }
  const saved = persistableDockLayout(splitDock)
  assert.ok(saved)
  const values = new Map<string, string>()
  const storage = {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => { values.set(key, value) },
  }
  const raw = JSON.stringify({ version: 2, dock: saved, sidebarWidth: 250 })
  writeStoredLayout(storage, raw, { recovering: false, lastGoodRaw: null })
  const restored = readStoredLayout(storage).stored?.dock
  assert.ok(restored)
  assert.equal(restored.grid.orientation, 0)
  assert.deepEqual(restored.grid.root.data.map((node: { data: { id: string } }) => node.data.id), ['group-agent', 'group-side'])
  assert.equal(restored.activeGroup, 'group-side')
  assert.equal(restored.panels['file:%2Frepo:README.md'].params.preview, true)
  let loaded: unknown
  assert.equal(restoreDockLayout({ fromJSON: (value) => { loaded = value } }, restored), true)
  assert.deepEqual(loaded, saved)
})

test('maximize is deliberately ephemeral across persistence and salvage', () => {
  const maximized = { ...singleGroupDock, maximizedNode: { location: [0] } }
  const saved = persistableDockLayout(maximized)
  assert.ok(saved)
  assert.equal('maximizedNode' in saved, false)

  const restored = parseStoredLayout(JSON.stringify({ version: 2, dock: maximized, sidebarWidth: 250 }))
  assert.ok(restored?.dock)
  assert.equal('maximizedNode' in restored.dock, false)
  assert.deepEqual(Object.keys(restored.dock.panels), Object.keys(singleGroupDock.panels))
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

})

test('stored v2 layouts drop the retired Board panel and retain healthy neighbours', () => {
  const dock = {
    grid: {
      width: 900, height: 600, orientation: 0,
      root: {
        type: 'branch', size: 900, data: [
          { type: 'leaf', size: 450, data: { id: 'group-board', views: ['board'], activeView: 'board' } },
          { type: 'leaf', size: 450, data: { id: 'group-agent', views: ['agent:mavu'], activeView: 'agent:mavu' } },
        ],
      },
    },
    panels: {
      board: { id: 'board', contentComponent: 'board', params: { kind: 'board', preview: false } },
      'agent:mavu': { id: 'agent:mavu', contentComponent: 'agent', params: { kind: 'agent', name: 'mavu', preview: false } },
    },
    activeGroup: 'group-board',
  }
  const restored = parseStoredLayout(JSON.stringify({ version: 2, dock, sidebarWidth: 250 }))
  assert.deepEqual(Object.keys(restored?.dock?.panels ?? {}), ['agent:mavu'])
  assert.equal(restored?.dock?.grid.root.data[0].data.id, 'group-agent')
  assert.equal(restored?.dock?.activeGroup, 'group-agent')

  const boardOnly = { ...dock, grid: { root: { type: 'branch', data: [dock.grid.root.data[0]] } }, panels: { board: dock.panels.board } }
  assert.equal(parseStoredLayout(JSON.stringify({ version: 2, dock: boardOnly, sidebarWidth: 250 }))?.dock, null)
})

test('preview panels persist as previews with their active tab intact', () => {
  const preview = { kind: 'file', root: '/repo', path: 'README.md', preview: true, viewMode: 'rendered' } as const
  const dock = {
    grid: { width: 900, height: 600, orientation: 0, root: { type: 'branch', data: [{
      type: 'leaf', data: {
        id: 'group-file', views: ['agent:mavu', 'file:%2Frepo:README.md'], activeView: 'file:%2Frepo:README.md',
        tabGroups: [{ id: 'tabs-main', panelIds: ['agent:mavu', 'file:%2Frepo:README.md'] }],
      },
    }] } },
    panels: {
      'agent:mavu': { id: 'agent:mavu', contentComponent: 'agent', params: { kind: 'agent', name: 'mavu', preview: false } },
      'file:%2Frepo:README.md': { id: 'file:%2Frepo:README.md', contentComponent: 'file', params: preview },
    },
    activeGroup: 'group-file',
  }
  const saved = persistableDockLayout(dock)
  assert.ok(saved)
  const restored = parseStoredLayout(JSON.stringify({ version: 2, dock: saved, sidebarWidth: 250 }))?.dock
  assert.deepEqual(restored, saved)
  assert.equal(restored.panels['file:%2Frepo:README.md'].params.preview, true)
  assert.equal(restored.grid.root.data[0].data.activeView, 'file:%2Frepo:README.md')
})

test('moved previews still pin without changing preview replacement semantics', () => {
  const preview = { kind: 'file', root: '/repo', path: 'README.md', preview: true, viewMode: 'rendered' } as const
  let moved: DockPanelParams = preview
  assert.equal(pinMovedPreview({ params: preview, api: { updateParameters: (params) => { moved = params } } }), true)
  assert.equal(moved.preview, false)
})

test('a pinned Changes panel validates and persists by opaque root', () => {
  const id = 'changes:%2Frepo'
  const dock = {
    grid: { root: { type: 'branch', data: [{ type: 'leaf', data: { id: 'group-changes', views: [id], activeView: id } }] } },
    panels: { [id]: { id, contentComponent: 'changes', params: { kind: 'changes', root: '/repo', preview: false } } },
    activeGroup: 'group-changes',
  }
  const saved = persistableDockLayout(dock)
  assert.ok(saved)
  assert.ok(parseStoredLayout(JSON.stringify({ version: 2, dock: saved, sidebarWidth: 250 }))?.dock)
})

test('one poisoned panel is salvaged while healthy neighbours and backup stay intact', () => {
  const poisoned = structuredClone(singleGroupDock)
  delete (poisoned.panels['file:%2Frepo:README.md'].params as Partial<typeof poisoned.panels['file:%2Frepo:README.md']['params']>).viewMode
  const backupRaw = JSON.stringify({ version: 2, dock: singleGroupDock, sidebarWidth: 275 })
  const values = new Map<string, string>([
    [layoutStorageKey, JSON.stringify({ version: 2, dock: poisoned, sidebarWidth: 250 })],
    [layoutStorageBackupKey, backupRaw],
  ])
  const layouts = readStoredLayout({ getItem: (key) => values.get(key) ?? null })
  assert.equal(layouts.recovering, true)
  assert.deepEqual(Object.keys(layouts.stored?.dock?.panels ?? {}), ['agent:mavu', 'folder:%2Frepo:src'])
  assert.deepEqual(layouts.stored?.dock?.grid.root.data[0].data.views, ['agent:mavu', 'folder:%2Frepo:src'])
  assert.equal(layouts.stored?.dock?.grid.root.data[0].data.activeView, 'folder:%2Frepo:src')
  assert.equal(values.get(layoutStorageBackupKey), backupRaw)
})

test('the first valid save after recovery cannot rotate a poisoned primary over the backup', () => {
  const backupRaw = JSON.stringify({ version: 2, dock: singleGroupDock, sidebarWidth: 275 })
  const values = new Map<string, string>([[layoutStorageBackupKey, backupRaw]])
  const storage = { setItem: (key: string, value: string) => { values.set(key, value) } }
  const recoveredRaw = JSON.stringify({ version: 2, dock: null, sidebarWidth: 250 })
  const recovered = writeStoredLayout(storage, recoveredRaw, { recovering: true, lastGoodRaw: null })
  assert.equal(values.get(layoutStorageKey), recoveredRaw)
  assert.equal(values.get(layoutStorageBackupKey), backupRaw)

  const nextRaw = JSON.stringify({ version: 2, dock: null, sidebarWidth: 260 })
  writeStoredLayout(storage, nextRaw, recovered)
  assert.equal(values.get(layoutStorageKey), nextRaw)
  assert.equal(values.get(layoutStorageBackupKey), recoveredRaw)
})

test('pruning a nested group keeps branch depth and split orientation', () => {
  const dock = {
    grid: {
      width: 1200, height: 700, orientation: 0,
      root: { type: 'branch', size: 1200, data: [
        { type: 'leaf', size: 300, data: { id: 'group-retired', views: ['board'], activeView: 'board' } },
        { type: 'branch', size: 900, data: [
          { type: 'leaf', size: 450, data: { id: 'group-agent', views: ['agent:mavu'], activeView: 'agent:mavu' } },
          { type: 'leaf', size: 450, data: { id: 'group-folder', views: ['folder:%2Frepo:src'], activeView: 'folder:%2Frepo:src' } },
        ] },
      ] },
    },
    panels: {
      board: { id: 'board', contentComponent: 'board', params: { kind: 'board', preview: false } },
      'agent:mavu': singleGroupDock.panels['agent:mavu'],
      'folder:%2Frepo:src': singleGroupDock.panels['folder:%2Frepo:src'],
    },
    activeGroup: 'group-folder',
  }
  const restored = parseStoredLayout(JSON.stringify({ version: 2, dock, sidebarWidth: 250 }))?.dock
  assert.ok(restored)
  assert.equal(restored.grid.orientation, 0)
  assert.equal(restored.grid.root.data.length, 1)
  assert.equal(restored.grid.root.data[0].type, 'branch')
  assert.deepEqual(restored.grid.root.data[0].data.map((node: { data: { id: string } }) => node.data.id), ['group-agent', 'group-folder'])
})
