import type { Board } from '../types.ts'
import { findAgentRow } from './agentStatus.ts'

export function bareHcomName(name: string) {
  return name.split('-').at(-1) || name
}

export function agentHeaderIdentity(name: string): { primary: string, secondary?: string } {
  const primary = bareHcomName(name)
  return primary === name ? { primary } : { primary, secondary: name }
}

export function agentBoardTitle(board: Board | undefined, name: string): string {
  return findAgentRow(board, name)?.title || name
}
