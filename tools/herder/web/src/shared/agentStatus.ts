import type { Board } from '../types.ts'

export type AgentStatusPresentation = {
  className: 'active' | 'listening' | 'blocked' | 'unknown'
  label: string
  meaning: string
}

// Bus status is the honest agent lifecycle signal: active is working now,
// listening is available and waiting, and blocked cannot proceed.
export function agentStatusPresentation(status: string): AgentStatusPresentation {
  if (status === 'active') return { className: 'active', label: 'active', meaning: 'agent is currently working' }
  if (status === 'listening') return { className: 'listening', label: 'listening', meaning: 'agent is available and waiting' }
  if (status === 'blocked') return { className: 'blocked', label: 'blocked', meaning: 'agent cannot proceed' }
  return { className: 'unknown', label: status && status !== '-' ? status : 'unknown', meaning: 'agent status is unavailable' }
}

export function agentBusStatus(board: Board | undefined, name: string): string {
  if (!board) return '-'
  for (const workspace of board.workspaces) {
    for (const tab of workspace.tabs) {
      const pane = tab.panes.find((candidate) => candidate.agent === name)
      if (pane) return pane.bus_status
    }
  }
  return board.unplaced.find((candidate) => candidate.agent === name)?.bus_status ?? '-'
}
