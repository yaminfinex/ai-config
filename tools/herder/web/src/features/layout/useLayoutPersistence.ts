import { useCallback, useEffect, useRef, useState, type MutableRefObject } from 'react'
import type { DockviewApi } from 'dockview-react'
import { useDOMEvent } from '../../shared/lifecycle'
import {
  legacyLayoutStorageKey,
  layoutStorageBackupKey,
  layoutStorageKey,
  parseLegacyLayout,
  persistableDockLayout,
  readStoredLayout,
  writeStoredLayout,
  type LegacyLayout,
  type StoredLayout,
} from './dockLayout'

export const defaultSidebarWidth = 250

export function clampSidebarWidth(width: number) {
  return Math.min(440, Math.max(200, width))
}

export type InitialLayout = {
  stored: StoredLayout | null
  backup: StoredLayout | null
  legacy: LegacyLayout | null
  recovering: boolean
  lastGoodRaw: string | null
  sidebarWidth: number
  expandedItems: string[] | null
  knownWorkspaceItems: string[] | null
}

function readInitialLayout(): InitialLayout {
  let stored: StoredLayout | null = null
  let backup: StoredLayout | null = null
  let legacy: LegacyLayout | null = null
  let recovering = false
  let lastGoodRaw: string | null = null
  try {
    const layouts = readStoredLayout(localStorage)
    stored = layouts.stored
    backup = layouts.backup
    recovering = layouts.recovering
    lastGoodRaw = layouts.lastGoodRaw
    if (!stored) legacy = parseLegacyLayout(localStorage.getItem(legacyLayoutStorageKey))
  } catch { /* browser storage is best effort */ }
  const source = stored ?? legacy
  return {
    stored, backup, legacy, recovering, lastGoodRaw,
    sidebarWidth: clampSidebarWidth(source?.sidebarWidth ?? defaultSidebarWidth),
    expandedItems: source?.expandedItems ?? null,
    knownWorkspaceItems: source?.knownWorkspaceItems ?? null,
  }
}

export function useLayoutPersistence(apiRef: MutableRefObject<DockviewApi | undefined>, revision: number) {
  const [initial] = useState(readInitialLayout)
  const [sidebarWidth, setSidebarWidth] = useState(initial.sidebarWidth)
  const [expandedItems, setExpandedItems] = useState<string[] | null>(initial.expandedItems)
  const [knownWorkspaceItems, setKnownWorkspaceItems] = useState<string[] | null>(initial.knownWorkspaceItems)
  const [dockReady, setDockReady] = useState(false)
  const persistenceReady = useRef(false)
  const layoutDirty = useRef(false)
  const persistenceState = useRef({ recovering: initial.recovering, lastGoodRaw: initial.lastGoodRaw })
  const preferenceSnapshot = useRef(JSON.stringify([initial.sidebarWidth, initial.expandedItems, initial.knownWorkspaceItems]))

  const markDirty = useCallback(() => {
    if (persistenceReady.current) layoutDirty.current = true
  }, [])

  const noteBackupRecovery = useCallback(() => {
    persistenceState.current.recovering = true
  }, [])

  const completeRestore = useCallback((dirty: boolean) => {
    persistenceReady.current = true
    if (dirty) layoutDirty.current = true
    setDockReady(true)
  }, [])

  const resetPersistedLayout = useCallback(() => {
    try {
      localStorage.removeItem(layoutStorageKey)
      localStorage.removeItem(layoutStorageBackupKey)
    } catch { /* best effort */ }
    persistenceState.current = { recovering: false, lastGoodRaw: null }
    setSidebarWidth(defaultSidebarWidth)
  }, [])

  const flushLayout = useCallback(() => {
    if (!layoutDirty.current) return false
    const api = apiRef.current
    if (!api) return false
    const dock = persistableDockLayout(api.toJSON())
    if (!dock && api.panels.length > 0) return false
    const value: StoredLayout = { version: 2, dock, sidebarWidth }
    if (expandedItems !== null) value.expandedItems = expandedItems
    if (knownWorkspaceItems !== null) value.knownWorkspaceItems = knownWorkspaceItems
    const previous = persistenceState.current
    const next = writeStoredLayout(localStorage, JSON.stringify(value), previous)
    if (next === previous) return false
    persistenceState.current = next
    layoutDirty.current = false
    return true
  }, [apiRef, expandedItems, knownWorkspaceItems, sidebarWidth])

  useEffect(() => {
    if (!dockReady) return
    const nextPreferences = JSON.stringify([sidebarWidth, expandedItems, knownWorkspaceItems])
    if (preferenceSnapshot.current !== nextPreferences) {
      preferenceSnapshot.current = nextPreferences
      layoutDirty.current = true
    }
    if (!layoutDirty.current) return
    const timer = window.setTimeout(flushLayout, 120)
    return () => window.clearTimeout(timer)
  }, [dockReady, expandedItems, flushLayout, knownWorkspaceItems, revision, sidebarWidth])

  useDOMEvent(window, 'pagehide', () => { flushLayout() })

  return {
    initial,
    sidebarWidth,
    setSidebarWidth,
    expandedItems,
    setExpandedItems,
    knownWorkspaceItems,
    setKnownWorkspaceItems,
    markDirty,
    noteBackupRecovery,
    completeRestore,
    resetPersistedLayout,
    flushLayout,
  }
}
