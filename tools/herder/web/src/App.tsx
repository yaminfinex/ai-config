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
import { FleetSidebar } from './features/sidebar/FleetSidebar'
import { AgentPanel } from './features/transcript/AgentPanel'
import { ScreenPanel } from './features/screen/ScreenPanel'
import { AppLink, currentRoute, type Route } from './shared/navigation'
import { agentBusStatus } from './shared/agentStatus'
import { AgentStatusDot, Banner } from './shared/presentation'
import { ThemeToggle } from './shared/ThemeToggle'
import { useFleetStream, type StreamState } from './stream/useFleetStream'
import { agentTabID } from './previewTabs'
import type { Board, FileTarget, FolderTarget, Pane } from './types'
import { FilePanel } from './features/files/FilePanel'
import { QuickOpen } from './features/files/QuickOpen'
import { fileTabID, isMarkdownPath, type FileViewMode } from './features/files/fileTabs'
import { quickOpenAgentPreference, rootLabel } from './features/files/fileResolution'
import { FolderPanel } from './features/folders/FolderPanel'
import { folderTabID } from './features/folders/folderModel'
import { gitStateForFileOpen, initialGitFileState, type GitBase, type GitFileState } from './features/git/gitViewModel'
import { changesPanelID } from './features/git/changesModel'
import { ChangesPanel } from './features/git/ChangesPanel'
import { ShortcutReference } from './features/layout/ShortcutReference'
import { bindShellShortcuts, shortcutLabels } from './features/layout/shellShortcuts'
import { followScrollCommandEvent, type FollowScrollCommand } from './shared/useFollowScroll'
import { dockOpenTarget, placementInGroup, type OpenPlacement } from './features/layout/openPlacement'
import { layoutRouteState, shouldReplayInitialRoute } from './features/layout/routeReplay'
import { agentMentionMatcher, type AgentMentionMatcher } from './shared/agentMentions'
import {
  layoutStorageBackupKey,
  layoutStorageKey,
  legacyLayoutStorageKey,
  panelParams,
  parseLegacyLayout,
  pinMovedPreview,
  persistableDockLayout,
  readStoredLayout,
  restoreDockLayout,
  screenIdentityState,
  screenPanelParams,
  writeStoredLayout,
  type AgentPanelParams,
  type ChangesPanelParams,
  type DockPanelParams,
  type FilePanelParams,
  type FolderPanelParams,
  type LegacyLayout,
  type ScreenPanelParams,
  type StoredLayout,
} from './features/layout/dockLayout'

const defaultSidebarWidth = 250
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

function dockGroupFacts(api: DockviewApi) {
  const active = api.activeGroup
  return {
    activeGroupID: active?.id,
    firstGroupID: api.groups[0]?.id,
    rightGroupID: active ? api.adjacentGroupInDirection(active, 'right')?.id : undefined,
    leftGroupID: active ? api.adjacentGroupInDirection(active, 'left')?.id : undefined,
    fallbackGroupID: api.groups.find((group) => group.id !== active?.id)?.id,
    groupCount: api.groups.length,
  }
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

function screenTabID(paneID: string) { return `screen:${paneID}` }

function dockPanelID(params: DockPanelParams) {
  if (params.kind === 'agent') return agentTabID(params.name)
  if (params.kind === 'screen') return screenTabID(params.pane.pane_id)
  if (params.kind === 'folder') return folderTabID(params.root, params.path)
  if (params.kind === 'changes') return changesPanelID(params.root)
  return fileTabID(params.root, params.path)
}

function panelTitle(params: DockPanelParams) {
  if (params.kind === 'agent') return params.name
  if (params.kind === 'screen') return params.pane.label || params.pane.pane_id
  if (params.kind === 'folder') return rootLabel(params.path) || rootLabel(params.root)
  if (params.kind === 'changes') return `Changes · ${rootLabel(params.root)}`
  return rootLabel(params.path)
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
  mentionMatcher: AgentMentionMatcher
  identityReadOnly: string
  openAgent: (name: string, preview: boolean, placement?: OpenPlacement) => void
  openFile: (target: FileTarget, placement?: OpenPlacement) => void
  openFileInDiff: (target: FileTarget, base: GitBase, placement?: OpenPlacement) => void
  openChanges: (root: string, placement?: OpenPlacement) => void
  openFolder: (target: FolderTarget, placement?: OpenPlacement) => void
  pinPanel: (id: string) => void
  setFileViewMode: (id: string, mode: FileViewMode) => void
  fileGitStates: Record<string, GitFileState>
  setFileGitState: (id: string, state: GitFileState) => void
  agentScreenPanes: Record<string, string>
  setAgentScreenPane: (name: string, paneID?: string) => void
  onViewer: (viewer: string) => void
  onAgentStatus: (name: string, status: string) => void
  agentStatuses: Record<string, string>
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

function AgentDockPanel({ params, api }: IDockviewPanelProps<AgentPanelParams>) {
  const workspace = useWorkspace()
  const visible = usePanelVisibility(api)
  return <AgentPanel name={params.name} active={visible} liveStatus={agentBusStatus(workspace.board, params.name)} screenPaneID={workspace.agentScreenPanes[params.name]}
    mentionMatcher={workspace.mentionMatcher} onOpenAgent={(name, placement) => workspace.openAgent(name, true, placementInGroup(placement, api.group.id))}
    onScreenPane={(paneID) => workspace.setAgentScreenPane(params.name, paneID)} onOpenFile={(target, placement) => workspace.openFile(target, placementInGroup(placement, api.group.id))}
    onOpenFolder={(target, placement) => workspace.openFolder(target, placementInGroup(placement, api.group.id))}
    onOpenChanges={(root, placement) => workspace.openChanges(root, placementInGroup(placement, api.group.id))}
    identityReadOnly={workspace.identityReadOnly} onViewer={workspace.onViewer} onSend={() => workspace.pinPanel(api.id)} onStatus={workspace.onAgentStatus} />
}

function ScreenDockPanel({ params, api }: IDockviewPanelProps<ScreenPanelParams>) {
  const workspace = useWorkspace()
  const visible = usePanelVisibility(api)
  const identity = screenIdentityState(params, workspace.board)
  if (identity === 'checking') return <main className="panel-unavailable" role="status"><strong>Verifying screen identity…</strong><p>The live fleet must confirm this saved pane before it can be subscribed.</p></main>
  const pane = visiblePane(workspace.board, params)
  if (!pane) return <main className="panel-unavailable tombstone" role="status"><strong>Screen no longer matches</strong><p>The saved pane identity is gone or now belongs to different live evidence. No replacement pane was opened.</p></main>
  return <ScreenPanel pane={pane} active={visible} />
}

function FileDockPanel({ params, api }: IDockviewPanelProps<FilePanelParams>) {
  const workspace = useWorkspace()
  const visible = usePanelVisibility(api)
  return <FilePanel target={{ root: params.root, path: params.path, ...(params.line ? { line: params.line } : {}) }} viewMode={params.viewMode}
    gitState={workspace.fileGitStates[api.id] ?? initialGitFileState()} active={visible}
    onViewMode={(mode) => workspace.setFileViewMode(api.id, mode)} onGitState={(state) => workspace.setFileGitState(api.id, state)}
    onOpenFile={(target, placement) => workspace.openFile(target, placementInGroup(placement, api.group.id))}
    onOpenFolder={(target, placement) => workspace.openFolder(target, placementInGroup(placement, api.group.id))} />
}

function FolderDockPanel({ params, api }: IDockviewPanelProps<FolderPanelParams>) {
  const workspace = useWorkspace()
  const visible = usePanelVisibility(api)
  return <FolderPanel target={{ root: params.root, path: params.path }} active={visible}
    onOpenFile={(target, placement) => workspace.openFile(target, placementInGroup(placement, api.group.id))}
    onOpenFolder={(target, placement) => workspace.openFolder(target, placementInGroup(placement, api.group.id))} />
}

function ChangesDockPanel({ params, api }: IDockviewPanelProps<ChangesPanelParams>) {
  const workspace = useWorkspace()
  const visible = usePanelVisibility(api)
  return <ChangesPanel root={params.root} active={visible} onOpenDiff={(target, base, placement) => workspace.openFileInDiff(target, base, placementInGroup(placement, api.group.id))} />
}

function DockTab({ params, api }: IDockviewPanelHeaderProps<DockPanelParams>) {
  const workspace = useWorkspace()
  const boardStatus = params.kind === 'agent' ? agentBusStatus(workspace.board, params.name) : '-'
  const status = params.kind === 'agent' && boardStatus === '-' ? workspace.agentStatuses[params.name] ?? '-' : boardStatus
  const meta = params.kind === 'agent' ? status !== '-' ? status : 'unknown' : params.kind === 'screen' ? 'read-only' : params.kind === 'file' ? 'file · read-only' : params.kind === 'folder' ? 'folder · read-only' : params.kind === 'changes' ? 'git · read-only' : ''
  return <div className={`herder-dock-tab${params.preview ? ' preview' : ''}`} title={params.preview ? 'Preview — double-click to pin' : undefined}
    onDoubleClick={(event) => { if (params.preview) workspace.pinPanel(api.id); event.stopPropagation() }}
    onAuxClick={(event) => { if (event.button === 1) api.close() }}>
    <span className="dock-tab-label">{params.preview && <span className="preview-dot" aria-hidden="true" />}{params.kind === 'screen' ? '▣ ' : params.kind === 'file' ? '◇ ' : params.kind === 'folder' ? '▰ ' : params.kind === 'changes' ? '± ' : ''}{panelTitle(params)}</span>
    {meta && <span className="dock-tab-meta">{params.kind === 'agent' && <AgentStatusDot status={status} />}{meta}</span>}
    <button type="button" className="dock-tab-close" aria-label={`Close ${panelTitle(params)}`} onPointerDown={(event) => event.stopPropagation()} onClick={() => api.close()}>×</button>
  </div>
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
  return <section className="dock-watermark" role="status"><strong>No panels open</strong><p>Open an agent from the fleet sidebar or find a file or folder. Your sidebar and shortcuts are still available.</p><div>
    <button type="button" onClick={() => workspace.showQuickOpen(containerApi.activeGroup?.id)}>Quick Open</button>
    <button type="button" onClick={workspace.resetLayout}>Reset layout</button>
  </div></section>
}

const dockComponents = { agent: AgentDockPanel, screen: ScreenDockPanel, file: FileDockPanel, folder: FolderDockPanel, changes: ChangesDockPanel }

function Shell({ initialRoute }: { initialRoute: Exclude<Route, { page: 'missing' }> }) {
  const [initial] = useState(readInitialLayout)
  const [quickOpen, setQuickOpen] = useState(false)
  const [shortcutReference, setShortcutReference] = useState(false)
  const [quickOpenGroup, setQuickOpenGroup] = useState<string>()
  const [sidebarWidth, setSidebarWidth] = useState(initial.sidebarWidth)
  const [expandedItems, setExpandedItems] = useState<string[] | null>(initial.expandedItems)
  const [knownWorkspaceItems, setKnownWorkspaceItems] = useState<string[] | null>(initial.knownWorkspaceItems)
  const [agentStatuses, setAgentStatuses] = useState<Record<string, string>>({})
  const [agentScreenPanes, setAgentScreenPanes] = useState<Record<string, string>>({})
  const [fileGitStates, setFileGitStates] = useState<Record<string, GitFileState>>({})
  const [activePanelID, setActivePanelID] = useState('')
  const [revision, setRevision] = useState(0)
  const [dockReady, setDockReady] = useState(false)
  const apiRef = useRef<DockviewApi | undefined>(undefined)
  const dockDisposables = useRef<Array<{ dispose: () => void }>>([])
  const persistenceReady = useRef(false)
  const layoutDirty = useRef(false)
  const persistenceState = useRef({ recovering: initial.recovering, lastGoodRaw: initial.lastGoodRaw })
  const preferenceSnapshot = useRef(JSON.stringify([initial.sidebarWidth, initial.expandedItems, initial.knownWorkspaceItems]))
  const queryClient = useQueryClient()
  const boardQuery = useQuery({ queryKey: queryKeys.fleet, queryFn: () => getFleet(), staleTime: Infinity, retry: false })
  const viewerQuery = useQuery(viewerQueryOptions())
  const mentionMatcher = useMemo(() => agentMentionMatcher(boardQuery.data), [boardQuery.data])

  const syncDock = useCallback(() => {
    if (persistenceReady.current) layoutDirty.current = true
    setActivePanelID(apiRef.current?.activePanel?.id ?? '')
    setRevision((value) => value + 1)
  }, [])

  const addPanel = useCallback((params: DockPanelParams, position?: { referenceGroup: string, direction: 'within' | 'right' }) => {
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
    const added = api.addPanel({
      id, component: params.kind, tabComponent: 'herder-tab', title: panelTitle(params), params,
      ...(position ? { position } : {}),
    })
    syncDock()
    return added
  }, [syncDock])

  const openAgent = useCallback((name: string, preview: boolean, placement?: OpenPlacement) => {
    const api = apiRef.current
    if (!api) return
    const target = dockOpenTarget(api.getPanel(agentTabID(name)), placement, dockGroupFacts(api))
    if (target.kind === 'existing') {
      const existing = target.panel
      const current = panelParams(existing.params)
      existing.api.updateParameters(current?.kind === 'agent' ? { ...current, preview: current.preview && preview } : { kind: 'agent', name, preview })
      existing.api.setActive()
      syncDock()
      return
    }
    const group = target.groupID ? api.getGroup(target.groupID) : undefined
    const replaced = preview ? group?.panels.find((panel) => {
      const current = panelParams(panel.params)
      return current?.kind === 'agent' && current.preview
    }) : undefined
    addPanel({ kind: 'agent', name, preview }, target.position)
    if (replaced) api.removePanel(replaced)
    syncDock()
  }, [addPanel, syncDock])

  const openScreen = useCallback((pane: Pane, preview: boolean, placement?: OpenPlacement) => {
    const api = apiRef.current
    if (!boardQuery.data) return
    const params = screenPanelParams(boardQuery.data, pane, preview)
    if (!params) return
    if (!api) return
    const target = dockOpenTarget(api.getPanel(screenTabID(pane.pane_id)), placement, dockGroupFacts(api))
    if (target.kind === 'existing') {
      const existing = target.panel
      const current = panelParams(existing.params)
      existing.api.updateParameters(current?.kind === 'screen' ? { ...params, preview: current.preview && preview } : params)
      existing.api.setActive()
      syncDock()
      return
    }
    const group = target.groupID ? api.getGroup(target.groupID) : undefined
    const replaced = preview ? group?.panels.find((panel) => {
      const current = panelParams(panel.params)
      return current?.kind === 'screen' && current.preview
    }) : undefined
    addPanel(params, target.position)
    if (replaced) api.removePanel(replaced)
    syncDock()
  }, [addPanel, boardQuery.data, syncDock])

  const openFile = useCallback((target: FileTarget, placement?: OpenPlacement) => {
    const api = apiRef.current
    if (!api) return
    const id = fileTabID(target.root, target.path)
    const dockTarget = dockOpenTarget(panelFromAPI(api, id) ?? undefined, placement, dockGroupFacts(api))
    if (dockTarget.kind === 'existing') {
      const existing = dockTarget.panel
      if (existing.params.kind !== 'file') return
      const params: FilePanelParams = { ...existing.params, ...target, viewMode: target.line ? 'source' : existing.params.viewMode }
      existing.panel.api.updateParameters(params)
      existing.panel.api.setActive()
      if (target.line) setFileGitStates((current) => ({ ...current, [id]: gitStateForFileOpen(current[id], target.line) }))
      queryClient.invalidateQueries({ queryKey: queryKeys.file(target.root, target.path) })
      syncDock()
      return
    }
    const requestedPlacement = placement ?? (quickOpenGroup ? { direction: 'within' as const, groupID: quickOpenGroup } : undefined)
    const newTarget = dockOpenTarget(undefined, requestedPlacement, dockGroupFacts(api))
    const group = newTarget.groupID ? api.getGroup(newTarget.groupID) : undefined
    const replaced = group?.panels.find((panel) => {
      const current = panelParams(panel.params)
      return current?.kind === 'file' && current.preview
    })
    const params: FilePanelParams = {
      kind: 'file', root: target.root, path: target.path,
      ...(target.line ? { line: target.line } : {}), preview: true,
      viewMode: isMarkdownPath(target.path) && !target.line ? 'rendered' : 'source',
    }
    addPanel(params, newTarget.position)
    if (replaced) api.removePanel(replaced)
    queryClient.invalidateQueries({ queryKey: queryKeys.file(target.root, target.path) })
    setQuickOpenGroup(undefined)
    syncDock()
  }, [addPanel, queryClient, quickOpenGroup, syncDock])

  const openFileInDiff = useCallback((target: FileTarget, base: GitBase, placement?: OpenPlacement) => {
    openFile(target, placement)
    const id = fileTabID(target.root, target.path)
    setFileGitStates((current) => ({ ...current, [id]: { mode: 'diff', base } }))
  }, [openFile])

  const openChanges = useCallback((root: string, placement?: OpenPlacement) => {
    const api = apiRef.current
    if (!api) return
    const id = changesPanelID(root)
    const dockTarget = dockOpenTarget(panelFromAPI(api, id) ?? undefined, placement, dockGroupFacts(api))
    if (dockTarget.kind === 'existing' && dockTarget.panel.params.kind === 'changes') {
      const existing = dockTarget.panel
      existing.panel.api.setActive()
      queryClient.invalidateQueries({ queryKey: queryKeys.gitStatus(root) })
      syncDock()
      return
    }
    const requestedGroup = dockTarget.kind === 'new' && dockTarget.groupID ? api.getGroup(dockTarget.groupID) : undefined
    const replaced = requestedGroup?.panels.find((panel) => {
      const params = panelParams(panel.params)
      return params?.kind === 'changes' && params.preview
    })
    addPanel({ kind: 'changes', root, preview: true }, dockTarget.kind === 'new' ? dockTarget.position : undefined)
    if (replaced) api.removePanel(replaced)
    queryClient.invalidateQueries({ queryKey: queryKeys.gitStatus(root) })
  }, [addPanel, queryClient, syncDock])

  const openFolder = useCallback((target: FolderTarget, placement?: OpenPlacement) => {
    const api = apiRef.current
    if (!api) return
    const id = folderTabID(target.root, target.path)
    const dockTarget = dockOpenTarget(panelFromAPI(api, id) ?? undefined, placement, dockGroupFacts(api))
    if (dockTarget.kind === 'existing' && dockTarget.panel.params.kind === 'folder') {
      const existing = dockTarget.panel
      existing.panel.api.setActive()
      queryClient.invalidateQueries({ queryKey: queryKeys.fileTree(target.root, target.path) })
      queryClient.invalidateQueries({ queryKey: queryKeys.backlog(target.root, target.path) })
      syncDock()
      return
    }
    const requestedPlacement = placement ?? (quickOpenGroup ? { direction: 'within' as const, groupID: quickOpenGroup } : undefined)
    const newTarget = dockOpenTarget(undefined, requestedPlacement, dockGroupFacts(api))
    const group = newTarget.groupID ? api.getGroup(newTarget.groupID) : undefined
    const replaced = group?.panels.find((panel) => {
      const current = panelParams(panel.params)
      return current?.kind === 'folder' && current.preview
    })
    addPanel({ kind: 'folder', root: target.root, path: target.path, preview: true }, newTarget.position)
    if (replaced) api.removePanel(replaced)
    queryClient.invalidateQueries({ queryKey: queryKeys.fileTree(target.root, target.path) })
    queryClient.invalidateQueries({ queryKey: queryKeys.backlog(target.root, target.path) })
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
    if (route.page === 'shell') return
    openAgent(route.name, true)
    const api = apiRef.current
    const id = agentTabID(route.name)
    const current = api ? panelFromAPI(api, id) : null
    if (current) setPathForPanel(current.params, push)
  }, [openAgent])

  const onDockReady = useCallback((event: DockviewReadyEvent) => {
    dockDisposables.current.forEach((disposable) => disposable.dispose())
    dockDisposables.current = []
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
    const onLayout = event.api.onDidLayoutChange(() => syncDock())
    const onActive = event.api.onDidActivePanelChange(({ panel }) => {
      const params = panelParams(panel?.params)
      setPathForPanel(params ?? undefined)
      syncDock()
    })
    const onRemove = event.api.onDidRemovePanel((panel) => {
      const params = panelParams(panel.params)
      if (params?.kind === 'file') {
        setFileGitStates((current) => {
          if (!(panel.id in current)) return current
          const next = { ...current }
          delete next[panel.id]
          return next
        })
      }
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
    const onMove = event.api.onDidMovePanel(({ panel }) => {
      if (pinMovedPreview(panel)) syncDock()
    })
    dockDisposables.current = [onLayout, onActive, onRemove, onMove]
    persistenceReady.current = true
    if ((replayedRoute && !restoreFailed) || restoredLegacy) layoutDirty.current = true
    setDockReady(true)
    setActivePanelID(event.api.activePanel?.id ?? '')
    setRevision((value) => value + 1)
  }, [applyRoute, initial, initialRoute, openAgent, syncDock])

  useEffect(() => () => dockDisposables.current.forEach((disposable) => disposable.dispose()), [])

  useEffect(() => {
    const update = () => {
      const route = currentRoute()
      if (route.page !== 'missing') applyRoute(route)
    }
    window.addEventListener('popstate', update)
    return () => window.removeEventListener('popstate', update)
  }, [applyRoute])

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

  useEffect(() => {
    const flush = () => { flushLayout() }
    window.addEventListener('pagehide', flush)
    return () => window.removeEventListener('pagehide', flush)
  }, [flushLayout])

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
  const stream = useFleetStream(agentNames, screenPaneIDs)
  const activeParams = openPanels.find((params) => {
    return dockPanelID(params) === activePanelID
  })
  const viewerFailure = viewerQuery.error ? apiProblem(viewerQuery.error) : null
  const viewer = viewerQuery.data?.viewer ?? 'unresolved'
  const viewerState = viewerQuery.isPending ? 'resolving' : viewerQuery.data ? 'attributed' : viewerFailure?.response?.status === 409 ? 'unresolved' : 'unavailable'
  const viewerProblem = viewerState === 'unavailable' ? viewerFailure?.problem.detail ?? '' : ''
  const viewerReadOnly = viewerQuery.isPending ? 'Resolving viewer identity…' : viewerQuery.data ? ''
    : viewerReadOnlyMessage(viewerFailure?.problem ?? { error: 'request failed', detail: 'unknown failure' }, viewerFailure?.response?.status)

  const setAgentStatus = useCallback((name: string, status: string) => setAgentStatuses((current) => current[name] === status ? current : { ...current, [name]: status }), [])
  const setFileGitState = useCallback((id: string, state: GitFileState) => setFileGitStates((current) => {
    const previous = current[id]
    const unchanged = previous?.mode === state.mode && previous.base === state.base &&
      previous.revision?.sha === state.revision?.sha && previous.revision?.path === state.revision?.path &&
      previous.commit?.sha === state.commit?.sha && previous.commit?.path === state.commit?.path
    return unchanged ? current : { ...current, [id]: state }
  }), [])
  const setAgentScreenPane = useCallback((name: string, paneID?: string) => setAgentScreenPanes((current) => {
    if (paneID) return current[name] === paneID ? current : { ...current, [name]: paneID }
    if (!(name in current)) return current
    const next = { ...current }; delete next[name]; return next
  }), [])
  const streamProblems = useMemo<Record<string, string>>(() => ({
    ...stream.problems,
    ...(boardQuery.error ? { fleet: boardQuery.error.message } : {}),
  }), [boardQuery.error, stream.problems])

  const showQuickOpen = useCallback((groupID?: string) => {
    setQuickOpenGroup(groupID ?? apiRef.current?.activeGroup?.id)
    setQuickOpen(true)
  }, [])
  const resetLayout = useCallback(() => {
    const api = apiRef.current
    if (!api) return
    api.clear()
    try {
      localStorage.removeItem(layoutStorageKey)
      localStorage.removeItem(layoutStorageBackupKey)
    } catch { /* best effort */ }
    persistenceState.current = { recovering: false, lastGoodRaw: null }
    setSidebarWidth(defaultSidebarWidth)
    syncDock()
  }, [syncDock])

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
    const stop = () => { window.removeEventListener('pointermove', move); window.removeEventListener('pointerup', stop) }
    window.addEventListener('pointermove', move); window.addEventListener('pointerup', stop)
  }

  const activeAgentStatus = activeParams?.kind === 'agent' ? agentBusStatus(boardQuery.data, activeParams.name) : '-'
  const quickOpenAgent = activeParams?.kind === 'agent' ? quickOpenAgentPreference(activeParams.name, activeAgentStatus) : undefined
  const workspace = useMemo<WorkspaceContextValue>(() => ({
    board: boardQuery.data, mentionMatcher, identityReadOnly: viewerReadOnly, openAgent, openFile, openFileInDiff, openChanges, openFolder, pinPanel, setFileViewMode, fileGitStates, setFileGitState,
    agentScreenPanes, setAgentScreenPane, onViewer: (resolvedViewer) => queryClient.setQueryData(queryKeys.viewer, { viewer: resolvedViewer }),
    onAgentStatus: setAgentStatus, agentStatuses, resetLayout, showQuickOpen, stream, streamProblems,
  }), [agentScreenPanes, agentStatuses, boardQuery.data, fileGitStates, mentionMatcher, openAgent, openChanges, openFile, openFileInDiff, openFolder, pinPanel, queryClient, resetLayout, setAgentScreenPane, setAgentStatus, setFileGitState, setFileViewMode, showQuickOpen, stream, streamProblems, viewerReadOnly])

  return <WorkspaceContext.Provider value={workspace}><div className="app-shell">
    <QuickOpen open={quickOpen} agent={quickOpenAgent} groupID={quickOpenGroup} onClose={() => { setQuickOpen(false); setQuickOpenGroup(undefined) }} onOpenFile={openFile} onOpenFolder={openFolder} />
    <ShortcutReference open={shortcutReference} onClose={() => setShortcutReference(false)} />
    <div className="sidebar-region" style={{ width: sidebarWidth }}>
      <FleetSidebar board={boardQuery.data} activeAgent={activeParams?.kind === 'agent' ? activeParams.name : undefined} activePane={activeParams?.kind === 'screen' ? activeParams.pane.pane_id : undefined}
        onPreviewAgent={(name, placement) => openAgent(name, true, placement)} onPinAgent={(name, placement) => openAgent(name, false, placement)} onPreviewPane={(pane, placement) => openScreen(pane, true, placement)} onPinPane={(pane, placement) => openScreen(pane, false, placement)}
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
  </div></WorkspaceContext.Provider>
}

export default function App() {
  const route = currentRoute()
  if (route.page !== 'missing') return <Shell initialRoute={route} />
  return <main className="agent-page"><AppLink to="/" className="back-link">← Workspace</AppLink><section className="not-found"><strong>404 · Page not found</strong></section></main>
}
