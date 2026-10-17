import { useCallback, useEffect, useMemo, useRef, useState, useSyncExternalStore, type MutableRefObject } from 'react'
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
  hold: boolean
  syncDock: () => void
  withHistorySuppressed: <T>(operation: () => T) => T
  onProblem: (problem: string) => void
}

// useSpaceMembers publishes each space's pinned agent and file tabs to the
// spaces.members namespace and opens members another device or the native
// client added. It never closes a panel because a member is missing. Lane
// spaces are live spaces like the user's own while the board lists them.
// ready turns true once a full pull has landed; moveMembers re-keys a row
// (the one-time lane move). hold keeps reconciliation off until that move
// has run, so nothing writes a lane space's layout before it.
export function useSpaceMembers({ store, apiRef, activeSpaceIDRef, revision, spaces, hold, syncDock, withHistorySuppressed, onProblem }: Options) {
  const [members] = useState(() => store ? createSpaceMembersStore() : null)
  const syncRef = useRef<ReturnType<typeof createSpaceMembersSync> | null>(null)
  const started = useRef(false)
  const timer = useRef<number | undefined>(undefined)
  const onProblemRef = useRef(onProblem)
  onProblemRef.current = onProblem
  const spacesRef = useRef(spaces)
  spacesRef.current = spaces
  const [ready, setReady] = useState(false)

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
    if (!members || !store || !started.current || hold) return
    reconcileSpaceMembers(members, {
      liveSpaceIDs: spacesRef.current.map(({ id }) => id),
      closedSpaceIDs: store.records().flatMap(({ record }) => record.deleted ? [record.id] : []),
      read,
      add,
    })
  }, [add, hold, members, read, store])
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
    // The backfill waits for a full pull so a first run publishes the
    // union of this browser's docks and what other devices already wrote.
    void sync.start()
    void sync.pulled().then(() => {
      if (disposed) return
      started.current = true
      setReady(true)
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

  useEffect(() => { schedule() }, [hold, revision, schedule, spaces])

  const stateChanged = useCallback((namespace: string, rev: number) => {
    void syncRef.current?.stateChanged(namespace, rev)
  }, [])
  // membersBySpace is each live space's spaces.members row, in dock order:
  // what the lens counts. A space with no row yet reads its own dock.
  const subscribe = useCallback((listener: () => void) => members?.subscribe(listener) ?? (() => undefined), [members])
  const version = useSyncExternalStore(subscribe, () => members?.version() ?? 0)
  const membersBySpace = useMemo(() => {
    void version
    return Object.fromEntries(spaces.map((space) => {
      const row = members?.row(space.id)
      return [space.id, row && !row.deleted ? row.members : read(space.id) ?? []]
    })) as Record<string, SpaceMember[]>
    // revision covers a dock change in a space not yet published.
  }, [members, read, revision, spaces, version])
  // moveMembers gives to the members row from has, unless to already has
  // one, and removes from's.
  const moveMembers = useCallback((from: string, to: string) => {
    const row = members?.row(from)
    if (!members || !row || row.deleted) return
    const target = members.row(to)
    if (!target || target.deleted || target.members.length === 0) members.publish(to, row.members)
    members.remove(from)
  }, [members])
  return { stateChanged, membersBySpace, ready, moveMembers }
}
