import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import type { DockviewApi, DockviewReadyEvent } from 'dockview-react'
import { apiProblem, getFleet, queryKeys, viewerReadOnlyMessage } from '../../api/client'
import { viewerQueryOptions } from '../../api/queries'
import type { FileTarget } from '../../types'
import { agentBusStatus } from '../../shared/agentStatus'
import { agentMentionMatcher } from '../../shared/agentMentions'
import { useDOMEvent } from '../../shared/lifecycle'
import { type Route } from '../../shared/navigation'
import { createFileWatchRegistry, type FileWatchTarget } from '../../stream/fileWatchRegistry'
import { useFleetStream } from '../../stream/useFleetStream'
import { quickOpenAgentPreference } from '../files/fileResolution'
import type { GitFileState } from '../git/gitViewModel'
import { useLayoutPersistence } from '../layout/useLayoutPersistence'
import {
  createHistorySuppressor,
  decideHistoryUpdate,
  routeFromHistory,
  shouldReplayInitialRoute,
  type HistoryCause,
} from '../layout/historyModel'
import { panelParams, pinMovedPreview, restoreDockLayout, screenIdentityState, type AgentPanelParams, type DockPanelParams } from '../layout/dockLayout'
import { panelID } from './panelRegistry'
import { panelFromAPI, useWorkspaceActions } from './useWorkspaceActions'
import { usePanelRecords } from './usePanelRecords'
import { subscribeToDock } from './subscribeToDock'
import { useWorkspaceShortcuts } from './useWorkspaceShortcuts'
import type { WorkspaceActionsValue, WorkspaceDataValue } from './workspaceContext'
import {
  createSpacesStore,
  closeSpaceLayout,
  initializeSpaces,
  defaultMaxSpaces,
  moveBeforeActiveClose,
  performSpaceSwitch,
  readLegacyLayoutFamilies,
  readActiveSpace,
  removeLayoutRecovery,
  reopenSpaceLayout,
  restoreSpaceDock,
  writeActiveSpace,
  type SpaceDefinition,
  type SpacesStatus,
  type SpacesStore,
  type SpacesInitialization,
  type LegacyLayoutFamilies,
} from '../spaces/index.ts'

function sameGitFileState(left: GitFileState, right: GitFileState) {
  return left.mode === right.mode && left.base === right.base &&
    left.revision?.sha === right.revision?.sha && left.revision?.path === right.revision?.path &&
    left.commit?.sha === right.commit?.sha && left.commit?.path === right.commit?.path
}

type SpacesRuntime = {
  initialization: SpacesInitialization
  store: SpacesStore | null
  spaces: SpaceDefinition[]
  recent: SpaceDefinition[]
  activeSpaceID: string | null
  status: SpacesStatus
  problem: string
}

function unavailableSpacesRuntime(problem: string, legacy?: LegacyLayoutFamilies): SpacesRuntime {
  const initialization: SpacesInitialization = {
    mode: 'legacy', activeSpaceID: null,
    legacy: legacy ?? { stored: null, backup: null, legacy: null, recovering: false, lastGoodRaw: null },
    problem,
  }
  return {
    initialization, store: null, spaces: [], recent: [], activeSpaceID: null,
    status: { persistent: false, recovered: false, problem }, problem,
  }
}

function initializeBrowserSpaces(): SpacesRuntime {
  try {
    const initialization = initializeSpaces(localStorage)
    if (initialization.mode === 'legacy') return unavailableSpacesRuntime(initialization.problem, initialization.legacy)
    const store = createSpacesStore({ onPurge: (id) => removeLayoutRecovery(localStorage, id) })
    const spaces = store.list()
    const activeSpaceID = readActiveSpace(spaces, sessionStorage, localStorage)
    if (!activeSpaceID) return unavailableSpacesRuntime(
      'Spaces are unavailable because their saved definitions could not be read. Your current layout is still being saved.',
      readLegacyLayoutFamilies(localStorage),
    )
    return { initialization, store, spaces, recent: store.recentlyClosed(), activeSpaceID, status: store.status(), problem: store.status().problem }
  } catch {
    const initialization = initializeSpaces({
      getItem: () => null,
      setItem: () => { throw new Error('storage unavailable') },
      removeItem: () => undefined,
    })
    const problem = 'Spaces are unavailable in this browser right now. Your current layout is still being saved.'
    return unavailableSpacesRuntime(problem, initialization.mode === 'legacy' ? initialization.legacy : undefined)
  }
}

export function useWorkspaceController(initialRoute: Exclude<Route, { page: 'missing' }>) {
  const [quickOpen, setQuickOpen] = useState(false)
  const [shortcutReference, setShortcutReference] = useState(false)
  const [quickOpenGroup, setQuickOpenGroup] = useState<string>()
  const { records: agentStatuses, set: setAgentStatusRecord, prune: pruneAgentStatus } = usePanelRecords<string>()
  const { records: agentScreenPanes, set: setAgentScreenPaneRecord, prune: pruneAgentScreenPane } = usePanelRecords<string>()
  const [focusedScreenPaneID, setFocusedScreenPaneID] = useState<string>()
  const { records: fileGitStates, set: setFileGitStateRecord, prune: pruneFileGitState } = usePanelRecords<GitFileState>(sameGitFileState)
  const { records: folderSelectionHints, set: setFolderSelectionHint, prune: pruneFolderSelectionHint } = usePanelRecords<FileTarget>()
  const [fileWatchTargets, setFileWatchTargets] = useState<FileWatchTarget[]>([])
  const [activePanelID, setActivePanelID] = useState('')
  const [revision, setRevision] = useState(0)
  const [spacesRuntime] = useState(initializeBrowserSpaces)
  const [spaces, setSpaces] = useState(spacesRuntime.spaces)
  const [recentSpaces, setRecentSpaces] = useState(spacesRuntime.recent)
  const [spacesStatus, setSpacesStatus] = useState(spacesRuntime.status)
  const [activeSpaceID, setActiveSpaceID] = useState(spacesRuntime.activeSpaceID)
  const [spaceProblem, setSpaceProblem] = useState(spacesRuntime.problem)
  const [historySuppressor] = useState(() => createHistorySuppressor(
    (callback) => window.requestAnimationFrame(callback),
    (handle) => window.cancelAnimationFrame(handle),
  ))
  const apiRef = useRef<DockviewApi | undefined>(undefined)
  const notesFocusReturn = useRef<HTMLElement | null>(null)
  const disposeDock = useRef<() => void>(() => undefined)
  const queryClient = useQueryClient()
  const boardQuery = useQuery({ queryKey: queryKeys.fleet, queryFn: () => getFleet(), staleTime: Infinity, retry: false })
  const viewerQuery = useQuery(viewerQueryOptions())
  const mentionMatcher = useMemo(() => agentMentionMatcher(boardQuery.data), [boardQuery.data])
  const fileWatchRegistry = useMemo(() => createFileWatchRegistry(setFileWatchTargets), [])
  const layout = useLayoutPersistence(apiRef, revision, spacesRuntime.initialization, activeSpaceID)

  useEffect(() => () => fileWatchRegistry.dispose(), [fileWatchRegistry])
  useEffect(() => () => historySuppressor.dispose(), [historySuppressor])
  useEffect(() => {
    const store = spacesRuntime.store
    if (!store) return
    const refresh = () => {
      setSpaces(store.list())
      setRecentSpaces(store.recentlyClosed())
      setSpacesStatus(store.status())
      if (store.status().problem) setSpaceProblem(store.status().problem)
    }
    const unsubscribe = store.subscribe(refresh)
    return () => unsubscribe()
  }, [spacesRuntime.store])

  const updateHistory = useCallback((params: DockPanelParams | undefined, cause: HistoryCause) => {
    const update = decideHistoryUpdate(window.history.state, params, cause, historySuppressor.active())
    window.history[update.method === 'push' ? 'pushState' : 'replaceState'](update.entry.state, '', update.entry.path)
  }, [historySuppressor])

  const onActivePanelParamsChanged = useCallback((params: DockPanelParams) => {
    updateHistory(params, 'merge')
  }, [updateHistory])

  const syncDock = useCallback(() => {
    layout.markDirty()
    setActivePanelID(apiRef.current?.activePanel?.id ?? '')
    setRevision((value) => value + 1)
  }, [layout.markDirty])

  const workspaceActions = useWorkspaceActions({
    apiRef,
    board: boardQuery.data,
    queryClient,
    quickOpenGroup,
    setQuickOpenGroup,
    syncDock,
    setFileGitState: setFileGitStateRecord,
    setFolderSelectionHint,
    resetPersistedLayout: layout.resetPersistedLayout,
    withHistorySuppressed: historySuppressor.run,
    onActivePanelParamsChanged,
  })
  const { openPanel, openAgent, openScreen, openFile, openFileInDiff, openChanges, openFolder, closePanel, pinPanel, setFileViewMode, resetLayout } = workspaceActions

  const applyRoute = useCallback((route: Exclude<Route, { page: 'missing' }>) => {
    historySuppressor.run(() => {
      if (route.page === 'shell') {
        updateHistory(undefined, 'replay')
        return
      }
      openPanel({ ...route.params, preview: true }, undefined, false)
      const api = apiRef.current
      const current = api ? panelFromAPI(api, panelID(route.params)) : null
      updateHistory(current?.params, 'replay')
    })
  }, [historySuppressor, openPanel, updateHistory])

  const switchSpace = useCallback((spaceID: string) => {
    const store = spacesRuntime.store
    if (!store || !store.list().some((space) => space.id === spaceID) || activeSpaceID === spaceID) return false
    const api = apiRef.current
    if (!api) return false
    return performSpaceSwitch(spaceID, {
      flush: () => {
        const flushed = layout.flushBeforeSwitch()
        if (!flushed) setSpaceProblem('This space could not switch because its latest layout was not saved. Nothing was discarded.')
        return flushed
      },
      suspend: layout.beginRestore,
      read: layout.readSpace,
      withHistorySuppressed: historySuppressor.run,
      dock: api,
      restore: restoreDockLayout,
      recoveredFromBackup: layout.noteBackupRecovery,
      complete: (id) => layout.completeRestore(id, false),
      persistActive: (id) => writeActiveSpace(id, sessionStorage, localStorage),
      replaceStamp: () => updateHistory(panelParams(api.activePanel?.params) ?? undefined, 'stamp'),
      finish: ({ restoreFailed, activeSaved }) => {
        setActiveSpaceID(spaceID)
        setActivePanelID(api.activePanel?.id ?? '')
        setSpaceProblem(restoreFailed
          ? 'This space could not be fully restored. Its unreadable layout was kept for recovery; other spaces were not changed.'
          : activeSaved ? store.status().problem : 'This tab remembers its active space only while it stays open because browser storage is unavailable.')
        setRevision((value) => value + 1)
      },
    })
  }, [activeSpaceID, historySuppressor, layout.beginRestore, layout.completeRestore, layout.flushBeforeSwitch, layout.noteBackupRecovery, layout.readSpace, spacesRuntime.store, updateHistory])

  useEffect(() => {
    if (!spacesRuntime.store || !activeSpaceID || spaces.some((space) => space.id === activeSpaceID)) return
    const fallback = spaces[0]
    if (fallback) switchSpace(fallback.id)
  }, [activeSpaceID, spaces, spacesRuntime.store, switchSpace])

  const onDockReady = useCallback((event: DockviewReadyEvent) => {
    disposeDock.current()
    apiRef.current = event.api
    let restored = false
    let restoreFailed = false
    let restoredLegacy = false
    historySuppressor.run(() => {
      const result = restoreSpaceDock(
        event.api,
        layout.initial,
        restoreDockLayout,
        () => layout.noteBackupRecovery(activeSpaceID ?? undefined),
      )
      restored = result.restored
      restoreFailed = result.restoreFailed
      if (!restored && layout.initial.legacy) {
        layout.initial.legacy.openTabs.forEach((name) => openAgent(name, false))
        const active = layout.initial.legacy.activeTab === 'board' ? event.api.panels[0] : event.api.getPanel(layout.initial.legacy.activeTab)
        active?.api.setActive()
        restoredLegacy = true
      }
    })
    const replayedRoute = shouldReplayInitialRoute(initialRoute, window.history.state, restored)
    if (replayedRoute) applyRoute(initialRoute)
    disposeDock.current = subscribeToDock(event.api, {
      layout: syncDock,
      activePanel: ({ panel }) => {
        updateHistory(panelParams(panel?.params) ?? undefined, 'activation')
        syncDock()
      },
      removePanel: (panel) => {
        const params = panelParams(panel.params)
        if (params?.kind === 'file') pruneFileGitState(panel.id)
        if (params?.kind === 'folder') pruneFolderSelectionHint(panel.id)
        if (params?.kind !== 'agent') return
        pruneAgentStatus(params.name)
        pruneAgentScreenPane(params.name)
      },
      movePanel: ({ panel }) => { if (pinMovedPreview(panel)) syncDock() },
    })
    updateHistory(panelParams(event.api.activePanel?.params) ?? undefined, 'stamp')
    layout.completeRestore(activeSpaceID, (replayedRoute && !restoreFailed) || restoredLegacy)
    if (activeSpaceID) writeActiveSpace(activeSpaceID, sessionStorage, localStorage)
    if (restoreFailed) setSpaceProblem('This space could not be fully restored. Its unreadable layout was kept for recovery; other spaces were not changed.')
    setActivePanelID(event.api.activePanel?.id ?? '')
    setRevision((value) => value + 1)
  }, [activeSpaceID, applyRoute, historySuppressor, initialRoute, layout.completeRestore, layout.initial, layout.noteBackupRecovery, openAgent, pruneAgentScreenPane, pruneAgentStatus, pruneFileGitState, pruneFolderSelectionHint, syncDock, updateHistory])

  useEffect(() => () => disposeDock.current(), [])
  useDOMEvent(window, 'popstate', () => {
    const route = routeFromHistory(window.location.pathname, window.location.search, window.history.state)
    if (route.page !== 'missing') applyRoute(route)
  })

  const restoredPanels = layout.initial.stored?.dock ? Object.values(layout.initial.stored.dock.panels).flatMap((panel) => {
    const params = panelParams(panel.params)
    return params ? [params] : []
  }) : layout.initial.legacy ? layout.initial.legacy.openTabs.map((name): AgentPanelParams => ({ kind: 'agent', name, preview: false })) : []
  const openPanels = apiRef.current?.panels.flatMap((panel) => {
    const params = panelParams(panel.params)
    return params ? [params] : []
  }) ?? restoredPanels
  const agentNames = [...new Set(openPanels.flatMap((params) => params.kind === 'agent' ? [params.name] : []))]
  const provenScreenPaneIDs = openPanels.flatMap((params) => params.kind === 'screen' && screenIdentityState(params, boardQuery.data) === 'ready' ? [params.identity.paneID] : [])
  const screenPaneIDs = [...new Set([...provenScreenPaneIDs, ...agentNames.flatMap((name) => agentScreenPanes[name] ? [agentScreenPanes[name]] : [])])]
  const focusedPane = focusedScreenPaneID && screenPaneIDs.includes(focusedScreenPaneID) ? focusedScreenPaneID : undefined
  useFleetStream(agentNames, screenPaneIDs, fileWatchTargets, focusedPane)
  const activeParams = openPanels.find((params) => panelID(params) === activePanelID)
  const viewerFailure = viewerQuery.error ? apiProblem(viewerQuery.error) : null
  const viewer = viewerQuery.data?.viewer ?? 'unresolved'
  const viewerState = viewerQuery.isPending ? 'resolving' : viewerQuery.data ? 'attributed' : viewerFailure?.response?.status === 409 ? 'unresolved' : 'unavailable'
  const viewerProblem = viewerState === 'unavailable' ? viewerFailure?.problem.detail ?? '' : ''
  const viewerReadOnly = viewerQuery.isPending ? 'Resolving viewer identity…' : viewerQuery.data ? ''
    : viewerReadOnlyMessage(viewerFailure?.problem ?? { error: 'request failed', detail: 'unknown failure' }, viewerFailure?.response?.status)
  const fleetProblem = boardQuery.error?.message ?? ''

  const showQuickOpen = useCallback((groupID?: string) => {
    setQuickOpenGroup(groupID ?? apiRef.current?.activeGroup?.id)
    setQuickOpen(true)
  }, [])
  const createSpace = useCallback(() => {
    const store = spacesRuntime.store
    if (!store) return { ok: false as const, reason: spacesRuntime.problem }
    const result = store.create()
    if (!result.ok) { setSpaceProblem(result.reason); return result }
    store.flush()
    switchSpace(result.value.id)
    setSpaceProblem(store.status().problem)
    return result
  }, [spacesRuntime.problem, spacesRuntime.store, switchSpace])
  const renameSpace = useCallback((id: string, name: string) => {
    const store = spacesRuntime.store
    if (!store) return { ok: false as const, reason: spacesRuntime.problem }
    const result = store.rename(id, name)
    if (!result.ok) setSpaceProblem(result.reason)
    else { store.flush(); setSpaceProblem(store.status().problem) }
    return result
  }, [spacesRuntime.problem, spacesRuntime.store])
  const closeSpace = useCallback((id: string) => {
    const store = spacesRuntime.store
    if (!store) return { ok: false as const, reason: spacesRuntime.problem }
    const live = store.list()
    if (!live.some((space) => space.id === id)) return { ok: false as const, reason: 'This space is already closed.' }
    const movedAside = moveBeforeActiveClose(id, activeSpaceID, live, {
      create: store.create,
      rollbackCreate: store.rollbackCreate,
      switchTo: switchSpace,
    })
    if (!movedAside.ok) { setSpaceProblem(movedAside.reason); return movedAside }
    const moved = closeSpaceLayout(localStorage, id)
    if (!moved.ok) { setSpaceProblem(moved.reason); return moved }
    const closed = store.close(id)
    if (!closed.ok) {
      reopenSpaceLayout(localStorage, id)
      setSpaceProblem(closed.reason)
      return closed
    }
    store.flush()
    setSpaceProblem(store.status().problem)
    return closed
  }, [activeSpaceID, spacesRuntime.problem, spacesRuntime.store, switchSpace])
  const reopenSpace = useCallback((id: string) => {
    const store = spacesRuntime.store
    if (!store) return { ok: false as const, reason: spacesRuntime.problem }
    if (store.list().length >= defaultMaxSpaces) {
      const refusal = { ok: false as const, reason: `This space cannot be reopened while the ${defaultMaxSpaces}-space limit is full.` }
      setSpaceProblem(refusal.reason)
      return refusal
    }
    const restored = reopenSpaceLayout(localStorage, id)
    if (!restored.ok) { setSpaceProblem(restored.reason); return restored }
    const reopened = store.reopen(id)
    if (!reopened.ok) {
      closeSpaceLayout(localStorage, id)
      setSpaceProblem(reopened.reason)
      return reopened
    }
    store.flush()
    removeLayoutRecovery(localStorage, id)
    switchSpace(id)
    setSpaceProblem(store.status().problem)
    return reopened
  }, [spacesRuntime.problem, spacesRuntime.store, switchSpace])
  const toggleFleetRail = useCallback(() => {
    layout.setFleetRail((rail) => ({ ...rail, collapsed: !rail.collapsed }))
  }, [layout.setFleetRail])
  const toggleNotesRail = useCallback(() => {
    const opening = layout.notesRail.collapsed
    if (opening) notesFocusReturn.current = document.activeElement instanceof HTMLElement ? document.activeElement : null
    layout.setNotesRail((rail) => ({ ...rail, collapsed: !rail.collapsed }))
    if (opening) window.requestAnimationFrame(() => document.querySelector<HTMLInputElement>('[data-notes-quick-input="general"]')?.focus())
    else {
      const target = notesFocusReturn.current
      notesFocusReturn.current = null
      window.requestAnimationFrame(() => target?.isConnected && target.focus())
    }
  }, [layout.notesRail.collapsed, layout.setNotesRail])
  useWorkspaceShortcuts({ apiRef, shortcutReference, setShortcutReference, showQuickOpen, closePanel, toggleNotesRail })

  const activeAgentStatus = activeParams?.kind === 'agent' ? agentBusStatus(boardQuery.data, activeParams.name) : '-'
  const quickOpenAgent = activeParams?.kind === 'agent' ? quickOpenAgentPreference(activeParams.name, activeAgentStatus) : undefined
  const setAgentStatus = useCallback((name: string, status: string) => setAgentStatusRecord(name, status), [setAgentStatusRecord])
  const setFileGitState = useCallback((id: string, state: GitFileState) => setFileGitStateRecord(id, state), [setFileGitStateRecord])
  const setAgentScreenPane = useCallback((name: string, paneID?: string) => setAgentScreenPaneRecord(name, paneID), [setAgentScreenPaneRecord])
  const onViewer = useCallback((resolvedViewer: string) => queryClient.setQueryData(queryKeys.viewer, { viewer: resolvedViewer }), [queryClient])

  const actions = useMemo<WorkspaceActionsValue>(() => ({
    openAgent, openFile, openFileInDiff, openChanges, openFolder, closePanel, pinPanel, setFileViewMode, setFileGitState,
    consumeFolderSelectionHint: pruneFolderSelectionHint,
    setAgentScreenPane, onTerminalFocus: setFocusedScreenPaneID, onViewer, onAgentStatus: setAgentStatus,
    resetLayout, showQuickOpen,
  }), [closePanel, onViewer, openAgent, openChanges, openFile, openFileInDiff, openFolder, pinPanel, pruneFolderSelectionHint, resetLayout, setAgentScreenPane, setAgentStatus, setFileGitState, setFileViewMode, showQuickOpen])
  const data = useMemo<WorkspaceDataValue>(() => ({
    board: boardQuery.data, mentionMatcher, identityReadOnly: viewerReadOnly, fileGitStates, folderSelectionHints, agentScreenPanes, agentStatuses,
  }), [agentScreenPanes, agentStatuses, boardQuery.data, fileGitStates, folderSelectionHints, mentionMatcher, viewerReadOnly])

  return {
    actions, data, fileWatchRegister: fileWatchRegistry.register,
    quickOpen, quickOpenAgent, quickOpenGroup,
    closeQuickOpen: () => { setQuickOpen(false); setQuickOpenGroup(undefined) },
    shortcutReference, setShortcutReference,
    fleetRail: layout.fleetRail, setFleetRail: layout.setFleetRail, toggleFleetRail,
    notesRail: layout.notesRail, setNotesRail: layout.setNotesRail, toggleNotesRail,
    expandedItems: layout.expandedItems, setExpandedItems: layout.setExpandedItems,
    knownWorkspaceItems: layout.knownWorkspaceItems, setKnownWorkspaceItems: layout.setKnownWorkspaceItems,
    board: boardQuery.data,
    activeAgent: activeParams?.kind === 'agent' ? activeParams.name : undefined,
    activePane: activeParams?.kind === 'screen' ? activeParams.pane.pane_id : undefined,
    openAgent, openScreen, openFile, openFolder,
    onDockReady, flushLayout: () => { layout.flushAll(); spacesRuntime.store?.flush() },
    spaces: {
      enabled: Boolean(spacesRuntime.store),
      items: spaces,
      recent: recentSpaces,
      activeID: activeSpaceID,
      status: spacesStatus,
      problem: spaceProblem,
      switch: switchSpace,
      create: createSpace,
      rename: renameSpace,
      close: closeSpace,
      reopen: reopenSpace,
    },
    spaceProblem,
    fleetProblem, viewerProblem, viewer, viewerPending: viewerQuery.isPending,
  }
}
