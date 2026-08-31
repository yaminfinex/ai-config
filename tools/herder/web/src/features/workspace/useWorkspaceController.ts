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

function sameGitFileState(left: GitFileState, right: GitFileState) {
  return left.mode === right.mode && left.base === right.base &&
    left.revision?.sha === right.revision?.sha && left.revision?.path === right.revision?.path &&
    left.commit?.sha === right.commit?.sha && left.commit?.path === right.commit?.path
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
  const [historySuppressor] = useState(() => createHistorySuppressor(
    (callback) => window.requestAnimationFrame(callback),
    (handle) => window.cancelAnimationFrame(handle),
  ))
  const apiRef = useRef<DockviewApi | undefined>(undefined)
  const disposeDock = useRef<() => void>(() => undefined)
  const queryClient = useQueryClient()
  const boardQuery = useQuery({ queryKey: queryKeys.fleet, queryFn: () => getFleet(), staleTime: Infinity, retry: false })
  const viewerQuery = useQuery(viewerQueryOptions())
  const mentionMatcher = useMemo(() => agentMentionMatcher(boardQuery.data), [boardQuery.data])
  const fileWatchRegistry = useMemo(() => createFileWatchRegistry(setFileWatchTargets), [])
  const layout = useLayoutPersistence(apiRef, revision)

  useEffect(() => () => fileWatchRegistry.dispose(), [fileWatchRegistry])
  useEffect(() => () => historySuppressor.dispose(), [historySuppressor])

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

  const onDockReady = useCallback((event: DockviewReadyEvent) => {
    disposeDock.current()
    apiRef.current = event.api
    let restored = false
    let restoreFailed = false
    let restoredLegacy = false
    historySuppressor.run(() => {
      if (layout.initial.stored?.dock) {
        restored = restoreDockLayout(event.api, layout.initial.stored.dock)
        restoreFailed = !restored
      }
      if (!restored && layout.initial.backup?.dock && layout.initial.backup !== layout.initial.stored) {
        restored = restoreDockLayout(event.api, layout.initial.backup.dock)
        restoreFailed = !restored
        if (restored) layout.noteBackupRecovery()
      }
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
    layout.completeRestore((replayedRoute && !restoreFailed) || restoredLegacy)
    setActivePanelID(event.api.activePanel?.id ?? '')
    setRevision((value) => value + 1)
  }, [applyRoute, historySuppressor, initialRoute, layout.completeRestore, layout.initial, layout.noteBackupRecovery, openAgent, pruneAgentScreenPane, pruneAgentStatus, pruneFileGitState, pruneFolderSelectionHint, syncDock, updateHistory])

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
  const stream = useFleetStream(agentNames, screenPaneIDs, fileWatchTargets, focusedPane)
  const activeParams = openPanels.find((params) => panelID(params) === activePanelID)
  const viewerFailure = viewerQuery.error ? apiProblem(viewerQuery.error) : null
  const viewer = viewerQuery.data?.viewer ?? 'unresolved'
  const viewerState = viewerQuery.isPending ? 'resolving' : viewerQuery.data ? 'attributed' : viewerFailure?.response?.status === 409 ? 'unresolved' : 'unavailable'
  const viewerProblem = viewerState === 'unavailable' ? viewerFailure?.problem.detail ?? '' : ''
  const viewerReadOnly = viewerQuery.isPending ? 'Resolving viewer identity…' : viewerQuery.data ? ''
    : viewerReadOnlyMessage(viewerFailure?.problem ?? { error: 'request failed', detail: 'unknown failure' }, viewerFailure?.response?.status)
  const streamProblems = useMemo<Record<string, string>>(() => ({
    ...stream.problems,
    ...(boardQuery.error ? { fleet: boardQuery.error.message } : {}),
  }), [boardQuery.error, stream.problems])

  const showQuickOpen = useCallback((groupID?: string) => {
    setQuickOpenGroup(groupID ?? apiRef.current?.activeGroup?.id)
    setQuickOpen(true)
  }, [])
  const toggleFleetRail = useCallback(() => {
    layout.setFleetRail((rail) => ({ ...rail, collapsed: !rail.collapsed }))
  }, [layout.setFleetRail])
  const toggleNotesRail = useCallback(() => {
    const opening = layout.notesRail.collapsed
    layout.setNotesRail((rail) => ({ ...rail, collapsed: !rail.collapsed }))
    if (opening) window.requestAnimationFrame(() => document.querySelector<HTMLElement>('.utility-rail-right')?.focus())
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
    board: boardQuery.data, mentionMatcher, identityReadOnly: viewerReadOnly, fileGitStates, folderSelectionHints, agentScreenPanes, agentStatuses, stream, streamProblems,
  }), [agentScreenPanes, agentStatuses, boardQuery.data, fileGitStates, folderSelectionHints, mentionMatcher, stream, streamProblems, viewerReadOnly])

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
    onDockReady, flushLayout: layout.flushLayout,
    stream, streamProblems, viewerProblem, viewer, viewerPending: viewerQuery.isPending,
  }
}
