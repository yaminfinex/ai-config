import { createContext, useCallback, useContext, type ReactNode } from 'react'
import { useQuery } from '@tanstack/react-query'
import { fleetQueryOptions } from '../../api/queries'
import { findAgentRow } from '../../shared/agentStatus'
import { agentUnread, useReadMarker } from '../spaces/index.ts'
import type { Board, FileTarget, FolderTarget } from '../../types'
import type { AgentMentionMatcher } from '../../shared/agentMentions'
import type { FileViewMode } from '../files/fileTabs'
import type { GitBase, GitFileState } from '../git/gitViewModel'
import type { OpenPlacement } from '../layout/openPlacement'
import type { DockPanelParams } from '../layout/dockLayout'
import type { SpaceDefinition } from '../spaces/spacesModel'
import type { QuickOpenMode } from '../files/quickOpenModel.ts'

export type WorkspaceActionsValue = {
  openAgent: (name: string, preview: boolean, placement?: OpenPlacement, focus?: boolean) => void
  openFile: (target: FileTarget, placement?: OpenPlacement) => void
  openFileInDiff: (target: FileTarget, base: GitBase, placement?: OpenPlacement) => void
  openChanges: (root: string, placement?: OpenPlacement) => void
  openFolder: (target: FolderTarget, placement?: OpenPlacement, selectionHint?: FileTarget) => void
  consumeFolderSelectionHint: (id: string) => void
  closePanel: (id: string) => void
  pinPanel: (id: string) => void
  setFileViewMode: (id: string, mode: FileViewMode) => void
  setFileGitState: (id: string, state: GitFileState) => void
  setAgentScreenPane: (name: string, paneID?: string) => void
  setAgentTailPane: (name: string, paneID?: string) => void
  onTerminalFocus: (paneID?: string) => void
  onViewer: (viewer: string) => void
  onAgentStatus: (name: string, status: string) => void
  resetLayout: () => void
  showQuickOpen: (groupID?: string, mode?: QuickOpenMode) => void
  sendPanelToSpace: (sourceID: string, params: DockPanelParams, spaceID: string) => boolean
  sendPanelToNewSpace: (sourceID: string, params: DockPanelParams) => boolean
  // markUnread marks an agent unread from entries[index] of its loaded
  // transcript window, or from the start of its latest turn.
  markUnread: (name: string, index?: number) => void
  // markRead marks agents read now, as a dwell read would.
  markRead: (names: readonly string[]) => void
}

// WorkspaceDataValue holds no fleet board: a fleet event changes the board,
// and a changed context value re-renders every dock panel and tab under it.
// Read the board through useFleetSelect instead.
export type WorkspaceDataValue = {
  mentionMatcher: AgentMentionMatcher
  identityReadOnly: string
  fileGitStates: Record<string, GitFileState>
  folderSelectionHints: Record<string, FileTarget>
  agentScreenPanes: Record<string, string>
  agentStatuses: Record<string, string>
  spaces: SpaceDefinition[]
  activeSpaceID: string | null
  activePanel: { id: string, params: DockPanelParams } | null
}

export const WorkspaceActionsContext = createContext<WorkspaceActionsValue | null>(null)
export const WorkspaceDataContext = createContext<WorkspaceDataValue | null>(null)

function required<T>(value: T | null) {
  if (!value) throw new Error('dock workspace context is unavailable')
  return value
}

export function useWorkspaceActionsContext() {
  return required(useContext(WorkspaceActionsContext))
}

export function useWorkspaceData() {
  return required(useContext(WorkspaceDataContext))
}

export function WorkspaceProviders({ actions, data, children }: { actions: WorkspaceActionsValue, data: WorkspaceDataValue, children: ReactNode }) {
  return <WorkspaceActionsContext.Provider value={actions}><WorkspaceDataContext.Provider value={data}>
    {children}
  </WorkspaceDataContext.Provider></WorkspaceActionsContext.Provider>
}

// useAgentUnread is whether an agent can be marked read: the menus and the
// footer offer "Mark read" in place of "Mark unread" while it is.
export function useAgentUnread(name: string): boolean {
  const row = useFleetSelect(useCallback((board: Board | undefined) => findAgentRow(board, name), [name]))
  return agentUnread(row, useReadMarker(name))
}

// useFleetSelect reads one slice of the fleet board and re-renders only when
// that slice changes, so a status change on one agent leaves the rest alone.
// The query's structural sharing keeps an unchanged slice's identity. Pass a
// stable select; it also answers for the board not yet loaded.
export function useFleetSelect<T>(select: (board: Board | undefined) => T): T {
  const query = useQuery({ ...fleetQueryOptions(), select })
  return query.isSuccess ? query.data : select(undefined)
}
