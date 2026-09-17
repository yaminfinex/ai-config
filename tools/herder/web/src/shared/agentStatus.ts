import type { Board } from '../types.ts'
import type { Row } from '../types.ts'

export type AgentStatusPresentation = {
  className: 'active' | 'listening' | 'blocked' | 'retired' | 'unknown'
  label: string
  meaning: string
}

// Bus status is the honest agent lifecycle signal: active is working now,
// listening is available and waiting, and blocked cannot proceed.
export function agentStatusPresentation(status: string): AgentStatusPresentation {
  if (status === 'active') return { className: 'active', label: 'active', meaning: 'agent is currently working' }
  if (status === 'listening') return { className: 'listening', label: 'listening', meaning: 'agent is available and waiting' }
  if (status === 'blocked') return { className: 'blocked', label: 'blocked', meaning: 'agent cannot proceed' }
  if (status === 'retired') return { className: 'retired', label: 'retired', meaning: 'agent has stopped; transcript is read-only' }
  return { className: 'unknown', label: status && status !== '-' ? status : 'unknown', meaning: 'agent status is unavailable' }
}

export function agentBusStatus(board: Board | undefined, name: string): string {
  return findAgentRow(board, name)?.bus_status ?? '-'
}

export function agentBoardTool(board: Board | undefined, name: string): string {
  return findAgentRow(board, name)?.tool ?? ''
}

export function findAgentRow(board: Board | undefined, name: string): Row | undefined {
  if (!board) return undefined
  for (const workspace of board.workspaces) {
    for (const tab of workspace.tabs) {
      for (const pane of tab.panes) {
        const row = findRow(pane, name)
        if (row) return row
      }
    }
  }
  for (const candidate of board.unplaced) {
    const row = findRow(candidate, name)
    if (row) return row
  }
}

function findRow(row: Row, name: string): Row | undefined {
  if (row.agent === name) return row
  for (const child of row.subagents ?? []) {
    const match = findRow(child, name)
    if (match) return match
  }
}

// Terse per-tool marker for tab bars and headers; empty when unknown so
// callers can omit the badge instead of showing a lying placeholder.
export function agentToolBadge(tool: string | undefined): string {
  if (!tool || tool === '-') return ''
  if (tool === 'claude') return 'cl'
  if (tool === 'codex') return 'cx'
  return tool.slice(0, 2)
}
