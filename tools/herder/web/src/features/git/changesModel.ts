import type { GitBranchBase } from '../../types'

export function changesPanelID(root: string) {
  return `changes:${encodeURIComponent(root)}`
}

export function branchBaseAvailable(branchBase: GitBranchBase) {
  return branchBase.status === 'available'
}

export function entryChangeCount(entry: { additions?: number, deletions?: number }) {
  return entry.additions === undefined || entry.deletions === undefined ? '' : `+${entry.additions} / −${entry.deletions}`
}
