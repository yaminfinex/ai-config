import type { GitLogEntry, GitLogRead } from '../../types'

export type HistoryPagingState = {
  entries: GitLogEntry[]
  nextCursor?: string
  end: 'more' | 'beginning' | 'truncated'
}

export function historyPagingState(pages: GitLogRead[] | undefined): HistoryPagingState {
  const entries = pages?.flatMap((page) => page.entries) ?? []
  const lastPage = pages?.at(-1)
  if (lastPage?.next_cursor) return { entries, nextCursor: lastPage.next_cursor, end: 'more' }
  return { entries, end: lastPage?.history_truncated ? 'truncated' : 'beginning' }
}
