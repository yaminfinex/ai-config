import { createContext, useContext, type ReactNode } from 'react'
import type { Board, FileTarget, FolderTarget } from '../../types'
import type { AgentMentionMatcher } from '../../shared/agentMentions'
import type { FileViewMode } from '../files/fileTabs'
import type { GitBase, GitFileState } from '../git/gitViewModel'
import type { OpenPlacement } from '../layout/openPlacement'
import type { DockPanelParams } from '../layout/dockLayout'
import type { SpaceDefinition } from '../spaces/spacesModel'

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
  onTerminalFocus: (paneID?: string) => void
  onViewer: (viewer: string) => void
  onAgentStatus: (name: string, status: string) => void
  resetLayout: () => void
  showQuickOpen: (groupID?: string) => void
  sendPanelToSpace: (sourceID: string, params: DockPanelParams, spaceID: string) => boolean
  sendPanelToNewSpace: (sourceID: string, params: DockPanelParams) => boolean
}

export type WorkspaceDataValue = {
  board?: Board
  mentionMatcher: AgentMentionMatcher
  identityReadOnly: string
  fileGitStates: Record<string, GitFileState>
  folderSelectionHints: Record<string, FileTarget>
  agentScreenPanes: Record<string, string>
  agentStatuses: Record<string, string>
  spaces: SpaceDefinition[]
  activeSpaceID: string | null
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
