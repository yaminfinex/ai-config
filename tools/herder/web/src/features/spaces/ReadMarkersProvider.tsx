import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, useSyncExternalStore, type ReactNode } from 'react'
import { useDOMEvent } from '../../shared/lifecycle'
import type { GenericStateRow } from '../../shared/stateSync.ts'
import type { ReadMarker, ReadMarkers } from './readMarkerModel.ts'
import { createReadMarkerStore, readMarkerRowsKey, type ReadMarkerStore } from './readMarkerStore.ts'
import { browserReadMarkersTransport, createReadMarkersSync, createReadMarkersSyncPersistence, readMarkerStoreSyncAdapter } from './readMarkerSync.ts'

type ReadMarkersContextValue = {
  store: ReadMarkerStore
  // pulled turns true once the first pull has settled (or failed), so a
  // fresh browser seeds only agents the server has no marker for.
  pulled: boolean
  stateChanged: (namespace: string, rev: number) => void
  syncProblem: string
}

const ReadMarkersContext = createContext<ReadMarkersContextValue | null>(null)

// ReadMarkersProvider holds the read.markers rows: what the owner has read
// of each agent, shared by every browser through /api/state.
export function ReadMarkersProvider({ children }: { children: ReactNode }) {
  const [store] = useState(() => createReadMarkerStore())
  const [pulled, setPulled] = useState(false)
  const [syncProblem, setSyncProblem] = useState('')
  const syncRef = useRef<ReturnType<typeof createReadMarkersSync> | null>(null)

  useEffect(() => {
    let persistence
    try { persistence = createReadMarkersSyncPersistence(window.localStorage) } catch { setPulled(true); return }
    const sync = createReadMarkersSync({
      store: readMarkerStoreSyncAdapter(store),
      persistence,
      transport: browserReadMarkersTransport(),
      retry: (callback, delay) => window.setTimeout(callback, delay),
      cancelRetry: (handle) => window.clearTimeout(handle as number),
      onProblem: setSyncProblem,
    })
    syncRef.current = sync
    void sync.start().finally(() => setPulled(true))
    const online = () => { void sync.retryNow() }
    window.addEventListener('online', online)
    return () => {
      window.removeEventListener('online', online)
      sync.dispose()
      if (syncRef.current === sync) syncRef.current = null
    }
  }, [store])
  // Another tab of this browser read or marked something.
  useDOMEvent<StorageEvent>(window, 'storage', (event) => {
    if (event.key !== readMarkerRowsKey || !event.newValue) return
    try {
      const rows: unknown = JSON.parse(event.newValue)
      if (Array.isArray(rows)) store.merge(rows as GenericStateRow[])
    } catch { /* the server still carries the rows */ }
  })
  const stateChanged = useCallback((namespace: string, rev: number) => { void syncRef.current?.stateChanged(namespace, rev) }, [])
  const value = useMemo(() => ({ store, pulled, stateChanged, syncProblem }), [pulled, stateChanged, store, syncProblem])
  return <ReadMarkersContext.Provider value={value}>{children}</ReadMarkersContext.Provider>
}

export function useReadMarkersContext() {
  const value = useContext(ReadMarkersContext)
  if (!value) throw new Error('read markers context is unavailable')
  return value
}

export function useReadMarkers(): ReadMarkers {
  const { store } = useReadMarkersContext()
  return useSyncExternalStore(store.subscribe, store.markers, store.markers)
}

export function useReadMarker(name: string): ReadMarker | undefined {
  return useReadMarkers()[name]
}
