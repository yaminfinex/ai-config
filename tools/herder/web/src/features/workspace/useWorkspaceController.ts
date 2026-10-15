import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import type { DockviewApi, DockviewReadyEvent } from 'dockview-react'
import { apiProblem, queryKeys, viewerReadOnlyMessage } from '../../api/client'
import { fleetQueryOptions, viewerQueryOptions } from '../../api/queries'
import type { FileTarget } from '../../types'
import { agentBusStatus } from '../../shared/agentStatus'
import { agentMentionMatcher } from '../../shared/agentMentions'
import { forgetFollowScroll } from '../../shared/followScroll'
import { useDOMEvent } from '../../shared/lifecycle'
import { type Route } from '../../shared/navigation'
import { createFileWatchRegistry, type FileWatchTarget } from '../../stream/fileWatchRegistry'
import { useFleetStream } from '../../stream/useFleetStream'
import { quickOpenAgentPreference } from '../files/fileResolution'
import type { QuickOpenMode } from '../files/quickOpenModel.ts'
import type { GitFileState } from '../git/gitViewModel'
import { useLayoutPersistence } from '../layout/useLayoutPersistence'
import {
  aloneHistoryEntry,
  createHistorySuppressor,
  decideHistoryUpdate,
  layoutRouteState,
  lensHistoryEntry,
  pathWithSpace,
  replayHistoryRoute,
  routeFromHistory,
  routeFromLocation,
  spaceIDFromSearch,
  shouldReplayInitialRoute,
  viewFromLocation,
  type HistoryCause,
} from '../layout/historyModel'
import { cardBounds, crumbParts, morphIn, morphOut, swipe, useLens, type LensView } from '../lens/index.ts'
import { panelParams, pinMovedPreview, restoreDockLayout, storedSpaceActivePanel, writePanelToStoredSpace, type AgentPanelParams, type DockPanelParams } from '../layout/dockLayout'
import { panelID } from './panelRegistry'
import { panelFromAPI, useWorkspaceActions } from './useWorkspaceActions'
import { usePanelRecords } from './usePanelRecords'
import { screenSubscriptionPaneIDs } from './screenSubscriptions'
import { subscribeToDock } from './subscribeToDock'
import { useWorkspaceShortcuts } from './useWorkspaceShortcuts'
import { useArrive } from './useArrive'
import { useSpaceAgents } from './useSpaceAgents'
import { useSpaceAttention } from './useSpaceAttention'
import { useSpaceSwitcher } from './useSpaceSwitcher'
import { useSpaceMembers } from './useSpaceMembers'
import { useSwitchSpaceFocusing } from './useSwitchSpaceFocusing'
import type { WorkspaceActionsValue, WorkspaceDataValue } from './workspaceContext'
import { useNotes } from '../notes/NotesProvider.tsx'
import { planOpenGroupAsSpace, runOpenGroupAsSpace } from '../sidebar/groupDropModel.ts'
import {
  createSpacesStore,
  browserSpacesTransport,
  createServerSpaceLookup,
  createSpacesSync,
  createSpacesSyncPersistence,
  createAndSwitchSpace,
  closeSpaceLayout,
  hasRecoverableSpaceLayout,
  initializeSpaces,
  moveBeforeActiveClose,
  performSpaceSwitch,
  sendPanelToExistingSpace,
  sendPanelToNewSpace as sendPanelToNewSpaceModel,
  readLegacyLayoutFamilies,
  readActiveSpace,
  removeLayoutRecovery,
  reopenSpaceLayout,
  resetSpacesSyncCursor,
  restoreSpaceDock,
  serverSpaceLookupMessage,
  spaceStreamAgents,
  spacesStoreSyncAdapter,
  writeActiveSpace,
  type SpaceDefinition,
  type SpacesStatus,
  type SpacesStore,
  type SpacesInitialization,
  type LegacyLayoutFamilies,
  useReadMarkersContext,
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
  lookupSpaceID: string | null
}

function unavailableSpacesRuntime(problem: string, legacy?: LegacyLayoutFamilies): SpacesRuntime {
  const initialization: SpacesInitialization = {
    mode: 'legacy', activeSpaceID: null,
    legacy: legacy ?? { stored: null, backup: null, legacy: null, recovering: false, lastGoodRaw: null },
    problem,
  }
  return {
    initialization, store: null, spaces: [], recent: [], activeSpaceID: null,
    status: { persistent: false, recovered: false, problem }, problem, lookupSpaceID: null,
  }
}

function initializeBrowserSpaces(): SpacesRuntime {
  try {
    const initialization = initializeSpaces(localStorage)
    if (initialization.mode === 'legacy') return unavailableSpacesRuntime(initialization.problem, initialization.legacy)
    const store = createSpacesStore({ onPurge: (id) => { removeLayoutRecovery(localStorage, id); resetSpacesSyncCursor(localStorage) } })
  const spaces = store.list()
    const urlSpaceID = spaceIDFromSearch(window.location.search)
    const selection = readActiveSpace(spaces, urlSpaceID, sessionStorage, localStorage)
    if (!selection.id) return unavailableSpacesRuntime(
      'Spaces are unavailable because their saved definitions could not be read. Your current layout is still being saved.',
      readLegacyLayoutFamilies(localStorage),
    )
    const problem = selection.staleURL
      ? serverSpaceLookupMessage
      : store.status().problem
    return { initialization, store, spaces, recent: store.recentlyClosed().filter((space) => hasRecoverableSpaceLayout(localStorage, space.id)), activeSpaceID: selection.id, status: store.status(), problem, lookupSpaceID: selection.staleURL ? urlSpaceID : null }
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
  const { stateChanged: onNotesStateChanged } = useNotes()
  const { stateChanged: onReadMarkersStateChanged } = useReadMarkersContext()
  const [quickOpen, setQuickOpen] = useState(false)
  const [quickOpenMode, setQuickOpenMode] = useState<QuickOpenMode>({ kind: 'normal' })
  const [shortcutReference, setShortcutReference] = useState(false)
  const [quickOpenGroup, setQuickOpenGroup] = useState<string>()
  const { records: agentStatuses, set: setAgentStatusRecord, prune: pruneAgentStatus } = usePanelRecords<string>()
  const { records: agentTailPanes, set: setAgentTailPaneRecord, prune: pruneAgentTailPane } = usePanelRecords<string>()
  const { records: fileGitStates, set: setFileGitStateRecord, prune: pruneFileGitState } = usePanelRecords<GitFileState>(sameGitFileState)
  const { records: folderSelectionHints, set: setFolderSelectionHint, prune: pruneFolderSelectionHint } = usePanelRecords<FileTarget>()
  const [fileWatchTargets, setFileWatchTargets] = useState<FileWatchTarget[]>([])
  const [activePanelID, setActivePanelID] = useState('')
  const [revision, setRevision] = useState(0)
  const [spacesRuntime] = useState(initializeBrowserSpaces)
  // The view is home's: the lens, a zoom into one space's dock, or one agent
  // alone. The URL carries it (see viewFromLocation).
  const [view, setViewState] = useState<LensView>(() => viewFromLocation(initialRoute, window.location.search, spacesRuntime.store !== null))
  const viewRef = useRef(view)
  const setView = useCallback((next: LensView) => { viewRef.current = next; setViewState(next) }, [])
  const [lensSelected, setLensSelected] = useState<string | null>(spacesRuntime.activeSpaceID)
  const [spaces, setSpaces] = useState(spacesRuntime.spaces)
  const [recentSpaces, setRecentSpaces] = useState(spacesRuntime.recent)
  const [spacesStatus, setSpacesStatus] = useState(spacesRuntime.status)
  const [activeSpaceID, setActiveSpaceID] = useState(spacesRuntime.activeSpaceID)
  const activeSpaceIDRef = useRef(activeSpaceID)
  const [spaceProblem, setSpaceProblem] = useState(spacesRuntime.problem)
  const [spaceAnnouncement, setSpaceAnnouncement] = useState('')
  const [pendingLookupSwitchID, setPendingLookupSwitchID] = useState<string>()
  const [historySuppressor] = useState(() => createHistorySuppressor(
    (callback) => window.requestAnimationFrame(callback),
    (handle) => window.cancelAnimationFrame(handle),
  ))
  const apiRef = useRef<DockviewApi | undefined>(undefined)
  const spacesSyncRef = useRef<ReturnType<typeof createSpacesSync> | null>(null)
  const notesFocusReturn = useRef<HTMLElement | null>(null)
  const disposeDock = useRef<() => void>(() => undefined)
  const queryClient = useQueryClient()
  const boardQuery = useQuery(fleetQueryOptions())
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
      setRecentSpaces(store.recentlyClosed().filter((space) => hasRecoverableSpaceLayout(localStorage, space.id)))
      setSpacesStatus(store.status())
      if (store.status().problem) setSpaceProblem(store.status().problem)
    }
    const unsubscribe = store.subscribe(refresh)
    return () => unsubscribe()
  }, [spacesRuntime.store])

  const updateHistory = useCallback((params: DockPanelParams | undefined, cause: HistoryCause, spaceID = activeSpaceIDRef.current, suppressed = historySuppressor.active()) => {
    // Under the lens or an agent alone the dock is out of sight: its
    // changes keep the entry the view already has.
    const current = viewRef.current
    if (current.kind !== 'zoom') {
      const entry = current.kind === 'lens' ? lensHistoryEntry : aloneHistoryEntry(current.agent)
      window.history.replaceState(entry.state, '', entry.path)
      return
    }
    const update = decideHistoryUpdate(window.history.state, params, cause, suppressed, spaceID)
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

  // Every arrival at an agent (a zoom, a switch, a tab, a mention, an open
  // from the sidebar or ⌘K) lands the same way: see useArrive.
  const aloneAgent = useCallback(() => viewRef.current.kind === 'alone' ? viewRef.current.agent : undefined, [])
  const arrive = useArrive(apiRef, aloneAgent)

  const workspaceActions = useWorkspaceActions({
    apiRef,
    queryClient,
    quickOpenGroup,
    setQuickOpenGroup,
    syncDock,
    setFileGitState: setFileGitStateRecord,
    setFolderSelectionHint,
    resetPersistedLayout: layout.resetPersistedLayout,
    withHistorySuppressed: historySuppressor.run,
    onActivePanelParamsChanged,
    arrive,
  })
  const { openPanel, openAgent, openFile, openFileInDiff, openChanges, openFolder, closePanel, pinPanel, setFileViewMode, resetLayout } = workspaceActions

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
    if (!store || !store.list().some((space) => space.id === spaceID) || activeSpaceIDRef.current === spaceID) return false
    const api = apiRef.current
    if (!api) return false
    const historyWasSuppressed = historySuppressor.active()
    return performSpaceSwitch(spaceID, {
      flush: () => {
        const flushed = layout.flushBeforeSwitch()
        if (!flushed) setSpaceProblem('This space could not switch because its latest layout was not saved. Nothing was discarded.')
        return flushed
      },
      beginHistory: () => updateHistory(undefined, 'space-switch', spaceID, historyWasSuppressed),
      suspend: () => {
        activeSpaceIDRef.current = spaceID
        layout.beginRestore()
      },
      read: layout.readSpace,
      withHistorySuppressed: historySuppressor.run,
      dock: api,
      restore: restoreDockLayout,
      recoveredFromBackup: layout.noteBackupRecovery,
      complete: (id) => layout.completeRestore(id, false),
      persistActive: (id) => writeActiveSpace(id, sessionStorage, localStorage),
      replaceStamp: () => updateHistory(panelParams(api.activePanel?.params) ?? undefined, 'stamp', spaceID),
      finish: ({ restoreFailed, activeSaved }) => {
        setActiveSpaceID(spaceID)
        setActivePanelID(api.activePanel?.id ?? '')
        setSpaceProblem(restoreFailed
          ? 'This space could not be fully restored. Its unreadable layout was kept for recovery; other spaces were not changed.'
          : activeSaved ? store.status().problem : 'This tab remembers its active space only while it stays open because browser storage is unavailable.')
        setRevision((value) => value + 1)
      },
    })
  }, [historySuppressor, layout.beginRestore, layout.completeRestore, layout.flushBeforeSwitch, layout.noteBackupRecovery, layout.readSpace, spacesRuntime.store, updateHistory])

  // zoomInto shows a space's dock with agent's panel active (none: the
  // layout's focused panel as it is) and arrives there. From the lens (or
  // alone) it pushes the zoom's own entry, so back returns there; from a
  // zoom it is a space switch like any other. From the lens it grows out of
  // the space's card; from a zoom or an agent alone it swipes in from the
  // side of by.
  const zoomInto = useCallback((spaceID: string, agent?: string, focus = true, by: -1 | 1 = 1) => {
    const store = spacesRuntime.store
    const api = apiRef.current
    if (!store?.list().some((space) => space.id === spaceID) || !api) return false
    const fromZoom = viewRef.current.kind === 'zoom'
    const fromLens = viewRef.current.kind === 'lens'
    const card = fromLens ? cardBounds(spaceID) : null
    const moving = fromLens || activeSpaceIDRef.current !== spaceID
    if (activeSpaceIDRef.current !== spaceID && !(fromZoom ? switchSpace(spaceID) : historySuppressor.run(() => switchSpace(spaceID)))) return false
    setLensSelected(spaceID)
    if (!fromZoom) {
      setView({ kind: 'zoom' })
      window.history.pushState(layoutRouteState, '', pathWithSpace('/', spaceID))
    }
    historySuppressor.run(() => {
      if (!agent) return
      const panel = api.panels.find((candidate) => {
        const params = panelParams(candidate.params)
        return params?.kind === 'agent' && params.name === agent
      })
      // Never setActive on the already-active panel (see openPanel).
      if (panel && api.activePanel?.id !== panel.id) panel.api.setActive()
      else if (!panel) openAgent(agent, true)
    })
    updateHistory(panelParams(api.activePanel?.params) ?? undefined, 'stamp')
    if (focus) arrive()
    if (fromLens) morphIn(card)
    else if (moving) swipe(by)
    return true
  }, [arrive, historySuppressor, openAgent, setView, spacesRuntime.store, switchSpace, updateHistory])
  // zoomOut returns to the lens with the zoomed space still selected.
  const zoomOut = useCallback(() => {
    const current = viewRef.current
    if (current.kind === 'lens') return false
    if (current.kind === 'zoom' && activeSpaceIDRef.current) setLensSelected(activeSpaceIDRef.current)
    setView({ kind: 'lens' })
    window.history.pushState(lensHistoryEntry.state, '', lensHistoryEntry.path)
    window.requestAnimationFrame(() => document.querySelector<HTMLElement>('.lens')?.focus({ preventScroll: true }))
    if (current.kind === 'zoom' && activeSpaceIDRef.current) morphOut(activeSpaceIDRef.current)
    return true
  }, [setView])
  // openAlone opens an agent in no space by itself, with no dock around it.
  const openAlone = useCallback((agent: string) => {
    if (!spacesRuntime.store) return
    const current = viewRef.current
    if (current.kind === 'alone' && current.agent === agent) return
    setView({ kind: 'alone', agent })
    const entry = aloneHistoryEntry(agent)
    window.history.pushState(entry.state, '', entry.path)
    arrive()
    if (current.kind !== 'lens') swipe(1)
  }, [arrive, setView, spacesRuntime.store])
  // From the lens or an agent alone, opening anything zooms into the active
  // space first; the open joins the zoom's history entry.
  const inZoom = useCallback(<A extends unknown[], R>(open: (...args: A) => R) => (...args: A): R => {
    const spaceID = activeSpaceIDRef.current
    if (viewRef.current.kind === 'zoom' || !spaceID || !zoomInto(spaceID, undefined, false)) return open(...args)
    const result = historySuppressor.run(() => open(...args))
    updateHistory(panelParams(apiRef.current?.activePanel?.params) ?? undefined, 'stamp')
    return result
  }, [historySuppressor, updateHistory, zoomInto])

  useEffect(() => {
    const store = spacesRuntime.store
    if (!store) return
    let lookup: ReturnType<typeof createServerSpaceLookup> | undefined
    const sync = createSpacesSync({
      store: spacesStoreSyncAdapter(store),
      persistence: createSpacesSyncPersistence(localStorage),
      transport: browserSpacesTransport(),
      retry: (callback, delay) => window.setTimeout(callback, delay),
      cancelRetry: (handle) => window.clearTimeout(handle as number),
      onProblem: (problem) => setSpaceProblem(problem || store.status().problem),
      onRows: () => lookup?.rowsArrived(store.list().map(({ id }) => id)),
    })
    spacesSyncRef.current = sync
    if (spacesRuntime.lookupSpaceID) {
      lookup = createServerSpaceLookup(spacesRuntime.lookupSpaceID, {
        hasLocal: (id) => store.list().some((space) => space.id === id),
        switchTo: (id) => { setPendingLookupSwitchID(id); setSpaceProblem(store.status().problem) },
        fallback: () => {
          const fallback = store.list().find((space) => space.id === spacesRuntime.activeSpaceID)
          setSpaceProblem(`The space in this link is closed or unavailable. This tab opened ${fallback?.name ?? 'another space'} instead.`)
        },
        scheduleTimeout: (callback, delay) => window.setTimeout(callback, delay),
        cancelTimeout: (handle) => window.clearTimeout(handle as number),
      })
    }
    void sync.start().then(() => lookup?.firstPullCompleted(store.list().map(({ id }) => id)))
    const online = () => { void sync.retryNow() }
    window.addEventListener('online', online)
    return () => {
      window.removeEventListener('online', online)
      lookup?.dispose()
      sync.dispose()
      if (spacesSyncRef.current === sync) spacesSyncRef.current = null
    }
  }, [spacesRuntime.activeSpaceID, spacesRuntime.lookupSpaceID, spacesRuntime.store])

  useEffect(() => {
    if (!pendingLookupSwitchID || !apiRef.current || !spaces.some((space) => space.id === pendingLookupSwitchID)) return
    if (activeSpaceID === pendingLookupSwitchID || historySuppressor.run(() => switchSpace(pendingLookupSwitchID))) setPendingLookupSwitchID(undefined)
  }, [activeSpaceID, historySuppressor, pendingLookupSwitchID, revision, spaces, switchSpace])

  useEffect(() => {
    if (!spacesRuntime.store || !activeSpaceID || spaces.some((space) => space.id === activeSpaceID)) return
    const fallback = spaces[0]
    if (fallback) historySuppressor.run(() => switchSpace(fallback.id))
  }, [activeSpaceID, historySuppressor, spaces, spacesRuntime.store, switchSpace])

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
        // A space switch removes panels under suppressed history and keeps their scroll; a close forgets it.
        if (!historySuppressor.active()) forgetFollowScroll(panel.id)
        pruneAgentStatus(params.name)
        pruneAgentTailPane(params.name)
      },
      movePanel: ({ panel }) => { if (pinMovedPreview(panel)) syncDock() },
    })
    updateHistory(panelParams(event.api.activePanel?.params) ?? undefined, 'stamp')
    layout.completeRestore(activeSpaceID, (replayedRoute && !restoreFailed) || restoredLegacy)
    if (activeSpaceID) writeActiveSpace(activeSpaceID, sessionStorage, localStorage)
    if (restoreFailed) setSpaceProblem('This space could not be fully restored. Its unreadable layout was kept for recovery; other spaces were not changed.')
    setActivePanelID(event.api.activePanel?.id ?? '')
    setRevision((value) => value + 1)
    // A page loaded on a zoom or an agent alone arrives there.
    if (viewRef.current.kind !== 'lens') arrive()
  }, [activeSpaceID, applyRoute, arrive, historySuppressor, initialRoute, layout.completeRestore, layout.initial, layout.noteBackupRecovery, openAgent, pruneAgentStatus, pruneAgentTailPane, pruneFileGitState, pruneFolderSelectionHint, syncDock, updateHistory])

  useEffect(() => () => disposeDock.current(), [])
  useDOMEvent(window, 'popstate', () => {
    const next = viewFromLocation(routeFromLocation(window.location.pathname, window.location.search), window.location.search, spacesRuntime.store !== null)
    if (next.kind === 'lens' && activeSpaceIDRef.current && viewRef.current.kind === 'zoom') setLensSelected(activeSpaceIDRef.current)
    setView(next)
    if (next.kind === 'lens') return
    if (next.kind === 'zoom') {
      const route = routeFromHistory(window.location.pathname, window.location.search, window.history.state)
      historySuppressor.run(() => replayHistoryRoute(
        route,
        activeSpaceIDRef.current,
        (spaceID) => Boolean(spacesRuntime.store?.list().some((space) => space.id === spaceID)),
        (params) => Boolean(apiRef.current?.getPanel(panelID(params))),
        { switchSpace, applyRoute },
      ))
    }
    arrive()
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
  const screenPaneIDs = screenSubscriptionPaneIDs(agentNames, agentTailPanes)
  const { stateChanged: onMembersStateChanged, membersBySpace } = useSpaceMembers({
    store: spacesRuntime.store,
    apiRef,
    activeSpaceIDRef,
    revision,
    spaces,
    syncDock,
    withHistorySuppressed: historySuppressor.run,
    onProblem: (problem) => setSpaceProblem(problem || spacesRuntime.store?.status().problem || ''),
  })
  const onStateChanged = useCallback((namespace: string, rev: number) => {
    void spacesSyncRef.current?.stateChanged(namespace, rev)
    onMembersStateChanged(namespace, rev)
    onNotesStateChanged(namespace, rev)
    onReadMarkersStateChanged(namespace, rev)
  }, [onMembersStateChanged, onNotesStateChanged, onReadMarkersStateChanged])
  const openBySpace = useSpaceAgents({ revision, spaces, activeSpaceID, activeAgents: agentNames })
  const activeParams = openPanels.find((params) => panelID(params) === activePanelID)
  const viewerFailure = viewerQuery.error ? apiProblem(viewerQuery.error) : null
  const viewer = viewerQuery.data?.viewer ?? 'unresolved'
  const viewerState = viewerQuery.isPending ? 'resolving' : viewerQuery.data ? 'attributed' : viewerFailure?.response?.status === 409 ? 'unresolved' : 'unavailable'
  const viewerProblem = viewerState === 'unavailable' ? viewerFailure?.problem.detail ?? '' : ''
  const viewerReadOnly = viewerQuery.isPending ? 'Resolving viewer identity…' : viewerQuery.data ? ''
    : viewerReadOnlyMessage(viewerFailure?.problem ?? { error: 'request failed', detail: 'unknown failure' }, viewerFailure?.response?.status)
  const fleetProblem = boardQuery.error?.message ?? ''

  const showQuickOpen = useCallback((groupID?: string, mode: QuickOpenMode = { kind: 'normal' }) => {
    setQuickOpenGroup(groupID ?? apiRef.current?.activeGroup?.id)
    setQuickOpenMode(mode)
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
  const createNamedSpace = useCallback((name: string) => {
    const store = spacesRuntime.store
    if (!store) { setSpaceProblem(spacesRuntime.problem); return false }
    const result = createAndSwitchSpace(name, {
      create: store.create,
      rename: store.rename,
      switchTo: switchSpace,
      rollbackCreate: store.rollbackCreate,
      flush: store.flush,
    })
    setSpaceProblem(result.ok ? store.status().problem : result.reason)
    return result.ok
  }, [spacesRuntime.problem, spacesRuntime.store, switchSpace])
  // openGroupAsSpace: jump to the space named exactly after the group (or
  // stay, when it is already active) and touch nothing in it; else create one
  // with that name and open every member's transcript pinned once. Nothing is
  // closed and no group/space link is persisted — the space itself is the
  // only write.
  const openGroupAsSpace = useCallback((group: string, members: string[]) => {
    const store = spacesRuntime.store
    if (!store) { setSpaceProblem(spacesRuntime.problem); return false }
    return runOpenGroupAsSpace(planOpenGroupAsSpace(group, store.list()), members, {
      activeID: activeSpaceIDRef.current,
      switchTo: switchSpace,
      createNamed: createNamedSpace,
      open: (member) => openAgent(member, false),
    })
  }, [createNamedSpace, openAgent, spacesRuntime.problem, spacesRuntime.store, switchSpace])
  const renameSpace = useCallback((id: string, name: string) => {
    const store = spacesRuntime.store
    if (!store) return { ok: false as const, reason: spacesRuntime.problem }
    const result = store.rename(id, name)
    if (!result.ok) setSpaceProblem(result.reason)
    else { store.flush(); setSpaceProblem(store.status().problem) }
    return result
  }, [spacesRuntime.problem, spacesRuntime.store])
  const reorderSpace = useCallback((id: string, targetIndex: number) => {
    const store = spacesRuntime.store
    if (!store) return { ok: false as const, reason: spacesRuntime.problem }
    const before = store.list()
    const sourceIndex = before.findIndex((space) => space.id === id)
    if (sourceIndex < 0) return { ok: false as const, reason: 'This space is no longer open.' }
    const destination = Math.max(0, Math.min(Math.trunc(targetIndex), before.length - 1))
    const result = store.reorder(id, destination)
    if (!result.ok) setSpaceProblem(result.reason)
    else {
      store.flush()
      setSpaceProblem(store.status().problem)
      setSpaceAnnouncement(destination === sourceIndex
        ? `${result.value.name} is already ${destination === 0 ? 'first' : 'last'}.`
        : `Moved ${result.value.name} to position ${destination + 1} of ${before.length}.`)
    }
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
  const sendPanelToSpace = useCallback((sourceID: string, params: DockPanelParams, spaceID: string) => {
    try {
      const result = sendPanelToExistingSpace(spaceID, params, {
        write: writePanelToStoredSpace.bind(undefined, localStorage),
        closeSource: () => closePanel(sourceID),
      })
      setSpaceProblem(result.ok ? spacesRuntime.store?.status().problem ?? '' : result.reason)
      return result.ok
    } catch {
      setSpaceProblem('This pane could not be sent because browser storage is unavailable. Nothing was discarded.')
      return false
    }
  }, [closePanel, spacesRuntime.store])
  const sendPanelToNewSpace = useCallback((sourceID: string, params: DockPanelParams) => {
    const store = spacesRuntime.store
    if (!store) { setSpaceProblem(spacesRuntime.problem); return false }
    try {
      const result = sendPanelToNewSpaceModel(params, {
        create: store.create,
        rollbackCreate: store.rollbackCreate,
        write: writePanelToStoredSpace.bind(undefined, localStorage),
        flush: store.flush,
        closeSource: () => closePanel(sourceID),
      })
      setSpaceProblem(result.ok ? store.status().problem : result.reason)
      return result.ok
    } catch {
      setSpaceProblem('This pane could not be sent because browser storage is unavailable. Nothing was discarded.')
      return false
    }
  }, [closePanel, spacesRuntime.problem, spacesRuntime.store])
  const toggleFleetRail = useCallback(() => {
    layout.setFleetRail((rail) => ({ ...rail, collapsed: !rail.collapsed }))
  }, [layout.setFleetRail])
  const toggleNotesRail = useCallback(() => {
    const opening = layout.notesRail.collapsed
    if (opening) notesFocusReturn.current = document.activeElement instanceof HTMLElement ? document.activeElement : null
    layout.setNotesRail((rail) => ({ ...rail, collapsed: !rail.collapsed }))
    if (opening) window.requestAnimationFrame(() => document.querySelector<HTMLTextAreaElement>('[data-notes-quick-input="general"]')?.focus())
    else {
      const target = notesFocusReturn.current
      notesFocusReturn.current = null
      window.requestAnimationFrame(() => target?.isConnected && target.focus())
    }
  }, [layout.notesRail.collapsed, layout.setNotesRail])
  // Switches inside a zoom (the Spaces list, ⌥Tab, ⌘K) arrive at the new space's focused agent.
  const readActiveSpaceID = useCallback(() => activeSpaceIDRef.current, [])
  const switchSpaceFocusing = useSwitchSpaceFocusing(switchSpace, readActiveSpaceID, arrive)
  // A space's last focused panel: the live dock's for the active space, the
  // saved layout's for the others. A zoom returns there.
  const lastPanel = useCallback((spaceID: string) => spaceID === activeSpaceIDRef.current
    ? panelParams(apiRef.current?.activePanel?.params) ?? undefined
    : storedSpaceActivePanel(localStorage, spaceID), [])
  const spaceAttention = useSpaceAttention({ apiRef, board: boardQuery.data, openBySpace, membersBySpace, view })
  const { markUnread, markRead, toggleRead, seenNow, readThroughSeen, viewBlocks } = spaceAttention
  // A landed ⌘⇧⏎ has answered the agent: read up to what was seen, and its
  // current block viewed as zooming in on it would.
  const fileBack = useCallback((name: string) => {
    const seen = seenNow(name)
    return () => {
      readThroughSeen(name, seen)
      viewBlocks([name])
      zoomOut()
    }
  }, [readThroughSeen, seenNow, viewBlocks, zoomOut])
  const lens = useLens({
    enabled: spacesRuntime.store !== null, spaces, membersBySpace, board: boardQuery.data,
    attention: spaceAttention.attention, total: spaceAttention.total, view, activeSpaceID,
    selected: lensSelected, setSelected: setLensSelected, lastPanel, zoomInto, zoomOut, openAlone,
    markSpaceRead: spaceAttention.markSpaceRead, markSpaceUnread: spaceAttention.markSpaceUnread,
  })
  // The stream keeps the open panels' transcripts live, and the peeked agent's on the lens.
  const peeked = view.kind === 'lens' && lens.peek?.agent ? [lens.peek.agent] : []
  useFleetStream({ agents: spaceStreamAgents([...agentNames, ...peeked], openBySpace), screens: screenPaneIDs, watches: fileWatchTargets }, onStateChanged)
  // A space chosen anywhere but a zoom (⌥Tab or ⌘K on the lens) zooms into it.
  const switchSpaceAnywhere = useCallback((id: string, from?: 'quick-open') => viewRef.current.kind === 'zoom' ? switchSpaceFocusing(id, from) : lens.zoomCard(id), [lens.zoomCard, switchSpaceFocusing])
  const docked = useCallback(() => viewRef.current.kind === 'zoom', [])
  useWorkspaceShortcuts({ apiRef, docked, shortcutReference, setShortcutReference, showQuickOpen, closePanel, toggleNotesRail, stepSpace: lens.stepSpace, toggleRead, arrive })
  const spaceSwitcher = useSpaceSwitcher({ enabled: Boolean(spacesRuntime.store), spaces, activeSpaceID, switchSpace: switchSpaceAnywhere })
  const zoomedOpenAgent = useMemo(() => inZoom(openAgent), [inZoom, openAgent])
  const zoomedOpenFile = useMemo(() => inZoom(openFile), [inZoom, openFile])
  const zoomedOpenFileInDiff = useMemo(() => inZoom(openFileInDiff), [inZoom, openFileInDiff])
  const zoomedOpenChanges = useMemo(() => inZoom(openChanges), [inZoom, openChanges])
  const zoomedOpenFolder = useMemo(() => inZoom(openFolder), [inZoom, openFolder])

  const activeAgentStatus = activeParams?.kind === 'agent' ? agentBusStatus(boardQuery.data, activeParams.name) : '-'
  const quickOpenAgent = activeParams?.kind === 'agent' ? quickOpenAgentPreference(activeParams.name, activeAgentStatus) : undefined
  const setAgentStatus = useCallback((name: string, status: string) => setAgentStatusRecord(name, status), [setAgentStatusRecord])
  const setFileGitState = useCallback((id: string, state: GitFileState) => setFileGitStateRecord(id, state), [setFileGitStateRecord])
  const setAgentTailPane = useCallback((name: string, paneID?: string) => setAgentTailPaneRecord(name, paneID), [setAgentTailPaneRecord])
  const onViewer = useCallback((resolvedViewer: string) => queryClient.setQueryData(queryKeys.viewer, { viewer: resolvedViewer }), [queryClient])

  const actions = useMemo<WorkspaceActionsValue>(() => ({
    openAgent: zoomedOpenAgent, openFile: zoomedOpenFile, openFileInDiff: zoomedOpenFileInDiff, openChanges: zoomedOpenChanges, openFolder: zoomedOpenFolder, closePanel, pinPanel, setFileViewMode, setFileGitState,
    consumeFolderSelectionHint: pruneFolderSelectionHint,
    setAgentTailPane, onViewer, onAgentStatus: setAgentStatus,
    resetLayout, showQuickOpen, sendPanelToSpace, sendPanelToNewSpace, markUnread, markRead, arrive, fileBack,
  }), [arrive, closePanel, fileBack, markRead, markUnread, onViewer, zoomedOpenAgent, zoomedOpenChanges, zoomedOpenFile, zoomedOpenFileInDiff, zoomedOpenFolder, pinPanel, pruneFolderSelectionHint, resetLayout, sendPanelToNewSpace, sendPanelToSpace, setAgentStatus, setAgentTailPane, setFileGitState, setFileViewMode, showQuickOpen])
  // Panel params are read afresh from Dockview each render; the key keeps
  // the active panel's identity, and so the data context's, while it is unchanged.
  const activePanelKey = activeParams ? JSON.stringify(activeParams) : ''
  const activePanel = useMemo(() => activePanelKey ? { id: activePanelID, params: JSON.parse(activePanelKey) as DockPanelParams } : null, [activePanelID, activePanelKey])
  const data = useMemo<WorkspaceDataValue>(() => ({
    mentionMatcher, identityReadOnly: viewerReadOnly, fileGitStates, folderSelectionHints, agentStatuses,
    spaces, activeSpaceID, activePanel,
  }), [activePanel, activeSpaceID, agentStatuses, fileGitStates, folderSelectionHints, mentionMatcher, spaces, viewerReadOnly])

  return {
    actions, data, fileWatchRegister: fileWatchRegistry.register,
    quickOpen, quickOpenMode, quickOpenAgent, quickOpenGroup,
    closeQuickOpen: () => { setQuickOpen(false); setQuickOpenGroup(undefined); setQuickOpenMode({ kind: 'normal' }) },
    shortcutReference, setShortcutReference,
    fleetRail: layout.fleetRail, setFleetRail: layout.setFleetRail, toggleFleetRail,
    notesRail: layout.notesRail, setNotesRail: layout.setNotesRail, toggleNotesRail,
    expandedItems: layout.expandedItems, setExpandedItems: layout.setExpandedItems,
    knownWorkspaceItems: layout.knownWorkspaceItems, setKnownWorkspaceItems: layout.setKnownWorkspaceItems,
    knownManagerItems: layout.knownManagerItems, setKnownManagerItems: layout.setKnownManagerItems,
    fleetView: layout.fleetView, setFleetView: layout.setFleetView,
    pendingGroups: layout.pendingGroups, setPendingGroups: layout.setPendingGroups,
    board: boardQuery.data,
    activeAgent: activeParams?.kind === 'agent' ? activeParams.name : undefined,
    openAgent: zoomedOpenAgent, openFile: zoomedOpenFile, openFolder: zoomedOpenFolder,
    onLens: view.kind === 'lens',
    aloneAgent: view.kind === 'alone' ? view.agent : undefined,
    crumb: spacesRuntime.store && view.kind !== 'lens' ? crumbParts(view, spaces.find((space) => space.id === activeSpaceID), activeParams?.kind === 'agent' ? activeParams.name : undefined) : null,
    lens, zoomOut, lensSpaces: spaces.length,
    onDockReady, flushLayout: () => { layout.flushAll(); spacesRuntime.store?.flush() },
    spaces: {
      enabled: Boolean(spacesRuntime.store),
      items: spaces,
      recent: recentSpaces,
      activeID: activeSpaceID,
      status: spacesStatus,
      problem: spaceProblem,
      switch: switchSpaceAnywhere,
      create: createSpace,
      createNamed: createNamedSpace,
      openGroup: openGroupAsSpace,
      rename: renameSpace,
      reorder: reorderSpace,
      close: closeSpace,
      reopen: reopenSpace,
      announcement: spaceAnnouncement,
      attention: spaceAttention.attention,
      total: spaceAttention.total,
      markAllRead: spaceAttention.markSpaceRead,
      collapsed: layout.spacesCollapsed,
      onCollapsed: layout.setSpacesCollapsed,
    },
    spaceSwitcher,
    spaceProblem,
    fleetProblem, viewerProblem, viewer, viewerPending: viewerQuery.isPending,
  }
}
