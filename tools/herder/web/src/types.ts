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
  model?: string
  context_usage?: {
    used_tokens: number
    input_tokens: number
    cached_input_tokens?: number
    cache_creation_input_tokens?: number
    cache_read_input_tokens?: number
    output_tokens?: number
    window_tokens?: number
    used_percent?: number
  }
  queued?: QueuedMessage[]
}

export interface QueuedMessage {
  id: number
  sender: string
  intent?: string
  preview: string
  sent_at: string
  operator?: boolean
}

export type EntryKind =
  | 'human_prompt'
  | 'hcom_delivery_stub'
  | 'hcom_delivery'
  | 'task_notification'
  | 'injected_system'
  | 'command_stdout'
  | 'compact_divider'
  | 'assistant_text'
  | 'thinking'
  | 'tool_use'
  | 'tool_result'
  | 'turn_duration'
  | 'system_chip'
  | 'unknown'

export interface TranscriptEntry {
  uuid?: string
  line: number
  byteOffset: number
  timestamp?: string
  kind: EntryKind
  payload: unknown
  quarantine?: { reason: string }
}

export interface EntriesPage {
  sessionId: string
  window: { mode: 'from' | 'tail', from: number, limit: number }
  entries?: TranscriptEntry[]
  nextOffset?: number
  reset?: {
    reason: 'truncated' | 'session_changed'
    previous_session_id?: string
    session_id: string
    previous_offset: number
  }
  stats?: { sidechainSkipped: number }
}
