export interface Row {
  pane_id: string
  agent: string
  tool: string
  herdr_status: string
  bus_status: string
  gap: string
  parent_agent?: string
  subagents?: Row[]
}

export interface Pane extends Row {
  label?: string
  current_command?: string
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

export interface ScreenFrame {
  pane_id: string
  revision?: number
  status: 'available' | 'unavailable'
  text: string
  truncated: boolean
  detail?: string
  cols?: number
  rows?: number
}

export interface PaneHistory {
  pane_id: string
  text: string
  truncated: boolean
  fetched_at: string
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
  cwd?: string
  git?: {
    branch?: string
    remote_url?: string
    worktree_of?: string
  }
  session_id?: string
  parent_agent?: string
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

export type MatchTier = 'exact' | 'prefix' | 'suffix' | 'fuzzy'

export interface FileCandidate {
  root: string
  path: string
  kind: 'file' | 'dir'
  tier: MatchTier
  score: number
}

export interface RootOutcome {
  root: string
  status: 'complete' | 'degraded' | 'failed'
  detail?: string
}

export interface ResolveResponse {
  candidates: FileCandidate[]
  roots: RootOutcome[]
}

export type FileRead = {
  root: string
  path: string
  binary: true
  size: number
  fetched_at: string
} | {
  root: string
  path: string
  content: string
  binary: false
  size: number
  truncated: boolean
  fetched_at: string
}

export type GitBranchBase = {
  status: 'available'
  default_ref: string
  default_sha: string
  merge_base: string
  commits_ahead_of_base?: number
} | {
  status: 'unavailable'
  reason: string
}

export interface GitStatusEntry {
  path: string
  kind: 'modified' | 'added' | 'deleted' | 'renamed' | 'copied' | 'untracked' | 'conflicted' | 'type_changed'
  old_path?: string
  staged: boolean
  unstaged: boolean
  index_kind?: string
  worktree_kind?: string
  additions?: number
  deletions?: number
  binary?: boolean
}

export type GitStatusEntriesBase = {
  kind: 'uncommitted' | 'branch'
  sha: string
  default_ref?: string
  label: string
}

export type GitStatusRead = {
  root: string
  repo: {
    branch?: string
    head?: string
    upstream?: string
    ahead?: number
    behind?: number
    branch_base: GitBranchBase
  }
  entries: GitStatusEntry[]
  entries_base?: GitStatusEntriesBase
  fetched_at: string
} | {
  root: string
  git: { status: 'unavailable', reason: string }
  fetched_at: string
}

export interface GitDiffRead {
  root: string
  path: string
  base: { kind: 'uncommitted' | 'branch' | 'commit', sha: string, default_ref?: string, label: string }
  facts: {
    kind: 'unchanged' | 'modified' | 'added' | 'deleted' | 'renamed' | 'copied' | 'type_changed'
    old_path?: string
    binary: boolean
    old_mode?: string
    new_mode?: string
  }
  stats?: { additions: number, deletions: number }
  patch: string
  patch_bytes: number
  truncated: boolean
  fetched_at?: string
}

export interface GitLogEntry {
  sha: string
  author: string
  date: string
  subject: string
  path_then: string
}

export interface GitLogRead {
  root: string
  path: string
  entries: GitLogEntry[]
  next_cursor?: string
  fetched_at: string
}

export type GitFileRead = {
  root: string
  path: string
  sha: string
  binary: true
  size: number
} | {
  root: string
  path: string
  sha: string
  content: string
  binary: false
  size: number
  truncated: boolean
}

export interface FileTarget {
  root: string
  path: string
  line?: number
}

export interface FolderTarget {
  root: string
  path: string
}

export interface FileTreeEntry {
  name: string
  kind: 'file' | 'directory' | 'symlink'
  size?: number
}

export interface FileTreeRead {
  root: string
  path: string
  entries: FileTreeEntry[]
}

export interface BacklogTask {
  id?: string
  title?: string
  status?: string
  ordinal?: number
  labels?: string[]
  priority?: string
  assignee?: string[]
  created_date?: string
  updated_date?: string
  file: string
}

export interface BacklogRead {
  root: string
  path: string
  statuses: string[]
  tasks: BacklogTask[]
  unparsed: Array<{ file: string, reason: string }>
  truncated: boolean
  fetched_at: string
}

export interface BacklogUnavailable {
  root: string
  path: string
  backlog: { status: 'unavailable', reason: string }
  fetched_at: string
}

export type BacklogResponse = BacklogRead | BacklogUnavailable

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
