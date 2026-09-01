import { useCallback, useEffect, useRef, useState, type MutableRefObject } from 'react'
import type { DockviewApi } from 'dockview-react'
import { useDOMEvent } from '../../shared/lifecycle'
import {
  persistableDockLayout,
  readStoredSpaceLayout,
  type LegacyLayout,
  type LayoutWriteState,
  type StoredLayout,
  type StoredSpaceLayout,
} from './dockLayout'
import { persistLayoutSnapshot } from './layoutPersistenceModel.ts'
import { defaultRailPreferences, type RailPreference } from './utilityRailModel'
import {
  readShellPreferences,
  writeShellPreferences,
  type StoredShellPreferences,
} from './shellPreferences.ts'
import {
  activeSpaceSessionKey,
  clearAllLayoutFamilies,
  readLegacyLayoutFamilies,
  type SpacesInitialization,
} from '../spaces/spacesModel.ts'

type DockLayout = StoredLayout | StoredSpaceLayout

export type InitialLayout = {
  stored: DockLayout | null
  backup: DockLayout | null
  legacy: LegacyLayout | null
  recovering: boolean
  lastGoodRaw: string | null
  problem: boolean
  fleetRail: RailPreference
  notesRail: RailPreference
  expandedItems: string[] | null
  knownWorkspaceItems: string[] | null
}

function legacyInitial(initialization: Extract<SpacesInitialization, { mode: 'legacy' }>): InitialLayout {
  const initial = initialization.legacy ?? readLegacyLayoutFamilies(localStorage)
  const source = initial.stored ?? initial.legacy
  const rails = initial.stored?.rails ?? defaultRailPreferences(initial.legacy?.sidebarWidth)
  return {
    ...initial,
    problem: false,
    fleetRail: rails.fleet,
    notesRail: rails.notes,
    expandedItems: source?.expandedItems ?? null,
    knownWorkspaceItems: source?.knownWorkspaceItems ?? null,
  }
}

function spacesInitial(spaceID: string): InitialLayout {
  const shell = readShellPreferences(localStorage)
  const dock = readStoredSpaceLayout(localStorage, spaceID)
  return {
    stored: dock.stored,
    backup: dock.backup,
    legacy: null,
    recovering: dock.recovering,
    lastGoodRaw: dock.lastGoodRaw,
    problem: dock.problem,
    fleetRail: shell.stored.rails.fleet,
    notesRail: shell.stored.rails.notes,
    expandedItems: shell.stored.expandedItems ?? null,
    knownWorkspaceItems: shell.stored.knownWorkspaceItems ?? null,
  }
}

function initialShellWriteState(): LayoutWriteState {
  try {
    const shell = readShellPreferences(localStorage)
    return { recovering: shell.recovering, lastGoodRaw: shell.lastGoodRaw }
  } catch {
    return { recovering: false, lastGoodRaw: null }
  }
}

export function useLayoutPersistence(
  apiRef: MutableRefObject<DockviewApi | undefined>,
  revision: number,
  initialization: SpacesInitialization,
  initialSpaceID: string | null,
) {
  const [initial] = useState(() => initialization.mode === 'spaces' && initialSpaceID
    ? spacesInitial(initialSpaceID)
    : legacyInitial(initialization as Extract<SpacesInitialization, { mode: 'legacy' }>))
  const [fleetRail, setFleetRail] = useState(initial.fleetRail)
  const [notesRail, setNotesRail] = useState(initial.notesRail)
  const [expandedItems, setExpandedItems] = useState<string[] | null>(initial.expandedItems)
  const [knownWorkspaceItems, setKnownWorkspaceItems] = useState<string[] | null>(initial.knownWorkspaceItems)
  const [dockReady, setDockReady] = useState(false)
  const persistenceReady = useRef(false)
  const layoutDirty = useRef(false)
  const shellDirty = useRef(false)
  const timer = useRef<number | undefined>(undefined)
  const generation = useRef(0)
  const restoredSpaceID = useRef<string | null>(null)
  const legacyState = useRef<LayoutWriteState>({ recovering: initial.recovering, lastGoodRaw: initial.lastGoodRaw })
  const spaceStates = useRef(new Map<string, LayoutWriteState>())
  const [initialShellState] = useState(initialShellWriteState)
  const shellState = useRef<LayoutWriteState>(initialShellState)
  const preferenceSnapshot = useRef(JSON.stringify([initial.fleetRail, initial.notesRail, initial.expandedItems, initial.knownWorkspaceItems]))

  const cancelTimer = useCallback(() => {
    if (timer.current === undefined) return
    window.clearTimeout(timer.current)
    timer.current = undefined
  }, [])

  const markDirty = useCallback(() => {
    if (persistenceReady.current) layoutDirty.current = true
  }, [])

  const noteBackupRecovery = useCallback((spaceID?: string) => {
    if (initialization.mode === 'legacy') legacyState.current.recovering = true
    else if (spaceID) {
      const state = spaceStates.current.get(spaceID) ?? { recovering: false, lastGoodRaw: null }
      spaceStates.current.set(spaceID, { ...state, recovering: true })
    }
  }, [initialization.mode])

  const beginRestore = useCallback(() => {
    cancelTimer()
    generation.current += 1
    persistenceReady.current = false
    layoutDirty.current = false
    restoredSpaceID.current = null
  }, [cancelTimer])

  const completeRestore = useCallback((spaceID: string | null, dirty: boolean) => {
    restoredSpaceID.current = initialization.mode === 'spaces' ? spaceID : null
    persistenceReady.current = true
    if (dirty) layoutDirty.current = true
    setDockReady(true)
  }, [initialization.mode])

  const flushShell = useCallback(() => {
    if (initialization.mode !== 'spaces' || !shellDirty.current) return false
    const value: StoredShellPreferences = { version: 1, rails: { fleet: fleetRail, notes: notesRail } }
    if (expandedItems !== null) value.expandedItems = expandedItems
    if (knownWorkspaceItems !== null) value.knownWorkspaceItems = knownWorkspaceItems
    const previous = shellState.current
    const next = writeShellPreferences(localStorage, JSON.stringify(value), previous)
    if (next === previous) return false
    shellState.current = next
    shellDirty.current = false
    return true
  }, [expandedItems, fleetRail, initialization.mode, knownWorkspaceItems, notesRail])

  const flushLayout = useCallback(() => {
    if (!persistenceReady.current || !layoutDirty.current) return false
    const api = apiRef.current
    if (!api) return false
    const dock = persistableDockLayout(api.toJSON())
    if (!dock && api.panels.length > 0) return false
    if (initialization.mode === 'spaces') {
      const spaceID = restoredSpaceID.current
      if (!spaceID) return false
      const previous = spaceStates.current.get(spaceID) ?? { recovering: false, lastGoodRaw: null }
      const result = persistLayoutSnapshot(localStorage, { mode: 'spaces', activeSpaceID: spaceID }, JSON.stringify({ version: 4, dock }), previous)
      if (!result.wrote) return false
      spaceStates.current.set(spaceID, result.state)
    } else {
      const value: StoredLayout = { version: 3, dock, rails: { fleet: fleetRail, notes: notesRail } }
      if (expandedItems !== null) value.expandedItems = expandedItems
      if (knownWorkspaceItems !== null) value.knownWorkspaceItems = knownWorkspaceItems
      const previous = legacyState.current
      const result = persistLayoutSnapshot(localStorage, { mode: 'legacy' }, JSON.stringify(value), previous)
      if (!result.wrote) return false
      legacyState.current = result.state
    }
    layoutDirty.current = false
    return true
  }, [apiRef, expandedItems, fleetRail, initialization.mode, knownWorkspaceItems, notesRail])

  const flushAll = useCallback(() => {
    const dock = flushLayout()
    const shell = flushShell()
    return dock || shell
  }, [flushLayout, flushShell])

  const flushBeforeSwitch = useCallback(() => {
    flushAll()
    return !layoutDirty.current
  }, [flushAll])

  const readSpace = useCallback((spaceID: string) => {
    const value = readStoredSpaceLayout(localStorage, spaceID)
    spaceStates.current.set(spaceID, { recovering: value.recovering, lastGoodRaw: value.lastGoodRaw })
    return value
  }, [])

  const resetPersistedLayout = useCallback(() => {
    cancelTimer()
    if (initialization.mode === 'spaces') persistenceReady.current = false
    layoutDirty.current = false
    shellDirty.current = false
    try {
      clearAllLayoutFamilies(localStorage)
      sessionStorage.removeItem(activeSpaceSessionKey)
    } catch { /* browser storage is best effort */ }
    legacyState.current = { recovering: false, lastGoodRaw: null }
    spaceStates.current.clear()
    shellState.current = { recovering: false, lastGoodRaw: null }
    const defaults = defaultRailPreferences()
    setFleetRail(defaults.fleet)
    setNotesRail(defaults.notes)
    if (initialization.mode === 'spaces') window.location.reload()
  }, [cancelTimer, initialization.mode])

  useEffect(() => {
    if (!dockReady) return
    const nextPreferences = JSON.stringify([fleetRail, notesRail, expandedItems, knownWorkspaceItems])
    if (preferenceSnapshot.current !== nextPreferences) {
      preferenceSnapshot.current = nextPreferences
      if (initialization.mode === 'spaces') shellDirty.current = true
      else layoutDirty.current = true
    }
    if (!layoutDirty.current && !shellDirty.current) return
    cancelTimer()
    const scheduledGeneration = generation.current
    timer.current = window.setTimeout(() => {
      timer.current = undefined
      if (scheduledGeneration === generation.current) flushAll()
    }, 120)
    return cancelTimer
  }, [cancelTimer, dockReady, expandedItems, fleetRail, flushAll, initialization.mode, knownWorkspaceItems, notesRail, revision])

  useDOMEvent(window, 'pagehide', () => { flushAll() })

  return {
    initial,
    fleetRail,
    setFleetRail,
    notesRail,
    setNotesRail,
    expandedItems,
    setExpandedItems,
    knownWorkspaceItems,
    setKnownWorkspaceItems,
    markDirty,
    noteBackupRecovery,
    beginRestore,
    completeRestore,
    readSpace,
    resetPersistedLayout,
    flushLayout,
    flushAll,
    flushBeforeSwitch,
  }
}
