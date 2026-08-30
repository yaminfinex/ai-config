import { useCallback, useEffect, useRef, type MutableRefObject } from 'react'
import type { QueryClient } from '@tanstack/react-query'
import type { DockviewApi } from 'dockview-react'
import { focusComposerWhenReady } from '../../composerState'
import type { Board, FileTarget, FolderTarget, Pane } from '../../types'
import { fileTabID, isMarkdownPath, type FileViewMode } from '../files/fileTabs'
import { gitStateForFileOpen, type GitBase, type GitFileState } from '../git/gitViewModel'
import { dockOpenTarget, type OpenPlacement } from '../layout/openPlacement'
import { screenPanelParams, type DockPanelParams } from '../layout/dockLayout'
import type { PanelRecordUpdate } from './usePanelRecords'
import {
  invalidatePanel,
  mergePanelParams,
  panelID,
  panelParams,
  panelPresentation,
  panelUsesQuickOpenGroup,
  previewPanelToReplace,
} from './panelRegistry'

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

export function panelFromAPI(api: DockviewApi, id: string) {
  const panel = api.getPanel(id)
  const params = panelParams(panel?.params)
  return panel && params ? { panel, params } : null
}

type WorkspaceActionOptions = {
  apiRef: MutableRefObject<DockviewApi | undefined>
  board?: Board
  queryClient: QueryClient
  quickOpenGroup?: string
  setQuickOpenGroup: (groupID?: string) => void
  syncDock: () => void
  setFileGitState: (id: string, update: PanelRecordUpdate<GitFileState>) => void
  resetPersistedLayout: () => void
}

export function useWorkspaceActions({
  apiRef,
  board,
  queryClient,
  quickOpenGroup,
  setQuickOpenGroup,
  syncDock,
  setFileGitState,
  resetPersistedLayout,
}: WorkspaceActionOptions) {
  const composerFocusCancel = useRef<() => void>(() => undefined)
  const boardRef = useRef(board)
  const quickOpenGroupRef = useRef(quickOpenGroup)
  boardRef.current = board
  quickOpenGroupRef.current = quickOpenGroup

  const focusComposer = useCallback(() => {
    composerFocusCancel.current()
    composerFocusCancel.current = focusComposerWhenReady(
      () => document.querySelector<HTMLTextAreaElement>('.dv-active-group textarea[data-composer]'),
      requestAnimationFrame,
      20,
      cancelAnimationFrame,
    )
  }, [])
  useEffect(() => () => composerFocusCancel.current(), [])

  const openPanel = useCallback((params: DockPanelParams, placement?: OpenPlacement, focus = false) => {
    const api = apiRef.current
    if (!api) return undefined
    const id = panelID(params)
    const target = dockOpenTarget(api.getPanel(id), placement, dockGroupFacts(api))
    if (target.kind === 'existing') {
      const current = panelParams(target.panel.params)
      target.panel.api.updateParameters(current ? mergePanelParams(current, params) : params)
      target.panel.api.setActive()
      invalidatePanel(queryClient, params)
      syncDock()
      if (focus) focusComposer()
      return 'existing' as const
    }
    const requestedPlacement = !placement && panelUsesQuickOpenGroup(params) && quickOpenGroupRef.current
      ? { direction: 'within' as const, groupID: quickOpenGroupRef.current }
      : placement
    const newTarget = requestedPlacement === placement
      ? target
      : dockOpenTarget(undefined, requestedPlacement, dockGroupFacts(api))
    const group = newTarget.groupID ? api.getGroup(newTarget.groupID) : undefined
    const replaced = params.preview ? previewPanelToReplace(group?.panels ?? [], params.kind) : undefined
    const presentation = panelPresentation(params)
    api.addPanel({
      id,
      component: params.kind,
      tabComponent: 'herder-tab',
      title: presentation.title,
      params,
      ...(newTarget.position ? { position: newTarget.position } : {}),
    })
    if (replaced) api.removePanel(replaced)
    invalidatePanel(queryClient, params)
    if (panelUsesQuickOpenGroup(params)) setQuickOpenGroup(undefined)
    syncDock()
    if (focus) focusComposer()
    return 'new' as const
  }, [apiRef, focusComposer, queryClient, setQuickOpenGroup, syncDock])

  const openAgent = useCallback((name: string, preview: boolean, placement?: OpenPlacement, focus = false) => {
    openPanel({ kind: 'agent', name, preview }, placement, focus)
  }, [openPanel])

  const openScreen = useCallback((pane: Pane, preview: boolean, placement?: OpenPlacement) => {
    if (!boardRef.current) return
    const params = screenPanelParams(boardRef.current, pane, preview)
    if (params) openPanel(params, placement)
  }, [openPanel])

  const openFile = useCallback((target: FileTarget, placement?: OpenPlacement) => {
    const id = fileTabID(target.root, target.path)
    const result = openPanel({
      kind: 'file', root: target.root, path: target.path,
      ...(target.line ? { line: target.line } : {}), preview: true,
      viewMode: isMarkdownPath(target.path) && !target.line ? 'rendered' : 'source',
    }, placement)
    if (result === 'existing' && target.line) setFileGitState(id, (current) => gitStateForFileOpen(current, target.line as number))
  }, [openPanel, setFileGitState])

  const openFileInDiff = useCallback((target: FileTarget, base: GitBase, placement?: OpenPlacement) => {
    openFile(target, placement)
    setFileGitState(fileTabID(target.root, target.path), { mode: 'diff', base })
  }, [openFile, setFileGitState])

  const openChanges = useCallback((root: string, placement?: OpenPlacement) => {
    openPanel({ kind: 'changes', root, preview: true }, placement)
  }, [openPanel])

  const openFolder = useCallback((target: FolderTarget, placement?: OpenPlacement) => {
    openPanel({ kind: 'folder', root: target.root, path: target.path, preview: true }, placement)
  }, [openPanel])

  const pinPanel = useCallback((id: string) => {
    const api = apiRef.current
    const current = api ? panelFromAPI(api, id) : null
    if (!current?.params.preview) return
    current.panel.api.updateParameters({ ...current.params, preview: false })
    syncDock()
  }, [apiRef, syncDock])

  const setFileViewMode = useCallback((id: string, viewMode: FileViewMode) => {
    const api = apiRef.current
    const current = api ? panelFromAPI(api, id) : null
    if (current?.params.kind !== 'file' || current.params.viewMode === viewMode) return
    current.panel.api.updateParameters({ ...current.params, viewMode })
    syncDock()
  }, [apiRef, syncDock])

  const resetLayout = useCallback(() => {
    const api = apiRef.current
    if (!api) return
    api.clear()
    resetPersistedLayout()
    syncDock()
  }, [apiRef, resetPersistedLayout, syncDock])

  return { openPanel, openAgent, openScreen, openFile, openFileInDiff, openChanges, openFolder, pinPanel, setFileViewMode, resetLayout }
}
