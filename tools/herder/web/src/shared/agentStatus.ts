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
  if (!board) return '-'
  for (const workspace of board.workspaces) {
    for (const tab of workspace.tabs) {
      for (const pane of tab.panes) {
        const status = rowBusStatus(pane, name)
        if (status) return status
      }
    }
  }
  for (const row of board.unplaced) {
    const status = rowBusStatus(row, name)
    if (status) return status
  }
  return '-'
}

function rowBusStatus(row: Row, name: string): string | undefined {
  if (row.agent === name) return row.bus_status
  for (const child of row.subagents ?? []) {
    const status = rowBusStatus(child, name)
    if (status) return status
  }
  return undefined
}
