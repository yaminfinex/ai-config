import assert from 'node:assert/strict'
import test from 'node:test'

import { historyPagingState } from '../src/features/files/historyPaging.ts'
import type { GitLogRead } from '../src/types.ts'

function page(shas: string[], options: { next?: string, truncated?: boolean } = {}): GitLogRead {
  return {
    root: '/repo',
    path: 'src/App.tsx',
    entries: shas.map((sha) => ({ sha, author: 'Fixture', date: '2026-08-30T00:00:00Z', subject: sha, path_then: 'src/App.tsx' })),
    ...(options.next ? { next_cursor: options.next } : {}),
    ...(options.truncated ? { history_truncated: true } : {}),
    fetched_at: '2026-08-30T00:00:00Z',
  }
}

test('older Git history pages append without moving already visible rows', () => {
  const first = historyPagingState([page(['newest', 'newer'], { next: 'page-2' })])
  const accumulated = historyPagingState([
    page(['newest', 'newer'], { next: 'page-2' }),
    page(['older', 'oldest']),
  ])

  assert.deepEqual(first.entries.map((entry) => entry.sha), ['newest', 'newer'])
  assert.deepEqual(accumulated.entries.map((entry) => entry.sha), ['newest', 'newer', 'older', 'oldest'])
  assert.deepEqual(accumulated.entries.slice(0, first.entries.length), first.entries)
})

test('the last anchored page alone controls whether more history is available', () => {
  assert.deepEqual(historyPagingState([page(['newest'], { next: 'page-2' })]), {
    entries: page(['newest']).entries,
    nextCursor: 'page-2',
    end: 'more',
  })
  assert.equal(historyPagingState([page(['only'])]).end, 'beginning')
  assert.equal(historyPagingState([page(['thousandth'], { truncated: true })]).end, 'truncated')
})

test('an empty history has an honest beginning state', () => {
  assert.deepEqual(historyPagingState([page([])]), { entries: [], end: 'beginning' })
})
