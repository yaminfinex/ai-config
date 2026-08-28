import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import {
  DockviewReact,
  type DockviewApi,
  type DockviewReadyEvent,
  type DockviewTheme,
  type IDockviewHeaderActionsProps,
  type IDockviewPanelHeaderProps,
  type IDockviewPanelProps,
  type IWatermarkPanelProps,
} from 'dockview-react'
import { apiProblem, getFleet, queryKeys, viewerReadOnlyMessage } from './api/client'
import { viewerQueryOptions } from './api/queries'
import { BoardPanel } from './features/board/BoardPanel'
import { FleetSidebar } from './features/sidebar/FleetSidebar'
import { AgentPanel } from './features/transcript/AgentPanel'
import { ScreenPanel } from './features/screen/ScreenPanel'
import { AppLink, currentRoute, type Route } from './shared/navigation'
import { agentBusStatus } from './shared/agentStatus'
import { AgentStatusDot, Banner } from './shared/presentation'
import { ThemeToggle } from './shared/ThemeToggle'
import { useFleetStream, type StreamState } from './stream/useFleetStream'
import { agentTabID } from './previewTabs'
import type { Board, FileTarget, Pane } from './types'
import { FilePanel } from './features/files/FilePanel'
import { QuickOpen } from './features/files/QuickOpen'
import { fileTabID, isMarkdownPath, type FileViewMode } from './features/files/fileTabs'
import { isQuickOpenShortcut } from './features/files/fileShortcut'
import { quickOpenAgentPreference, rootLabel } from './features/files/fileResolution'
import {
  layoutStorageKey,
  legacyLayoutStorageKey,
  panelParams,
  parseLegacyLayout,
  parseStoredLayout,
  persistableDockLayout,
  restoreDockLayout,
  screenIdentityState,
  screenPanelParams,
  type AgentPanelParams,
  type DockPanelParams,
  type FilePanelParams,
  type LegacyLayout,
  type ScreenPanelParams,
  type StoredLayout,
} from './features/layout/dockLayout'

const boardPanel: DockPanelParams = { kind: 'board', preview: false }
const defaultSidebarWidth = 250
const herderTheme: DockviewTheme = {
  name: 'herder', className: 'dockview-theme-herder', gap: 0,
  dndOverlayMounting: 'absolute', dndPanelOverlay: 'group', dndTabIndicator: 'line',
  dndOverlayBorder: '2px solid var(--accent)', tabGroupIndicator: 'none', tabAnimation: 'smooth',
}

type InitialLayout = {
  stored: StoredLayout | null
  legacy: LegacyLayout | null
  sidebarWidth: number
  expandedItems: string[] | null
  knownWorkspaceItems: string[] | null
}

function clampSidebarWidth(width: number) {
  return Math.min(440, Math.max(200, width))
}

function readInitialLayout(): InitialLayout {
  let stored: StoredLayout | null = null
  let legacy: LegacyLayout | null = null
  try {
    stored = parseStoredLayout(localStorage.getItem(layoutStorageKey))
    if (!stored) legacy = parseLegacyLayout(localStorage.getItem(legacyLayoutStorageKey))
  } catch { /* browser storage is best effort */ }
  const source = stored ?? legacy
  return {
    stored, legacy,
    sidebarWidth: clampSidebarWidth(source?.sidebarWidth ?? defaultSidebarWidth),
    expandedItems: source?.expandedItems ?? null,
    knownWorkspaceItems: source?.knownWorkspaceItems ?? null,
  }
}

function screenTabID(paneID: string) { return `screen:${paneID}` }

function dockPanelID(params: DockPanelParams) {
  if (params.kind === 'board') return 'board'
  if (params.kind === 'agent') return agentTabID(params.name)
  if (params.kind === 'screen') return screenTabID(params.pane.pane_id)
  return fileTabID(params.root, params.path)
}

function panelTitle(params: DockPanelParams) {
  if (params.kind === 'board') return 'Board'
  if (params.kind === 'agent') return params.name
  if (params.kind === 'screen') return params.pane.label || params.pane.pane_id
  return rootLabel(params.path)
}

function setPathForPanel(params: DockPanelParams, push = true) {
  if (params.kind !== 'board' && params.kind !== 'agent') return
  const path = params.kind === 'board' ? '/' : `/agents/${encodeURIComponent(params.name)}`
  if (push && window.location.pathname !== path) window.history.pushState({}, '', path)
}

function panelFromAPI(api: DockviewApi, id: string) {
  const panel = api.getPanel(id)
  const params = panelParams(panel?.params)
  return panel && params ? { panel, params } : null
}

function visiblePane(board: Board | undefined, params: ScreenPanelParams): Pane | undefined {
  if (screenIdentityState(params, board) !== 'ready') return undefined
  for (const workspace of board?.workspaces ?? []) {
    for (const tab of workspace.tabs) {
      const pane = tab.panes.find((candidate) => candidate.pane_id === params.identity.paneID)
      if (pane) return pane
    }
  }
}

type WorkspaceContextValue = {
  board?: Board
  lifecycleBanner: (key: string, detail: string) => void
  identityReadOnly: string
  openFile: (target: FileTarget) => void
  pinPanel: (id: string) => void
  setFileViewMode: (id: string, mode: FileViewMode) => void
  agentScreenPanes: Record<string, string>
  setAgentScreenPane: (name: string, paneID?: string) => void
  onViewer: (viewer: string) => void
  onAgentStatus: (name: string, status: string) => void
  agentStatuses: Record<string, string>
  openBoard: () => void
  resetLayout: () => void
  showQuickOpen: (groupID?: string) => void
  stream: StreamState
  streamProblems: Record<string, string>
}

const WorkspaceContext = createContext<WorkspaceContextValue | null>(null)

function useWorkspace() {
  const value = useContext(WorkspaceContext)
  if (!value) throw new Error('dock workspace context is unavailable')
  return value
}

function usePanelVisibility(api: IDockviewPanelProps['api']) {
  const [visible, setVisible] = useState(api.isVisible)
  useEffect(() => {
    setVisible(api.isVisible)
    const disposable = api.onDidVisibilityChange((event) => setVisible(event.isVisible))
    return () => disposable.dispose()
  }, [api])
  return visible
}

function BoardDockPanel() {
  const workspace = useWorkspace()
  return <BoardPanel board={workspace.board} onBanner={workspace.lifecycleBanner} />
}

function AgentDockPanel({ params, api }: IDockviewPanelProps<AgentPanelParams>) {
  const workspace = useWorkspace()
  const visible = usePanelVisibility(api)
  return <AgentPanel name={params.name} active={visible} liveStatus={agentBusStatus(workspace.board, params.name)} screenPaneID={workspace.agentScreenPanes[params.name]}
    onScreenPane={(paneID) => workspace.setAgentScreenPane(params.name, paneID)} onOpenFile={workspace.openFile}
    identityReadOnly={workspace.identityReadOnly} onViewer={workspace.onViewer} onSend={() => workspace.pinPanel(api.id)} onStatus={workspace.onAgentStatus} />
}

function ScreenDockPanel({ params }: IDockviewPanelProps<ScreenPanelParams>) {
  const workspace = useWorkspace()
  const identity = screenIdentityState(params, workspace.board)
  if (identity === 'checking') return <main className="panel-unavailable" role="status"><strong>Verifying screen identity…</strong><p>The live fleet must confirm this saved pane before it can be subscribed.</p></main>
  const pane = visiblePane(workspace.board, params)
  if (!pane) return <main className="panel-unavailable tombstone" role="status"><strong>Screen no longer matches</strong><p>The saved pane identity is gone or now belongs to different live evidence. No replacement pane was opened.</p></main>
  return <ScreenPanel pane={pane} />
}

function FileDockPanel({ params, api }: IDockviewPanelProps<FilePanelParams>) {
  const workspace = useWorkspace()
  return <FilePanel target={{ root: params.root, path: params.path, ...(params.line ? { line: params.line } : {}) }} viewMode={params.viewMode}
    onViewMode={(mode) => workspace.setFileViewMode(api.id, mode)} onOpenFile={workspace.openFile} />
}

function DockTab({ params, api }: IDockviewPanelHeaderProps<DockPanelParams>) {
  const workspace = useWorkspace()
  const boardStatus = params.kind === 'agent' ? agentBusStatus(workspace.board, params.name) : '-'
  const status = params.kind === 'agent' && boardStatus === '-' ? workspace.agentStatuses[params.name] ?? '-' : boardStatus
  const meta = params.kind === 'agent' ? status !== '-' ? status : 'unknown' : params.kind === 'screen' ? 'read-only' : params.kind === 'file' ? 'file · read-only' : ''
  return <div className={`herder-dock-tab${params.preview ? ' preview' : ''}`} title={params.preview ? 'Preview — double-click to pin' : undefined}
    onDoubleClick={(event) => { if (params.preview) workspace.pinPanel(api.id); event.stopPropagation() }}
    onAuxClick={(event) => { if (event.button === 1) api.close() }}>
    <span className="dock-tab-label">{params.kind === 'board' ? '⌗ ' : params.kind === 'screen' ? '▣ ' : params.kind === 'file' ? '◇ ' : ''}{panelTitle(params)}</span>
    {meta && <span className="dock-tab-meta">{params.kind === 'agent' && <AgentStatusDot status={status} />}{meta}</span>}
    <button type="button" className="dock-tab-close" aria-label={`Close ${panelTitle(params)}`} onPointerDown={(event) => event.stopPropagation()} onClick={() => api.close()}>×</button>
  </div>
}

function DockHeaderActions({ containerApi, group, activePanel }: IDockviewHeaderActionsProps) {
  const workspace = useWorkspace()
  const primary = containerApi.groups[0]?.id === group.id
  return <div className="dock-header-actions">
    <button type="button" className="new-tab" title="Quick open file · Ctrl/Cmd+K" aria-label="Quick open file in this group" onClick={() => {
      activePanel?.api.setActive()
      workspace.showQuickOpen(group.id)
    }}>+</button>
    {primary && <>
      <span className={`stream-chip${workspace.streamProblems.stream ? ' fault' : ''}`}>{workspace.streamProblems.stream ? 'SSE: reconnecting' : 'SSE: connected'}</span>
      <span className="layout-chip" title="Drag tabs to an edge to split · Ctrl/Cmd+W close · Ctrl/Cmd+PageUp/PageDown switch · Alt+1 sidebar · Alt+2 composer">layout: this browser</span>
      <ThemeToggle />
    </>}
  </div>
}

function DockWatermark({ containerApi }: IWatermarkPanelProps) {
  const workspace = useWorkspace()
  return <section className="dock-watermark" role="status"><strong>No panels open</strong><p>Open the fleet board or find a file. Your sidebar and shortcuts are still available.</p><div>
    <button type="button" onClick={workspace.openBoard}>Open Board</button>
    <button type="button" onClick={() => workspace.showQuickOpen(containerApi.activeGroup?.id)}>Quick Open</button>
    <button type="button" onClick={workspace.resetLayout}>Reset layout</button>
  </div></section>
}

const dockComponents = { board: BoardDockPanel, agent: AgentDockPanel, screen: ScreenDockPanel, file: FileDockPanel }

function Shell({ initialRoute }: { initialRoute: Exclude<Route, { page: 'missing' }> }) {
  const [initial] = useState(readInitialLayout)
  const [quickOpen, setQuickOpen] = useState(false)
  const [quickOpenGroup, setQuickOpenGroup] = useState<string>()
  const [sidebarWidth, setSidebarWidth] = useState(initial.sidebarWidth)
  const [expandedItems, setExpandedItems] = useState<string[] | null>(initial.expandedItems)
  const [knownWorkspaceItems, setKnownWorkspaceItems] = useState<string[] | null>(initial.knownWorkspaceItems)
  const [lifecycleProblems, setLifecycleProblems] = useState<Record<string, string>>({})
  const [agentStatuses, setAgentStatuses] = useState<Record<string, string>>({})
  const [agentScreenPanes, setAgentScreenPanes] = useState<Record<string, string>>({})
  const [activePanelID, setActivePanelID] = useState('')
  const [revision, setRevision] = useState(0)
  const [dockReady, setDockReady] = useState(false)
  const apiRef = useRef<DockviewApi | undefined>(undefined)
  const dockDisposables = useRef<Array<{ dispose: () => void }>>([])
  const queryClient = useQueryClient()
  const boardQuery = useQuery({ queryKey: queryKeys.fleet, queryFn: () => getFleet(), staleTime: Infinity, retry: false })
  const viewerQuery = useQuery(viewerQueryOptions())

  const syncDock = useCallback(() => {
    setActivePanelID(apiRef.current?.activePanel?.id ?? '')
    setRevision((value) => value + 1)
  }, [])

  const addPanel = useCallback((params: DockPanelParams, groupID?: string) => {
    const api = apiRef.current
    if (!api) return undefined
    const id = dockPanelID(params)
    const existing = api.getPanel(id)
    if (existing) {
      existing.api.updateParameters(params)
      existing.api.setActive()
      syncDock()
      return existing
    }
    const group = groupID ? api.getGroup(groupID) : api.activeGroup ?? api.groups[0]
    const added = api.addPanel({
      id, component: params.kind, tabComponent: 'herder-tab', title: panelTitle(params), params,
      ...(group ? { position: { referenceGroup: group.id, direction: 'within' as const } } : {}),
    })
    syncDock()
    return added
  }, [syncDock])

  const openBoard = useCallback(() => { addPanel(boardPanel) }, [addPanel])

  const openAgent = useCallback((name: string, preview: boolean, groupID?: string) => {
    const api = apiRef.current
    const group = groupID ? api?.getGroup(groupID) : api?.activeGroup ?? api?.groups[0]
    const existing = api?.getPanel(agentTabID(name))
    if (existing) {
      const current = panelParams(existing.params)
      existing.api.updateParameters(current?.kind === 'agent' ? { ...current, preview: current.preview && preview } : { kind: 'agent', name, preview })
      existing.api.setActive()
      syncDock()
      return
    }
    const replaced = preview ? group?.panels.find((panel) => {
      const current = panelParams(panel.params)
      return current?.kind === 'agent' && current.preview
    }) : undefined
    addPanel({ kind: 'agent', name, preview }, group?.id)
    if (replaced) api?.removePanel(replaced)
    syncDock()
  }, [addPanel, syncDock])

  const openScreen = useCallback((pane: Pane, preview: boolean) => {
    const api = apiRef.current
    if (!boardQuery.data) return
    const params = screenPanelParams(boardQuery.data, pane, preview)
    if (!params) return
    const existing = api?.getPanel(screenTabID(pane.pane_id))
    if (existing) {
      const current = panelParams(existing.params)
      existing.api.updateParameters(current?.kind === 'screen' ? { ...params, preview: current.preview && preview } : params)
      existing.api.setActive()
      syncDock()
      return
    }
    const group = api?.activeGroup ?? api?.groups[0]
    const replaced = preview ? group?.panels.find((panel) => {
      const current = panelParams(panel.params)
      return current?.kind === 'screen' && current.preview
    }) : undefined
    addPanel(params, group?.id)
    if (replaced) api?.removePanel(replaced)
    syncDock()
  }, [addPanel, boardQuery.data, syncDock])

  const openFile = useCallback((target: FileTarget) => {
    const api = apiRef.current
    if (!api) return
    const id = fileTabID(target.root, target.path)
    const existing = panelFromAPI(api, id)
    if (existing?.params.kind === 'file') {
      const params: FilePanelParams = { ...existing.params, ...target, viewMode: target.line ? 'source' : existing.params.viewMode }
      existing.panel.api.updateParameters(params)
      existing.panel.api.setActive()
      queryClient.invalidateQueries({ queryKey: queryKeys.file(target.root, target.path) })
      syncDock()
      return
    }
    const group = quickOpenGroup ? api.getGroup(quickOpenGroup) : api.activeGroup ?? api.groups[0]
    const replaced = group?.panels.find((panel) => {
      const current = panelParams(panel.params)
      return current?.kind === 'file' && current.preview
    })
    const params: FilePanelParams = {
      kind: 'file', root: target.root, path: target.path,
      ...(target.line ? { line: target.line } : {}), preview: true,
      viewMode: isMarkdownPath(target.path) && !target.line ? 'rendered' : 'source',
    }
    addPanel(params, group?.id)
    if (replaced) api.removePanel(replaced)
    queryClient.invalidateQueries({ queryKey: queryKeys.file(target.root, target.path) })
    setQuickOpenGroup(undefined)
    syncDock()
  }, [addPanel, queryClient, quickOpenGroup, syncDock])

  const pinPanel = useCallback((id: string) => {
    const api = apiRef.current
    const current = api ? panelFromAPI(api, id) : null
    if (!current || !current.params.preview) return
    current.panel.api.updateParameters({ ...current.params, preview: false })
    syncDock()
  }, [syncDock])

  const setFileViewMode = useCallback((id: string, viewMode: FileViewMode) => {
    const api = apiRef.current
    const current = api ? panelFromAPI(api, id) : null
    if (current?.params.kind !== 'file' || current.params.viewMode === viewMode) return
    current.panel.api.updateParameters({ ...current.params, viewMode })
    syncDock()
  }, [syncDock])

  const applyRoute = useCallback((route: Exclude<Route, { page: 'missing' }>, push = false) => {
    if (route.page === 'board') openBoard()
    else openAgent(route.name, true)
    const api = apiRef.current
    const id = route.page === 'board' ? 'board' : agentTabID(route.name)
    const current = api ? panelFromAPI(api, id) : null
    if (current) setPathForPanel(current.params, push)
  }, [openAgent, openBoard])

  const onDockReady = useCallback((event: DockviewReadyEvent) => {
    dockDisposables.current.forEach((disposable) => disposable.dispose())
    dockDisposables.current = []
    apiRef.current = event.api
    let restored = false
    if (initial.stored?.dock) {
      restored = restoreDockLayout(event.api, initial.stored.dock)
    }
    if (!restored && initial.legacy) {
      addPanel(boardPanel)
      initial.legacy.openTabs.forEach((name) => openAgent(name, false))
      const active = initial.legacy.activeTab === 'board' ? event.api.getPanel('board') : event.api.getPanel(initial.legacy.activeTab)
      active?.api.setActive()
    }
    if (!restored && !initial.legacy) addPanel(boardPanel)
    applyRoute(initialRoute)
    const onLayout = event.api.onDidLayoutChange(syncDock)
    const onActive = event.api.onDidActivePanelChange(({ panel }) => {
      const params = panelParams(panel?.params)
      if (params) setPathForPanel(params)
      syncDock()
    })
    const onRemove = event.api.onDidRemovePanel((panel) => {
      const params = panelParams(panel.params)
      if (params?.kind !== 'agent') return
      setAgentStatuses((current) => {
        if (!(params.name in current)) return current
        const next = { ...current }; delete next[params.name]; return next
      })
      setAgentScreenPanes((current) => {
        if (!(params.name in current)) return current
        const next = { ...current }; delete next[params.name]; return next
      })
    })
    dockDisposables.current = [onLayout, onActive, onRemove]
    setDockReady(true)
    syncDock()
  }, [addPanel, applyRoute, initial, initialRoute, openAgent, syncDock])

  useEffect(() => () => dockDisposables.current.forEach((disposable) => disposable.dispose()), [])

  useEffect(() => {
    const update = () => {
      const route = currentRoute()
      if (route.page !== 'missing') applyRoute(route)
    }
    window.addEventListener('popstate', update)
    return () => window.removeEventListener('popstate', update)
  }, [applyRoute])

  useEffect(() => {
    if (!dockReady) return
    const timer = window.setTimeout(() => {
      const api = apiRef.current
      if (!api) return
      const value: StoredLayout = { version: 2, dock: persistableDockLayout(api.toJSON()), sidebarWidth }
      if (expandedItems !== null) value.expandedItems = expandedItems
      if (knownWorkspaceItems !== null) value.knownWorkspaceItems = knownWorkspaceItems
      try { localStorage.setItem(layoutStorageKey, JSON.stringify(value)) } catch { /* best effort */ }
    }, 120)
    return () => window.clearTimeout(timer)
  }, [dockReady, expandedItems, knownWorkspaceItems, revision, sidebarWidth])

  const restoredPanels = initial.stored?.dock ? Object.values(initial.stored.dock.panels).flatMap((panel) => {
    const params = panelParams(panel.params)
    return params ? [params] : []
  }) : initial.legacy ? [boardPanel, ...initial.legacy.openTabs.map((name): AgentPanelParams => ({ kind: 'agent', name, preview: false }))] : [boardPanel]
  const openPanels = apiRef.current?.panels.flatMap((panel) => {
    const params = panelParams(panel.params)
    return params ? [params] : []
  }) ?? restoredPanels
  const agentNames = [...new Set(openPanels.flatMap((params) => params.kind === 'agent' ? [params.name] : []))]
  const provenScreenPaneIDs = openPanels.flatMap((params) => params.kind === 'screen' && screenIdentityState(params, boardQuery.data) === 'ready' ? [params.identity.paneID] : [])
  const screenPaneIDs = [...new Set([...provenScreenPaneIDs, ...agentNames.flatMap((name) => agentScreenPanes[name] ? [agentScreenPanes[name]] : [])])]
  const stream = useFleetStream(agentNames, screenPaneIDs)
  const activeParams = openPanels.find((params) => {
    return dockPanelID(params) === activePanelID
  }) ?? boardPanel
  const viewerFailure = viewerQuery.error ? apiProblem(viewerQuery.error) : null
  const viewer = viewerQuery.data?.viewer ?? 'unresolved'
  const viewerState = viewerQuery.isPending ? 'resolving' : viewerQuery.data ? 'attributed' : viewerFailure?.response?.status === 409 ? 'unresolved' : 'unavailable'
  const viewerProblem = viewerState === 'unavailable' ? viewerFailure?.problem.detail ?? '' : ''
  const viewerReadOnly = viewerQuery.isPending ? 'Resolving viewer identity…' : viewerQuery.data ? ''
    : viewerReadOnlyMessage(viewerFailure?.problem ?? { error: 'request failed', detail: 'unknown failure' }, viewerFailure?.response?.status)

  const setLifecycleBanner = useCallback((key: string, detail: string) => setLifecycleProblems((current) => {
    const next = { ...current }
    if (detail) next[key] = detail; else delete next[key]
    return next
  }), [])
  const setAgentStatus = useCallback((name: string, status: string) => setAgentStatuses((current) => current[name] === status ? current : { ...current, [name]: status }), [])
  const setAgentScreenPane = useCallback((name: string, paneID?: string) => setAgentScreenPanes((current) => {
    if (paneID) return current[name] === paneID ? current : { ...current, [name]: paneID }
    if (!(name in current)) return current
    const next = { ...current }; delete next[name]; return next
  }), [])
  const streamProblems = useMemo<Record<string, string>>(() => ({
    ...stream.problems,
    ...(boardQuery.error ? { fleet: boardQuery.error.message } : {}),
    ...lifecycleProblems,
  }), [boardQuery.error, lifecycleProblems, stream.problems])

  const showQuickOpen = useCallback((groupID?: string) => {
    setQuickOpenGroup(groupID ?? apiRef.current?.activeGroup?.id)
    setQuickOpen(true)
  }, [])
  const resetLayout = useCallback(() => {
    const api = apiRef.current
    if (!api) return
    api.clear()
    try { localStorage.removeItem(layoutStorageKey) } catch { /* best effort */ }
    addPanel(boardPanel)
    setSidebarWidth(defaultSidebarWidth)
    syncDock()
  }, [addPanel, syncDock])

  useEffect(() => {
    const shortcut = (event: KeyboardEvent) => {
      const api = apiRef.current
      const command = event.ctrlKey || event.metaKey
      if (isQuickOpenShortcut(event, navigator.userAgent)) showQuickOpen()
      else if (command && event.key.toLowerCase() === 'w' && api?.activePanel) api.activePanel.api.close()
      else if (command && (event.key === 'PageDown' || event.key === 'PageUp') && api?.activeGroup) {
        const panels = api.activeGroup.panels
        const index = panels.findIndex((panel) => panel.id === api.activeGroup?.activePanel?.id)
        panels[(index + (event.key === 'PageDown' ? 1 : -1) + panels.length) % panels.length]?.api.setActive()
      } else if (event.altKey && event.key === '1') document.querySelector<HTMLElement>('.fleet-tree [role="treeitem"]')?.focus()
      else if (event.altKey && event.key === '2') document.querySelector<HTMLTextAreaElement>('.dv-active-group textarea[data-composer]')?.focus()
      else return
      event.preventDefault()
    }
    window.addEventListener('keydown', shortcut)
    return () => window.removeEventListener('keydown', shortcut)
  }, [showQuickOpen])

  const startResize = (event: React.PointerEvent) => {
    const startX = event.clientX
    const startWidth = sidebarWidth
    const move = (moveEvent: PointerEvent) => setSidebarWidth(clampSidebarWidth(startWidth + moveEvent.clientX - startX))
    const stop = () => { window.removeEventListener('pointermove', move); window.removeEventListener('pointerup', stop) }
    window.addEventListener('pointermove', move); window.addEventListener('pointerup', stop)
  }

  const activeAgentStatus = activeParams.kind === 'agent' ? agentBusStatus(boardQuery.data, activeParams.name) : '-'
  const quickOpenAgent = activeParams.kind === 'agent' ? quickOpenAgentPreference(activeParams.name, activeAgentStatus) : undefined
  const workspace = useMemo<WorkspaceContextValue>(() => ({
    board: boardQuery.data, lifecycleBanner: setLifecycleBanner, identityReadOnly: viewerReadOnly, openFile, pinPanel, setFileViewMode,
    agentScreenPanes, setAgentScreenPane, onViewer: (resolvedViewer) => queryClient.setQueryData(queryKeys.viewer, { viewer: resolvedViewer }),
    onAgentStatus: setAgentStatus, agentStatuses, openBoard, resetLayout, showQuickOpen, stream, streamProblems,
  }), [agentScreenPanes, agentStatuses, boardQuery.data, openBoard, openFile, pinPanel, queryClient, resetLayout, setAgentScreenPane, setAgentStatus, setFileViewMode, setLifecycleBanner, showQuickOpen, stream, streamProblems, viewerReadOnly])

  return <WorkspaceContext.Provider value={workspace}><div className="app-shell">
    <QuickOpen open={quickOpen} agent={quickOpenAgent} onClose={() => { setQuickOpen(false); setQuickOpenGroup(undefined) }} onOpenFile={openFile} />
    <div className="sidebar-region" style={{ width: sidebarWidth }}>
      <FleetSidebar board={boardQuery.data} activeAgent={activeParams.kind === 'agent' ? activeParams.name : undefined} activePane={activeParams.kind === 'screen' ? activeParams.pane.pane_id : undefined}
        onPreviewAgent={(name) => openAgent(name, true)} onPinAgent={(name) => openAgent(name, false)} onPreviewPane={(pane) => openScreen(pane, true)} onPinPane={(pane) => openScreen(pane, false)}
        expandedItems={expandedItems} onExpandedItems={setExpandedItems} knownWorkspaceItems={knownWorkspaceItems} onKnownWorkspaceItems={setKnownWorkspaceItems} />
    </div>
    <div className="sidebar-resizer" role="separator" aria-label="Resize fleet sidebar" aria-orientation="vertical" aria-valuemin={200} aria-valuemax={440} aria-valuenow={sidebarWidth} tabIndex={0}
      onPointerDown={startResize} onKeyDown={(event) => { if (event.key === 'ArrowLeft' || event.key === 'ArrowRight') { setSidebarWidth((width) => clampSidebarWidth(width + (event.key === 'ArrowRight' ? 10 : -10))); event.preventDefault() } }} />
    <section className="shell-main">
      <div className="shell-banners">
        {stream.serverUpdated && <div className="banner server-update" role="alert"><strong>update</strong><span>Server updated — refresh to load the new version</span><button type="button" onClick={() => window.location.reload()}>Refresh</button></div>}
        {viewerProblem && <Banner source="viewer" detail={viewerProblem} />}{Object.entries(streamProblems).map(([source, detail]) => <Banner source={source} detail={detail} tone={source === 'stream' && detail === 'Connecting to live fleet…' ? 'info' : 'error'} key={source} />)}
      </div>
      <div className="dock-host">
        <DockviewReact components={dockComponents} tabComponents={{ 'herder-tab': DockTab }} rightHeaderActionsComponent={DockHeaderActions} watermarkComponent={DockWatermark}
          onReady={onDockReady} theme={herderTheme} disableFloatingGroups announcements noPanelsOverlay="watermark" tabGroupAccent="off"
          pinnedTabs={{ enabled: false }} layoutHistory={{ enabled: false }} autoHideEdgeGroups={false} dockToEdgeGroups={false} dndCompass={false}
        />
      </div>
      <footer className="status-bar">
        <span>substrate: herdr {boardQuery.data ? '✓' : '…'} · hcom {streamProblems.hcom ? '×' : '✓'}</span>
        <span className={streamProblems.stream ? 'fault' : ''}>SSE: {streamProblems.stream ? 'reconnecting' : 'connected'}</span>
        <span>viewer: {viewerQuery.isPending ? 'resolving…' : viewer}</span><span>{viewerState === 'resolving' ? 'resolving identity' : viewerState === 'attributed' ? 'attributed' : viewerState === 'unavailable' ? 'identity unavailable' : 'read-only · unattributed'}</span>
        <span className="status-spacer" /><span>{stream.messages} messages</span><span>last event: {stream.lastEvent ? new Date(stream.lastEvent).toLocaleTimeString() : '—'}</span>
      </footer>
    </section>
  </div></WorkspaceContext.Provider>
}

export default function App() {
  const route = currentRoute()
  if (route.page !== 'missing') return <Shell initialRoute={route} />
  return <main className="agent-page"><AppLink to="/" className="back-link">← Fleet board</AppLink><section className="not-found"><strong>404 · Page not found</strong></section></main>
}
