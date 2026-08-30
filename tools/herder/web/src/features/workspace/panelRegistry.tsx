import { useEffect, useState, type FunctionComponent } from 'react'
import type { QueryClient } from '@tanstack/react-query'
import type { IDockviewPanelHeaderProps, IDockviewPanelProps } from 'dockview-react'
import { queryKeys } from '../../api/client'
import { AgentStatusDot } from '../../shared/presentation'
import { agentBusStatus } from '../../shared/agentStatus'
import { PanelState } from '../../shared/PanelState'
import type { Board } from '../../types'
import { AgentPanel } from '../transcript/AgentPanel'
import { ScreenPanel } from '../screen/ScreenPanel'
import { FilePanel } from '../files/FilePanel'
import { FolderPanel } from '../folders/FolderPanel'
import { ChangesPanel } from '../git/ChangesPanel'
import { initialGitFileState } from '../git/gitViewModel'
import { placementInGroup } from '../layout/openPlacement'
import { screenIdentityState, type AgentPanelParams, type ChangesPanelParams, type DockPanelParams, type FilePanelParams, type FolderPanelParams, type ScreenPanelParams } from '../layout/dockLayout'
import { useWorkspace } from './workspaceContext'
import { mergePanelParams, panelID, panelParams, panelPresentation, panelUsesQuickOpenGroup, previewPanelToReplace, type PanelKind } from './panelRegistryModel'

function usePanelVisibility(api: IDockviewPanelProps['api']) {
  const [visible, setVisible] = useState(api.isVisible)
  useEffect(() => {
    setVisible(api.isVisible)
    const disposable = api.onDidVisibilityChange((event) => setVisible(event.isVisible))
    return () => disposable.dispose()
  }, [api])
  return visible
}

function visiblePane(board: Board | undefined, params: ScreenPanelParams) {
  if (screenIdentityState(params, board) !== 'ready') return undefined
  for (const workspace of board?.workspaces ?? []) {
    for (const tab of workspace.tabs) {
      const pane = tab.panes.find((candidate) => candidate.pane_id === params.identity.paneID)
      if (pane) return pane
    }
  }
}

function AgentDockPanel({ params, api }: IDockviewPanelProps<AgentPanelParams>) {
  const workspace = useWorkspace()
  const visible = usePanelVisibility(api)
  return <AgentPanel name={params.name} active={visible} liveStatus={agentBusStatus(workspace.board, params.name)} screenPaneID={workspace.agentScreenPanes[params.name]}
    mentionMatcher={workspace.mentionMatcher} onOpenAgent={(name, placement) => workspace.openAgent(name, true, placementInGroup(placement, api.group.id), true)}
    onScreenPane={(paneID) => workspace.setAgentScreenPane(params.name, paneID)} onOpenFile={(target, placement) => workspace.openFile(target, placementInGroup(placement, api.group.id))}
    onOpenFolder={(target, placement) => workspace.openFolder(target, placementInGroup(placement, api.group.id))}
    onOpenChanges={(root, placement) => workspace.openChanges(root, placementInGroup(placement, api.group.id))}
    identityReadOnly={workspace.identityReadOnly} onViewer={workspace.onViewer} onSend={() => workspace.pinPanel(api.id)} onStatus={workspace.onAgentStatus}
    onTerminalFocus={workspace.onTerminalFocus} />
}

function ScreenDockPanel({ params, api }: IDockviewPanelProps<ScreenPanelParams>) {
  const workspace = useWorkspace()
  const visible = usePanelVisibility(api)
  const identity = screenIdentityState(params, workspace.board)
  if (identity === 'checking') return <PanelState as="main" className="panel-unavailable" title="Verifying screen identity…" detail="The live fleet must confirm this saved pane before it can be subscribed." />
  const pane = visiblePane(workspace.board, params)
  if (!pane) return <PanelState as="main" className="panel-unavailable tombstone" title="Screen no longer matches" detail="The saved pane identity is gone or now belongs to different live evidence. No replacement pane was opened." />
  return <ScreenPanel pane={pane} active={visible} onFocus={workspace.onTerminalFocus} onBlur={() => workspace.onTerminalFocus(undefined)} />
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

type PanelDescriptor = {
  component: FunctionComponent<IDockviewPanelProps>
  invalidate: (queryClient: QueryClient, params: DockPanelParams) => void
}

const noInvalidation = () => undefined
export const panelRegistry: Record<PanelKind, PanelDescriptor> = {
  agent: { component: AgentDockPanel as FunctionComponent<IDockviewPanelProps>, invalidate: noInvalidation },
  screen: { component: ScreenDockPanel as FunctionComponent<IDockviewPanelProps>, invalidate: noInvalidation },
  file: {
    component: FileDockPanel as FunctionComponent<IDockviewPanelProps>,
    invalidate: (queryClient, params) => { if (params.kind === 'file') queryClient.invalidateQueries({ queryKey: queryKeys.file(params.root, params.path) }) },
  },
  folder: {
    component: FolderDockPanel as FunctionComponent<IDockviewPanelProps>,
    invalidate: (queryClient, params) => {
      if (params.kind !== 'folder') return
      queryClient.invalidateQueries({ queryKey: queryKeys.fileTree(params.root, params.path) })
      queryClient.invalidateQueries({ queryKey: queryKeys.backlog(params.root, params.path) })
    },
  },
  changes: {
    component: ChangesDockPanel as FunctionComponent<IDockviewPanelProps>,
    invalidate: (queryClient, params) => { if (params.kind === 'changes') queryClient.invalidateQueries({ queryKey: queryKeys.gitStatus(params.root) }) },
  },
}

export const dockComponents: Record<string, FunctionComponent<IDockviewPanelProps>> = Object.fromEntries(
  Object.entries(panelRegistry).map(([kind, descriptor]) => [kind, descriptor.component]),
)

export function invalidatePanel(queryClient: QueryClient, params: DockPanelParams) {
  panelRegistry[params.kind].invalidate(queryClient, params)
}

export function DockTab({ params, api }: IDockviewPanelHeaderProps<DockPanelParams>) {
  const workspace = useWorkspace()
  const presentation = panelPresentation(params)
  const boardStatus = params.kind === 'agent' ? agentBusStatus(workspace.board, params.name) : '-'
  const status = params.kind === 'agent' && boardStatus === '-' ? workspace.agentStatuses[params.name] ?? '-' : boardStatus
  const meta = params.kind === 'agent' ? status !== '-' ? status : presentation.meta : presentation.meta
  return <div className={`herder-dock-tab${params.preview ? ' preview' : ''}`} title={params.preview ? 'Preview — double-click to pin' : undefined}
    onDoubleClick={(event) => { if (params.preview) workspace.pinPanel(api.id); event.stopPropagation() }}
    onAuxClick={(event) => { if (event.button === 1) api.close() }}>
    <span className="dock-tab-label">{params.preview && <span className="preview-dot" aria-hidden="true" />}{presentation.icon}{presentation.title}</span>
    {meta && <span className="dock-tab-meta">{params.kind === 'agent' && <AgentStatusDot status={status} />}{meta}</span>}
    <button type="button" className="dock-tab-close" aria-label={`Close ${presentation.title}`} onPointerDown={(event) => event.stopPropagation()} onClick={() => api.close()}>×</button>
  </div>
}

export { mergePanelParams, panelID, panelParams, panelPresentation, panelUsesQuickOpenGroup, previewPanelToReplace }
