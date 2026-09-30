import { useCallback, useEffect, useRef, useState, type MutableRefObject } from 'react'
import type { DockviewApi } from 'dockview-react'
import { writePanelToStoredSpace } from '../layout/dockLayout'
import {
  browserSpaceMembersTransport,
  createSpaceMembersStore,
  createSpaceMembersSync,
  createSpaceMembersSyncPersistence,
  memberParams,
  membersFromDock,
  reconcileSpaceMembers,
  spaceMembersStoreSyncAdapter,
  storedSpaceMembers,
  type SpaceDefinition,
  type SpaceMember,
  type SpacesStore,
} from '../spaces'
import { panelID, panelParams, panelPresentation } from './panelRegistry'

// A dock change settles well inside the one-second publish budget.
const reconcileDelayMs = 250

type Options = {
  store: SpacesStore | null
  apiRef: MutableRefObject<DockviewApi | undefined>
  activeSpaceIDRef: MutableRefObject<string | null>
  revision: number
  spaces: SpaceDefinition[]
  syncDock: () => void
  withHistorySuppressed: <T>(operation: () => T) => T
  onProblem: (problem: string) => void
}

// useSpaceMembers publishes each space's pinned agent and file tabs to the
// spaces.members namespace and opens members another device or the native
// client added. It never closes a panel because a member is missing.
export function useSpaceMembers({ store, apiRef, activeSpaceIDRef, revision, spaces, syncDock, withHistorySuppressed, onProblem }: Options) {
  const [members] = useState(() => store ? createSpaceMembersStore() : null)
  const syncRef = useRef<ReturnType<typeof createSpaceMembersSync> | null>(null)
  const started = useRef(false)
  const timer = useRef<number | undefined>(undefined)
  const onProblemRef = useRef(onProblem)
  onProblemRef.current = onProblem

  const read = useCallback((spaceID: string): SpaceMember[] | null => {
    if (spaceID !== activeSpaceIDRef.current) return storedSpaceMembers(localStorage, spaceID)
    const api = apiRef.current
    return api ? membersFromDock(api.toJSON()) : null
  }, [activeSpaceIDRef, apiRef])

  const add = useCallback((spaceID: string, missing: SpaceMember[]): boolean => {
    const api = apiRef.current
    if (spaceID === activeSpaceIDRef.current && api) {
      withHistorySuppressed(() => {
        for (const member of missing) {
          const params = memberParams(member)
          const existing = api.getPanel(panelID(params))
          const current = panelParams(existing?.params)
          if (existing && current) {
            if (current.preview) existing.api.updateParameters({ ...current, preview: false })
            continue
          }
          api.addPanel({
            id: panelID(params),
            component: params.kind,
            tabComponent: 'herder-tab',
            title: panelPresentation(params).title,
            params,
            inactive: api.panels.length > 0,
          })
        }
      })
      syncDock()
      return true
    }
    try {
      return missing.map((member) => writePanelToStoredSpace(localStorage, spaceID, memberParams(member)).ok).every(Boolean)
    } catch {
      return false
    }
  }, [activeSpaceIDRef, apiRef, syncDock, withHistorySuppressed])

  const reconcile = useCallback(() => {
    if (!members || !store || !started.current) return
    reconcileSpaceMembers(members, {
      liveSpaceIDs: store.list().map(({ id }) => id),
      closedSpaceIDs: store.records().flatMap(({ record }) => record.deleted ? [record.id] : []),
      read,
      add,
    })
  }, [add, members, read, store])
  const reconcileRef = useRef(reconcile)
  reconcileRef.current = reconcile

  const schedule = useCallback(() => {
    if (timer.current !== undefined) window.clearTimeout(timer.current)
    timer.current = window.setTimeout(() => {
      timer.current = undefined
      reconcileRef.current()
    }, reconcileDelayMs)
  }, [])

  useEffect(() => {
    if (!members) return
    const sync = createSpaceMembersSync({
      store: spaceMembersStoreSyncAdapter(members),
      persistence: createSpaceMembersSyncPersistence(localStorage),
      transport: browserSpaceMembersTransport(),
      retry: (callback, delay) => window.setTimeout(callback, delay),
      cancelRetry: (handle) => window.clearTimeout(handle as number),
      onProblem: (problem) => onProblemRef.current(problem),
      onRows: (rows) => { if (rows.length > 0) schedule() },
    })
    syncRef.current = sync
    let disposed = false
    // The backfill waits for the first pull so a first run publishes the
    // union of this browser's docks and what other devices already wrote.
    void sync.start().finally(() => {
      if (disposed) return
      started.current = true
      schedule()
    })
    const online = () => { void sync.retryNow() }
    window.addEventListener('online', online)
    return () => {
      disposed = true
      window.removeEventListener('online', online)
      sync.dispose()
      if (syncRef.current === sync) syncRef.current = null
      if (timer.current !== undefined) window.clearTimeout(timer.current)
      timer.current = undefined
    }
  }, [members, schedule])

  useEffect(() => { schedule() }, [revision, schedule, spaces])

  return useCallback((namespace: string, rev: number) => {
    void syncRef.current?.stateChanged(namespace, rev)
  }, [])
}
