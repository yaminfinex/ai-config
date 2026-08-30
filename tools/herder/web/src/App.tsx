import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import {
  DockviewReact,
  type DockviewApi,
  type DockviewReadyEvent,
  type DockviewTheme,
  type IDockviewHeaderActionsProps,
  type IWatermarkPanelProps,
} from 'dockview-react'
import { apiProblem, getFleet, queryKeys, viewerReadOnlyMessage } from './api/client'
import { viewerQueryOptions } from './api/queries'
import { FleetSidebar } from './features/sidebar/FleetSidebar'
import { AppLink, currentRoute, type Route } from './shared/navigation'
import { agentBusStatus } from './shared/agentStatus'
import { Banner } from './shared/presentation'
import { ThemeToggle } from './shared/ThemeToggle'
import { useFleetStream } from './stream/useFleetStream'
import { createFileWatchRegistry, FileWatchContext, type FileWatchTarget } from './stream/fileWatchRegistry'
import { agentTabID } from './previewTabs'
import { QuickOpen } from './features/files/QuickOpen'
import { usePanelRecords } from './features/workspace/usePanelRecords'
import { subscribeToDock } from './features/workspace/subscribeToDock'
import { WorkspaceContext, useWorkspace, type WorkspaceContextValue } from './features/workspace/workspaceContext'
import { dockComponents, DockTab, panelID } from './features/workspace/panelRegistry'
import { defaultSidebarWidth, panelFromAPI, useWorkspaceActions } from './features/workspace/useWorkspaceActions'
import { PanelState } from './shared/PanelState'
import { subscribeDOMEvent, useDOMEvent } from './shared/lifecycle'
import { quickOpenAgentPreference } from './features/files/fileResolution'
import type { GitFileState } from './features/git/gitViewModel'
import { ShortcutReference } from './features/layout/ShortcutReference'
import { bindShellShortcuts, shortcutLabels } from './features/layout/shellShortcuts'
import { followScrollCommandEvent, type FollowScrollCommand } from './shared/useFollowScroll'
import { layoutRouteState, shouldReplayInitialRoute } from './features/layout/routeReplay'
import { agentMentionMatcher } from './shared/agentMentions'
import {
  legacyLayoutStorageKey,
  panelParams,
  parseLegacyLayout,
  pinMovedPreview,
  persistableDockLayout,
  readStoredLayout,
  restoreDockLayout,
  screenIdentityState,
  writeStoredLayout,
  type AgentPanelParams,
  type DockPanelParams,
  type LegacyLayout,
  type StoredLayout,
} from './features/layout/dockLayout'

const herderTheme: DockviewTheme = {
  name: 'herder', className: 'dockview-theme-herder', gap: 0,
  dndOverlayMounting: 'absolute', dndPanelOverlay: 'group', dndTabIndicator: 'line',
  dndOverlayBorder: '2px solid var(--accent)', tabGroupIndicator: 'none', tabAnimation: 'smooth',
}

type InitialLayout = {
  stored: StoredLayout | null
  backup: StoredLayout | null
  legacy: LegacyLayout | null
  recovering: boolean
  lastGoodRaw: string | null
  sidebarWidth: number
  expandedItems: string[] | null
  knownWorkspaceItems: string[] | null
}

function clampSidebarWidth(width: number) {
  return Math.min(440, Math.max(200, width))
}

function readInitialLayout(): InitialLayout {
  let stored: StoredLayout | null = null
  let backup: StoredLayout | null = null
  let legacy: LegacyLayout | null = null
  let recovering = false
  let lastGoodRaw: string | null = null
  try {
    const layouts = readStoredLayout(localStorage)
    stored = layouts.stored
    backup = layouts.backup
    recovering = layouts.recovering
    lastGoodRaw = layouts.lastGoodRaw
    if (!stored) legacy = parseLegacyLayout(localStorage.getItem(legacyLayoutStorageKey))
  } catch { /* browser storage is best effort */ }
  const source = stored ?? legacy
  return {
    stored, backup, legacy, recovering, lastGoodRaw,
    sidebarWidth: clampSidebarWidth(source?.sidebarWidth ?? defaultSidebarWidth),
    expandedItems: source?.expandedItems ?? null,
    knownWorkspaceItems: source?.knownWorkspaceItems ?? null,
  }
}

function setPathForPanel(params: DockPanelParams | undefined, push = true) {
  if (!push) return
  if (params?.kind === 'agent') {
    const path = `/agents/${encodeURIComponent(params.name)}`
    if (window.location.pathname !== path) {
      window.history.pushState(layoutRouteState, '', path)
      return
    }
  }
  window.history.replaceState(layoutRouteState, '', `${window.location.pathname}${window.location.search}${window.location.hash}`)
}

function DockHeaderActions({ group, containerApi }: IDockviewHeaderActionsProps) {
  const [maximized, setMaximized] = useState(group.api.isMaximized())
  useEffect(() => {
    setMaximized(group.api.isMaximized())
    const disposable = containerApi.onDidMaximizedGroupChange(() => setMaximized(group.api.isMaximized()))
    return () => disposable.dispose()
  }, [containerApi, group])
  return <div className="dock-header-actions">
    <button type="button" className="dock-maximize" title={`${maximized ? 'Restore' : 'Maximize'} group · ${shortcutLabels(navigator.userAgent).toggleMaximize}`}
      aria-label={maximized ? 'Restore group' : 'Maximize group'} onClick={() => maximized ? group.api.exitMaximized() : group.api.maximize()}>
      <span aria-hidden="true">{maximized ? '⧉' : '□'}</span>
    </button>
  </div>
}

function DockWatermark({ containerApi }: IWatermarkPanelProps) {
  const workspace = useWorkspace()
  return <PanelState className="dock-watermark" title="No panels open" detail="Open an agent from the fleet sidebar or find a file or folder. Your sidebar and shortcuts are still available."><div>
    <button type="button" onClick={() => workspace.showQuickOpen(containerApi.activeGroup?.id)}>Quick Open</button>
    <button type="button" onClick={workspace.resetLayout}>Reset layout</button>
  </div></PanelState>
}

function sameGitFileState(left: GitFileState, right: GitFileState) {
  return left.mode === right.mode && left.base === right.base &&
    left.revision?.sha === right.revision?.sha && left.revision?.path === right.revision?.path &&
    left.commit?.sha === right.commit?.sha && left.commit?.path === right.commit?.path
}

function Shell({ initialRoute }: { initialRoute: Exclude<Route, { page: 'missing' }> }) {
  const [initial] = useState(readInitialLayout)
  const [quickOpen, setQuickOpen] = useState(false)
  const [shortcutReference, setShortcutReference] = useState(false)
  const [quickOpenGroup, setQuickOpenGroup] = useState<string>()
  const [sidebarWidth, setSidebarWidth] = useState(initial.sidebarWidth)
  const [expandedItems, setExpandedItems] = useState<string[] | null>(initial.expandedItems)
  const [knownWorkspaceItems, setKnownWorkspaceItems] = useState<string[] | null>(initial.knownWorkspaceItems)
  const { records: agentStatuses, set: setAgentStatusRecord, prune: pruneAgentStatus } = usePanelRecords<string>()
  const { records: agentScreenPanes, set: setAgentScreenPaneRecord, prune: pruneAgentScreenPane } = usePanelRecords<string>()
  const [focusedScreenPaneID, setFocusedScreenPaneID] = useState<string>()
  const { records: fileGitStates, set: setFileGitStateRecord, prune: pruneFileGitState } = usePanelRecords<GitFileState>(sameGitFileState)
  const [fileWatchTargets, setFileWatchTargets] = useState<FileWatchTarget[]>([])
  const [activePanelID, setActivePanelID] = useState('')
  const [revision, setRevision] = useState(0)
  const [dockReady, setDockReady] = useState(false)
  const apiRef = useRef<DockviewApi | undefined>(undefined)
  const disposeDock = useRef<() => void>(() => undefined)
  const persistenceReady = useRef(false)
  const layoutDirty = useRef(false)
  const persistenceState = useRef({ recovering: initial.recovering, lastGoodRaw: initial.lastGoodRaw })
  const preferenceSnapshot = useRef(JSON.stringify([initial.sidebarWidth, initial.expandedItems, initial.knownWorkspaceItems]))
  const queryClient = useQueryClient()
  const boardQuery = useQuery({ queryKey: queryKeys.fleet, queryFn: () => getFleet(), staleTime: Infinity, retry: false })
  const viewerQuery = useQuery(viewerQueryOptions())
  const mentionMatcher = useMemo(() => agentMentionMatcher(boardQuery.data), [boardQuery.data])
  const fileWatchRegistry = useMemo(() => createFileWatchRegistry(setFileWatchTargets), [])

  useEffect(() => () => fileWatchRegistry.dispose(), [fileWatchRegistry])

  const syncDock = useCallback(() => {
    if (persistenceReady.current) layoutDirty.current = true
    setActivePanelID(apiRef.current?.activePanel?.id ?? '')
    setRevision((value) => value + 1)
  }, [])

  const { openAgent, openScreen, openFile, openFileInDiff, openChanges, openFolder, pinPanel, setFileViewMode, resetLayout } = useWorkspaceActions({
    apiRef,
    board: boardQuery.data,
    queryClient,
    quickOpenGroup,
    setQuickOpenGroup,
    syncDock,
    setFileGitState: setFileGitStateRecord,
    setSidebarWidth,
    persistenceState,
  })

  const applyRoute = useCallback((route: Exclude<Route, { page: 'missing' }>, push = false) => {
    if (route.page === 'shell') return
    openAgent(route.name, true)
    const api = apiRef.current
    const id = agentTabID(route.name)
    const current = api ? panelFromAPI(api, id) : null
    if (current) setPathForPanel(current.params, push)
  }, [openAgent])

  const onDockReady = useCallback((event: DockviewReadyEvent) => {
    disposeDock.current()
    apiRef.current = event.api
    let restored = false
    let restoreFailed = false
    let restoredLegacy = false
    if (initial.stored?.dock) {
      restored = restoreDockLayout(event.api, initial.stored.dock)
      restoreFailed = !restored
    }
    if (!restored && initial.backup?.dock && initial.backup !== initial.stored) {
      restored = restoreDockLayout(event.api, initial.backup.dock)
      restoreFailed = !restored
      if (restored) persistenceState.current.recovering = true
    }
    if (!restored && initial.legacy) {
      initial.legacy.openTabs.forEach((name) => openAgent(name, false))
      const active = initial.legacy.activeTab === 'board' ? event.api.panels[0] : event.api.getPanel(initial.legacy.activeTab)
      active?.api.setActive()
      restoredLegacy = true
    }
    const replayedRoute = shouldReplayInitialRoute(initialRoute, window.history.state, restored)
    if (replayedRoute) applyRoute(initialRoute)
    disposeDock.current = subscribeToDock(event.api, {
      layout: () => syncDock(),
      activePanel: ({ panel }) => {
        const params = panelParams(panel?.params)
        setPathForPanel(params ?? undefined)
        syncDock()
      },
      removePanel: (panel) => {
        const params = panelParams(panel.params)
        if (params?.kind === 'file') pruneFileGitState(panel.id)
        if (params?.kind !== 'agent') return
        pruneAgentStatus(params.name)
        pruneAgentScreenPane(params.name)
      },
      movePanel: ({ panel }) => { if (pinMovedPreview(panel)) syncDock() },
    })
    persistenceReady.current = true
    if ((replayedRoute && !restoreFailed) || restoredLegacy) layoutDirty.current = true
    setDockReady(true)
    setActivePanelID(event.api.activePanel?.id ?? '')
    setRevision((value) => value + 1)
  }, [applyRoute, initial, initialRoute, openAgent, pruneAgentScreenPane, pruneAgentStatus, pruneFileGitState, syncDock])

  useEffect(() => () => disposeDock.current(), [])

  useDOMEvent(window, 'popstate', () => {
    const update = () => {
      const route = currentRoute()
      if (route.page !== 'missing') applyRoute(route)
    }
    update()
  })

  const flushLayout = useCallback(() => {
    if (!layoutDirty.current) return false
    const api = apiRef.current
    if (!api) return false
    const dock = persistableDockLayout(api.toJSON())
    if (!dock && api.panels.length > 0) return false
    const value: StoredLayout = { version: 2, dock, sidebarWidth }
    if (expandedItems !== null) value.expandedItems = expandedItems
    if (knownWorkspaceItems !== null) value.knownWorkspaceItems = knownWorkspaceItems
    const previous = persistenceState.current
    const next = writeStoredLayout(localStorage, JSON.stringify(value), previous)
    if (next === previous) return false
    persistenceState.current = next
    layoutDirty.current = false
    return true
  }, [expandedItems, knownWorkspaceItems, sidebarWidth])

  useEffect(() => {
    if (!dockReady) return
    const nextPreferences = JSON.stringify([sidebarWidth, expandedItems, knownWorkspaceItems])
    if (preferenceSnapshot.current !== nextPreferences) {
      preferenceSnapshot.current = nextPreferences
      layoutDirty.current = true
    }
    if (!layoutDirty.current) return
    const timer = window.setTimeout(flushLayout, 120)
    return () => window.clearTimeout(timer)
  }, [dockReady, expandedItems, flushLayout, knownWorkspaceItems, revision, sidebarWidth])

  useDOMEvent(window, 'pagehide', () => { flushLayout() })

  const restoredPanels = initial.stored?.dock ? Object.values(initial.stored.dock.panels).flatMap((panel) => {
    const params = panelParams(panel.params)
    return params ? [params] : []
  }) : initial.legacy ? initial.legacy.openTabs.map((name): AgentPanelParams => ({ kind: 'agent', name, preview: false })) : []
  const openPanels = apiRef.current?.panels.flatMap((panel) => {
    const params = panelParams(panel.params)
    return params ? [params] : []
  }) ?? restoredPanels
  const agentNames = [...new Set(openPanels.flatMap((params) => params.kind === 'agent' ? [params.name] : []))]
  const provenScreenPaneIDs = openPanels.flatMap((params) => params.kind === 'screen' && screenIdentityState(params, boardQuery.data) === 'ready' ? [params.identity.paneID] : [])
  const screenPaneIDs = [...new Set([...provenScreenPaneIDs, ...agentNames.flatMap((name) => agentScreenPanes[name] ? [agentScreenPanes[name]] : [])])]
  const effectiveFocusedScreenPaneID = focusedScreenPaneID && screenPaneIDs.includes(focusedScreenPaneID) ? focusedScreenPaneID : undefined
  const stream = useFleetStream(agentNames, screenPaneIDs, fileWatchTargets, effectiveFocusedScreenPaneID)
  const activeParams = openPanels.find((params) => {
    return panelID(params) === activePanelID
  })
  const viewerFailure = viewerQuery.error ? apiProblem(viewerQuery.error) : null
  const viewer = viewerQuery.data?.viewer ?? 'unresolved'
  const viewerState = viewerQuery.isPending ? 'resolving' : viewerQuery.data ? 'attributed' : viewerFailure?.response?.status === 409 ? 'unresolved' : 'unavailable'
  const viewerProblem = viewerState === 'unavailable' ? viewerFailure?.problem.detail ?? '' : ''
  const viewerReadOnly = viewerQuery.isPending ? 'Resolving viewer identity…' : viewerQuery.data ? ''
    : viewerReadOnlyMessage(viewerFailure?.problem ?? { error: 'request failed', detail: 'unknown failure' }, viewerFailure?.response?.status)

  const setAgentStatus = useCallback((name: string, status: string) => setAgentStatusRecord(name, status), [setAgentStatusRecord])
  const setFileGitState = useCallback((id: string, state: GitFileState) => setFileGitStateRecord(id, state), [setFileGitStateRecord])
  const setAgentScreenPane = useCallback((name: string, paneID?: string) => setAgentScreenPaneRecord(name, paneID), [setAgentScreenPaneRecord])
  const streamProblems = useMemo<Record<string, string>>(() => ({
    ...stream.problems,
    ...(boardQuery.error ? { fleet: boardQuery.error.message } : {}),
  }), [boardQuery.error, stream.problems])

  const showQuickOpen = useCallback((groupID?: string) => {
    setQuickOpenGroup(groupID ?? apiRef.current?.activeGroup?.id)
    setQuickOpen(true)
  }, [])
  useEffect(() => {
    const scrollActivePanel = (command: FollowScrollCommand) => {
      const viewport = document.querySelector('.dv-active-group [data-follow-scroll]')
      if (!viewport) return false
      viewport.dispatchEvent(new CustomEvent(followScrollCommandEvent, { detail: command }))
      return true
    }
    const switchTab = (direction: 'previous' | 'next') => {
      const api = apiRef.current
      if (!api?.activeGroup || api.activeGroup.panels.length === 0) return false
      const panels = api.activeGroup.panels
      const index = panels.findIndex((panel) => panel.id === api.activeGroup?.activePanel?.id)
      panels[(index + (direction === 'next' ? 1 : -1) + panels.length) % panels.length]?.api.setActive()
      return true
    }
    return bindShellShortcuts(window, {
      quickOpen: () => showQuickOpen(),
      closePanel: () => {
        const panel = apiRef.current?.activePanel
        if (!panel) return false
        panel.api.close()
        return true
      },
      openShortcutReference: () => setShortcutReference(true),
      closeShortcutReference: () => {
        if (!shortcutReference) return false
        setShortcutReference(false)
        return true
      },
      switchTab,
      focusFleet: () => {
        const item = document.querySelector<HTMLElement>('.fleet-tree [role="treeitem"]')
        if (!item) return false
        item.focus()
        return true
      },
      focusComposer: () => {
        const composer = document.querySelector<HTMLTextAreaElement>('.dv-active-group textarea[data-composer]')
        if (!composer) return false
        composer.focus()
        return true
      },
      goToTop: () => scrollActivePanel('top'),
      goToBottom: () => scrollActivePanel('bottom'),
      toggleMaximize: () => {
        const group = apiRef.current?.activeGroup
        if (!group) return false
        if (group.api.isMaximized()) group.api.exitMaximized()
        else group.api.maximize()
        return true
      },
    }, navigator.userAgent)
  }, [shortcutReference, showQuickOpen])

  const startResize = (event: React.PointerEvent) => {
    const startX = event.clientX
    const startWidth = sidebarWidth
    const move = (moveEvent: PointerEvent) => setSidebarWidth(clampSidebarWidth(startWidth + moveEvent.clientX - startX))
    let disposeUp: () => void = () => undefined
    const disposeMove = subscribeDOMEvent<PointerEvent>(window, 'pointermove', move)
    const stop = () => { disposeMove(); disposeUp() }
    disposeUp = subscribeDOMEvent(window, 'pointerup', stop)
  }

  const activeAgentStatus = activeParams?.kind === 'agent' ? agentBusStatus(boardQuery.data, activeParams.name) : '-'
  const quickOpenAgent = activeParams?.kind === 'agent' ? quickOpenAgentPreference(activeParams.name, activeAgentStatus) : undefined
  const workspace = useMemo<WorkspaceContextValue>(() => ({
    board: boardQuery.data, mentionMatcher, identityReadOnly: viewerReadOnly, openAgent, openFile, openFileInDiff, openChanges, openFolder, pinPanel, setFileViewMode, fileGitStates, setFileGitState,
    agentScreenPanes, setAgentScreenPane, onTerminalFocus: setFocusedScreenPaneID, onViewer: (resolvedViewer) => queryClient.setQueryData(queryKeys.viewer, { viewer: resolvedViewer }),
    onAgentStatus: setAgentStatus, agentStatuses, resetLayout, showQuickOpen, stream, streamProblems,
  }), [agentScreenPanes, agentStatuses, boardQuery.data, fileGitStates, mentionMatcher, openAgent, openChanges, openFile, openFileInDiff, openFolder, pinPanel, queryClient, resetLayout, setAgentScreenPane, setAgentStatus, setFileGitState, setFileViewMode, showQuickOpen, stream, streamProblems, viewerReadOnly])

  return <WorkspaceContext.Provider value={workspace}><FileWatchContext.Provider value={fileWatchRegistry.register}><div className="app-shell">
    <QuickOpen open={quickOpen} agent={quickOpenAgent} groupID={quickOpenGroup} onClose={() => { setQuickOpen(false); setQuickOpenGroup(undefined) }} onOpenFile={openFile} onOpenFolder={openFolder} />
    <ShortcutReference open={shortcutReference} onClose={() => setShortcutReference(false)} />
    <div className="sidebar-region" style={{ width: sidebarWidth }}>
      <FleetSidebar board={boardQuery.data} activeAgent={activeParams?.kind === 'agent' ? activeParams.name : undefined} activePane={activeParams?.kind === 'screen' ? activeParams.pane.pane_id : undefined}
        onPreviewAgent={(name, placement) => openAgent(name, true, placement, true)} onPinAgent={(name, placement) => openAgent(name, false, placement, true)} onPreviewPane={(pane, placement) => openScreen(pane, true, placement)} onPinPane={(pane, placement) => openScreen(pane, false, placement)}
        expandedItems={expandedItems} onExpandedItems={setExpandedItems} knownWorkspaceItems={knownWorkspaceItems} onKnownWorkspaceItems={setKnownWorkspaceItems} />
    </div>
    <div className="sidebar-resizer" role="separator" aria-label="Resize fleet sidebar" aria-orientation="vertical" aria-valuemin={200} aria-valuemax={440} aria-valuenow={sidebarWidth} tabIndex={0}
      onPointerDown={startResize} onKeyDown={(event) => { if (event.key === 'ArrowLeft' || event.key === 'ArrowRight') { setSidebarWidth((width) => clampSidebarWidth(width + (event.key === 'ArrowRight' ? 10 : -10))); event.preventDefault() } }} />
    <section className="shell-main">
      <div className="shell-banners">
        {stream.serverUpdated && <div className="banner server-update" role="alert"><strong>update</strong><span>Server updated — refresh to load the new version</span><button type="button" onClick={() => { flushLayout(); window.location.reload() }}>Refresh</button></div>}
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
        <span title="Dock layout is stored in this browser">layout: this browser</span>
        <span title="Web sends are attributed to this viewer; web senders are not addressable bus peers.">viewer: {viewerQuery.isPending ? 'resolving…' : viewer}</span><span>{viewerState === 'resolving' ? 'resolving identity' : viewerState === 'attributed' ? 'attributed' : viewerState === 'unavailable' ? 'identity unavailable' : 'read-only · unattributed'}</span>
        <span className="status-spacer" /><span>{stream.messages} messages</span><span>last event: {stream.lastEvent ? new Date(stream.lastEvent).toLocaleTimeString() : '—'}</span>
        <button type="button" className="shortcut-button" title="Keyboard shortcuts (?)" aria-label="Open keyboard shortcuts" onClick={() => setShortcutReference(true)}>?</button>
        <ThemeToggle />
      </footer>
    </section>
  </div></FileWatchContext.Provider></WorkspaceContext.Provider>
}

export default function App() {
  const route = currentRoute()
  if (route.page !== 'missing') return <Shell initialRoute={route} />
  return <main className="agent-page"><AppLink to="/" className="back-link">← Workspace</AppLink><section className="not-found"><strong>404 · Page not found</strong></section></main>
}
