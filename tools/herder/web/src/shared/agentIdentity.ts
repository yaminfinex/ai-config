import type { Board, Row } from '../types.ts'

export function bareHcomName(name: string) {
  return name.split('-').at(-1) ?? ''
}

export function agentHeaderIdentity(name: string): { primary: string, secondary?: string } {
  const primary = bareHcomName(name)
  return primary === name ? { primary } : { primary, secondary: name }
}

export function agentBoardTitle(board: Board | undefined, name: string): string {
  return findAgentRow(board, name)?.title || name
}

function findAgentRow(board: Board | undefined, name: string): Row | undefined {
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
