import { queryOptions, type QueryClient } from '@tanstack/react-query'
import { getEntries, getViewer, queryKeys, type Fetcher } from './client.ts'
import type { EntriesPage, TranscriptEntry } from '../types'

const entryWindowLimit = 500

function entryIdentity(entry: TranscriptEntry) {
  return entry.uuid || `offset:${entry.byteOffset}`
}

async function loadEntries(queryClient: QueryClient, name: string, fetcher?: Fetcher): Promise<EntriesPage> {
  const current = queryClient.getQueryData<EntriesPage>(queryKeys.entries(name))
  if (!current?.sessionId || current.nextOffset === undefined) return getEntries(name, { limit: entryWindowLimit }, fetcher)
  let offset = current.nextOffset
  let entries = current.entries ?? []
  let latest = current
  for (;;) {
    const page = await getEntries(name, { from: offset, limit: entryWindowLimit, sessionId: current.sessionId }, fetcher)
    if (page.reset) return getEntries(name, { limit: entryWindowLimit }, fetcher)
    const incoming = page.entries ?? []
    const seen = new Set(entries.map(entryIdentity))
    entries = [...entries, ...incoming.filter((entry) => !seen.has(entryIdentity(entry)))].slice(-entryWindowLimit)
    latest = { ...page, window: current.window, entries }
    const advanced = page.nextOffset ?? offset
    if (incoming.length < entryWindowLimit || advanced === offset) break
    offset = advanced
  }
  return latest
}

export function entriesQueryOptions(queryClient: QueryClient, name: string, fetcher?: Fetcher) {
  return queryOptions({
    queryKey: queryKeys.entries(name),
    queryFn: () => loadEntries(queryClient, name, fetcher),
    staleTime: Infinity,
    retry: false,
  })
}

export function viewerQueryOptions(fetcher?: Fetcher, retryDelay: (attemptIndex: number) => number = (attemptIndex) => Math.min(250 * 2 ** attemptIndex, 2_000)) {
  return queryOptions({
    queryKey: queryKeys.viewer,
    queryFn: () => getViewer(fetcher),
    staleTime: Infinity,
    retry: 3,
    retryDelay,
  })
}
