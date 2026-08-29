import type { GitBranchBase, GitStatusEntriesBase, GitStatusEntry } from '../../types'
import type { GitBase } from './gitViewModel'

export function changesPanelID(root: string) {
  return `changes:${encodeURIComponent(root)}`
}

export function branchBaseAvailable(branchBase: GitBranchBase) {
  return branchBase.status === 'available'
}

export function requestedChangesBase(choice: GitBase | null): GitBase {
  return choice ?? 'branch'
}

export function effectiveChangesBase(base: Pick<GitStatusEntriesBase, 'kind'> | undefined): GitBase {
  return base?.kind === 'branch' ? 'branch' : 'uncommitted'
}

export function changeSideLabel(entry: Pick<GitStatusEntry, 'kind' | 'staged' | 'unstaged'>, base: GitBase) {
  const sides = [entry.staged ? 'staged' : '', entry.unstaged ? 'unstaged' : ''].filter(Boolean).join(' + ')
  if (sides) return sides
  return base === 'branch' && entry.kind !== 'untracked' ? 'committed' : ''
}

export function branchChangeSummary(commits: number | undefined, files: number) {
  const fileLabel = `${files} changed ${files === 1 ? 'file' : 'files'} vs merge-base`
  return commits === undefined ? fileLabel : `${commits} ${commits === 1 ? 'commit' : 'commits'} ahead; ${fileLabel}`
}

export function entryChangeCount(entry: { additions?: number, deletions?: number }) {
  return entry.additions === undefined || entry.deletions === undefined ? '' : `+${entry.additions} / −${entry.deletions}`
}
