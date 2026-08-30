import { createContext, useContext } from 'react'
import type { Board, FileTarget, FolderTarget } from '../../types'
import type { StreamState } from '../../stream/useFleetStream'
import type { AgentMentionMatcher } from '../../shared/agentMentions'
import type { FileViewMode } from '../files/fileTabs'
import type { GitBase, GitFileState } from '../git/gitViewModel'
import type { OpenPlacement } from '../layout/openPlacement'

export type WorkspaceContextValue = {
  board?: Board
  mentionMatcher: AgentMentionMatcher
  identityReadOnly: string
  openAgent: (name: string, preview: boolean, placement?: OpenPlacement, focus?: boolean) => void
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
  onTerminalFocus: (paneID?: string) => void
  onViewer: (viewer: string) => void
  onAgentStatus: (name: string, status: string) => void
  agentStatuses: Record<string, string>
  resetLayout: () => void
  showQuickOpen: (groupID?: string) => void
  stream: StreamState
  streamProblems: Record<string, string>
}

export const WorkspaceContext = createContext<WorkspaceContextValue | null>(null)

export function useWorkspace() {
  const value = useContext(WorkspaceContext)
  if (!value) throw new Error('dock workspace context is unavailable')
  return value
}
