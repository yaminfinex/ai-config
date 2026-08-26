export interface Row {
  pane_id: string
  agent: string
  tool: string
  herdr_status: string
  bus_status: string
  gap: string
}

export interface Pane extends Row {
  label?: string
  agent_session?: string
}

export interface Tab {
  tab_id: string
  number: number
  label: string
  focused: boolean
  pane_count: number
  agent_status: string
  panes: Pane[]
}

export interface Workspace {
  workspace_id: string
  worktree_of?: string
  number: number
  label: string
  focused: boolean
  pane_count: number
  tab_count: number
  active_tab_id: string
  agent_status: string
  tabs: Tab[]
}

export interface Board {
  workspaces: Workspace[]
  unplaced: Row[]
}

export interface SubstrateEvent {
  source: string
  status: 'unreachable' | 'recovered'
  detail?: string
}

export interface Refusal {
  error: string
  detail: string
}

export interface LifecycleResult {
  name: string
  pane: string
}

export interface AgentDetail {
  name: string
  tool: string
  herdr_status: string
  bus_status: string
  gap: string
  pane: { workspace_id: string, tab_id: string, pane_id: string } | null
  directory?: string
  session_id?: string
  launch_context: Record<string, unknown>
}

export interface TranscriptExchange {
  position: number
  [key: string]: unknown
}

export interface TranscriptPage {
  exchanges: TranscriptExchange[]
  cursor: string
}
