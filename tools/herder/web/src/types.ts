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
