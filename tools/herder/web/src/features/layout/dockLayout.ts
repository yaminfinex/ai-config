import type { DockviewApi, SerializedDockview } from 'dockview-react'
import type { Board, Pane } from '../../types'
import type { FileViewMode } from '../files/fileTabs.ts'
import { agentTabID } from '../../previewTabs.ts'
import { panelID, panelParams, panelPresentation } from '../workspace/panelRegistryModel.ts'
import { clampRailWidth, defaultRailPreferences, type RailPreferences } from './utilityRailModel.ts'

export const layoutStorageKey = 'herder.web.layout.v3'
export const layoutStorageBackupKey = 'herder.web.layout.v3.last-good'
export const v2LayoutStorageKey = 'herder.web.layout.v2'
export const legacyLayoutStorageKey = 'herder.web.layout.v1'
export const spaceLayoutPrefix = 'herder.web.layout.v4:'
export const spaceLayoutBackupPrefix = 'herder.web.layout.v4.last-good:'
export const spaceLayoutRecoveryPrefix = 'herder.web.layout.v4.recovery:'

export type AgentPanelParams = { kind: 'agent', name: string, preview: boolean }
export type ScreenIdentity = { paneID: string, workspaceID: string, tabID: string, agent: string, sessionID?: string }
export type ScreenPanelParams = { kind: 'screen', pane: Pane, identity: ScreenIdentity, preview: boolean }
export type FilePanelParams = {
  kind: 'file'
  // The API's canonical absolute served-root path is the root ID. File reads
  // re-prove it server-side: an unserved root is refused instead of remapped.
  root: string
  path: string
  line?: number
  preview: boolean
  viewMode: FileViewMode
}
export type FolderPanelParams = { kind: 'folder', root: string, path: string, preview: boolean }
export type ChangesPanelParams = { kind: 'changes', root: string, preview: boolean }
export type DockPanelParams = AgentPanelParams | ScreenPanelParams | FilePanelParams | FolderPanelParams | ChangesPanelParams

export type StoredLayout = {
  version: 3
  dock: SerializedDockview | null
  rails: RailPreferences
  expandedItems?: string[]
  knownWorkspaceItems?: string[]
}

export type StoredSpaceLayout = {
  version: 4
  dock: SerializedDockview | null
}

export type LegacyLayout = {
  openTabs: string[]
  activeTab: string
  sidebarWidth: number
  expandedItems?: string[]
  knownWorkspaceItems?: string[]
}

type UnknownRecord = Record<string, unknown>
type GridNode = { type: 'leaf' | 'branch', data: UnknownRecord | GridNode[], size?: number, visible?: boolean }

function record(value: unknown): value is UnknownRecord {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function strings(value: unknown): value is string[] {
  return Array.isArray(value) && value.every((item) => typeof item === 'string')
}

function railPreferences(value: unknown): RailPreferences | null {
  if (!record(value) || !record(value.fleet) || !record(value.notes)) return null
  const fleet = value.fleet
  const notes = value.notes
  if (typeof fleet.width !== 'number' || !Number.isFinite(fleet.width) || typeof fleet.collapsed !== 'boolean' ||
    typeof notes.width !== 'number' || !Number.isFinite(notes.width) || typeof notes.collapsed !== 'boolean') return null
  return {
    fleet: { width: clampRailWidth(fleet.width), collapsed: fleet.collapsed },
    notes: { width: clampRailWidth(notes.width), collapsed: notes.collapsed },
  }
}

export { panelParams } from '../workspace/panelRegistryModel.ts'

function validGridNode(value: unknown, panelIDs: Set<string>): value is GridNode {
  if (!record(value) || (value.type !== 'leaf' && value.type !== 'branch')) return false
  if (value.type === 'branch') return Array.isArray(value.data) && value.data.length > 0 && value.data.every((child) => validGridNode(child, panelIDs))
  if (!record(value.data) || typeof value.data.id !== 'string' || !strings(value.data.views) || value.data.views.length === 0) return false
  return value.data.views.every((id) => panelIDs.has(id)) &&
    (value.data.activeView === undefined || typeof value.data.activeView === 'string' && value.data.views.includes(value.data.activeView))
}

function validDock(value: unknown): value is SerializedDockview {
  if (!record(value) || !record(value.panels) || !record(value.grid) || !record(value.grid.root)) return false
  if (value.grid.root.type !== 'branch') return false
  if ('floatingGroups' in value || 'popoutGroups' in value || 'edgeGroups' in value) return false
  const panelIDs = new Set(Object.keys(value.panels))
  if (panelIDs.size === 0) return false
  for (const [id, serialized] of Object.entries(value.panels)) {
    if (!record(serialized) || serialized.id !== id) return false
    const params = panelParams(serialized.params)
    if (!params || serialized.contentComponent !== params.kind || panelID(params) !== id) return false
  }
  return validGridNode(value.grid.root, panelIDs)
}

type ParsedStoredLayout = { layout: StoredLayout, salvaged: boolean }

function canonicalSerializedPanel(id: string, serialized: unknown) {
  if (!record(serialized) || serialized.id !== id) return null
  const params = panelParams(serialized.params)
  return params && serialized.contentComponent === params.kind && panelID(params) === id
    ? { ...serialized, params }
    : null
}

function sanitizeStoredDock(value: unknown): { dock: SerializedDockview | null, salvaged: boolean } | null {
  if (value === null) return { dock: null, salvaged: false }
  if (!record(value) || !record(value.panels) || !record(value.grid) || !record(value.grid.root) || value.grid.root.type !== 'branch') return null
  let salvaged = 'floatingGroups' in value || 'popoutGroups' in value || 'edgeGroups' in value
  const panels = Object.fromEntries(Object.entries(value.panels).flatMap(([id, serialized]) => {
    const canonical = canonicalSerializedPanel(id, serialized)
    if (canonical) {
      if (JSON.stringify(canonical) !== JSON.stringify(serialized)) salvaged = true
      return [[id, canonical]]
    }
    salvaged = true
    return []
  }))
  const keep = new Set(Object.keys(panels))
  const prunedRoot = pruneGridNode(value.grid.root, keep)
  if (!prunedRoot || keep.size === 0) return { dock: null, salvaged: true }
  const root: GridNode = prunedRoot.type === 'branch' ? prunedRoot : { type: 'branch', data: [prunedRoot] }
  if (JSON.stringify(root) !== JSON.stringify(value.grid.root)) salvaged = true
  const ids = groupIDs(root)
  const activeGroup = typeof value.activeGroup === 'string' && ids.has(value.activeGroup) ? value.activeGroup : firstGroupID(root)
  if (activeGroup !== value.activeGroup) salvaged = true
  const dock: UnknownRecord = { ...value, panels, grid: { ...value.grid, root }, activeGroup }
  // Maximize is intentionally session-only. A stale serialized group location
  // can make Dockview throw during fromJSON, so every restore starts neutral.
  delete dock.maximizedNode
  delete dock.floatingGroups
  delete dock.popoutGroups
  delete dock.edgeGroups
  return validDock(dock) ? { dock, salvaged } : null
}

function parseStoredLayoutResult(raw: string | null): ParsedStoredLayout | null {
  try {
    const value: unknown = JSON.parse(raw ?? '')
    if (!record(value) || (value.version !== 2 && value.version !== 3) ||
      (value.expandedItems !== undefined && !strings(value.expandedItems)) ||
      (value.knownWorkspaceItems !== undefined && !strings(value.knownWorkspaceItems))) return null
    const rails = value.version === 2
      ? typeof value.sidebarWidth === 'number' && Number.isFinite(value.sidebarWidth) ? defaultRailPreferences(value.sidebarWidth) : null
      : railPreferences(value.rails)
    if (!rails) return null
    const result = sanitizeStoredDock(value.dock)
    if (!result) return null
    return { layout: {
      version: 3, dock: result.dock, rails,
      ...(value.expandedItems === undefined ? {} : { expandedItems: value.expandedItems }),
      ...(value.knownWorkspaceItems === undefined ? {} : { knownWorkspaceItems: value.knownWorkspaceItems }),
    }, salvaged: result.salvaged }
  } catch {
    return null
  }
}

export function parseStoredLayout(raw: string | null): StoredLayout | null {
  return parseStoredLayoutResult(raw)?.layout ?? null
}

export function parseStoredSpaceLayout(raw: string | null): { layout: StoredSpaceLayout, salvaged: boolean } | null {
  try {
    const value: unknown = JSON.parse(raw ?? '')
    if (!record(value) || value.version !== 4) return null
    const result = sanitizeStoredDock(value.dock)
    return result ? { layout: { version: 4, dock: result.dock }, salvaged: result.salvaged } : null
  } catch {
    return null
  }
}

export function readStoredLayout(storage: Pick<Storage, 'getItem'>) {
  const primaryRaw = storage.getItem(layoutStorageKey)
  const backupRaw = storage.getItem(layoutStorageBackupKey)
  const primary = parseStoredLayoutResult(primaryRaw)
  const backup = parseStoredLayoutResult(backupRaw)
  const primaryUsable = Boolean(primary && (primary.layout.dock !== null || !primary.salvaged))
  const selected = primaryUsable ? primary : backup
  return {
    stored: selected?.layout ?? primary?.layout ?? null,
    backup: backup?.layout ?? null,
    recovering: Boolean((selected === backup && backup) || primary?.salvaged),
    lastGoodRaw: primary && !primary.salvaged ? primaryRaw : null,
  }
}

export type LayoutWriteState = { recovering: boolean, lastGoodRaw: string | null }

export function writeStoredLayout(storage: Pick<Storage, 'setItem'>, raw: string, state: LayoutWriteState): LayoutWriteState {
  try {
    if (!state.recovering) storage.setItem(layoutStorageBackupKey, state.lastGoodRaw ?? raw)
    storage.setItem(layoutStorageKey, raw)
    return { recovering: false, lastGoodRaw: raw }
  } catch {
    return state
  }
}

export function readStoredSpaceLayout(storage: Pick<Storage, 'getItem'> & Partial<Pick<Storage, 'setItem'>>, spaceID: string) {
  const encoded = encodeURIComponent(spaceID)
  const primaryRaw = storage.getItem(`${spaceLayoutPrefix}${encodeURIComponent(spaceID)}`)
  const backupRaw = storage.getItem(`${spaceLayoutBackupPrefix}${encodeURIComponent(spaceID)}`)
  const primary = parseStoredSpaceLayout(primaryRaw)
  const backup = parseStoredSpaceLayout(backupRaw)
  if (storage.setItem && ((primaryRaw && !primary) || (backupRaw && !backup))) {
    try {
      const recoveryKey = `${spaceLayoutRecoveryPrefix}${encoded}`
      const existing = storage.getItem(recoveryKey)
      let closed = false
      try { closed = (JSON.parse(existing ?? '') as { kind?: unknown }).kind === 'closed' } catch { /* replace malformed recovery */ }
      if (!closed) storage.setItem(recoveryKey, JSON.stringify({
        version: 1,
        kind: 'corrupt',
        primaryRaw,
        backupRaw,
        updated: Date.now(),
      }))
    } catch { /* preserving corrupt raw is best effort; restore still stays scoped */ }
  }
  const primaryUsable = Boolean(primary && (primary.layout.dock !== null || !primary.salvaged))
  const selected = primaryUsable ? primary : backup
  return {
    stored: selected?.layout ?? primary?.layout ?? null,
    backup: backup?.layout ?? null,
    recovering: Boolean((selected === backup && backup) || primary?.salvaged),
    lastGoodRaw: primary && !primary.salvaged ? primaryRaw : null,
    problem: Boolean(primaryRaw && !primary) || Boolean(backupRaw && !backup && !primaryUsable),
  }
}

export function writeStoredSpaceLayout(
  storage: Pick<Storage, 'setItem'>,
  spaceID: string,
  raw: string,
  state: LayoutWriteState,
): LayoutWriteState {
  try {
    const encoded = encodeURIComponent(spaceID)
    if (!state.recovering) storage.setItem(`${spaceLayoutBackupPrefix}${encoded}`, state.lastGoodRaw ?? raw)
    storage.setItem(`${spaceLayoutPrefix}${encoded}`, raw)
    return { recovering: false, lastGoodRaw: raw }
  } catch {
    return state
  }
}

export type StoredPanelWriteResult =
  | { ok: true, duplicate: boolean }
  | { ok: false, reason: 'corrupt' | 'recovering' | 'canonical' | 'write' }

export function catchUpExternalPanels(raw: string | null, target: {
  hasPanel: (id: string) => boolean
  addPanel: (id: string, params: DockPanelParams) => void
}) {
  const dock = parseStoredSpaceLayout(raw)?.layout.dock
  if (!dock) return []
  const added: string[] = []
  for (const [id, serialized] of Object.entries(dock.panels)) {
    if (target.hasPanel(id)) continue
    const params = panelParams(serialized.params)
    if (!params) continue
    target.addPanel(id, params)
    added.push(id)
  }
  return added
}

function activatePanelInGrid(node: GridNode, groupID: string, id: string): { node: GridNode, found: boolean } {
  if (node.type === 'leaf') {
    const data = node.data as UnknownRecord
    if (data.id !== groupID) return { node, found: false }
    const views = strings(data.views) ? data.views : []
    return {
      node: { ...node, data: { ...data, views: views.includes(id) ? views : [...views, id], activeView: id } },
      found: true,
    }
  }
  let found = false
  const data = (node.data as GridNode[]).map((child) => {
    if (found) return child
    const activated = activatePanelInGrid(child, groupID, id)
    found = activated.found
    return activated.node
  })
  return { node: { ...node, data }, found }
}

export function writePanelToStoredSpace(
  storage: Pick<Storage, 'getItem' | 'setItem'>,
  spaceID: string,
  params: DockPanelParams,
): StoredPanelWriteResult {
  const target = readStoredSpaceLayout(storage, spaceID)
  if (target.problem) return { ok: false, reason: 'corrupt' }
  if (target.recovering) return { ok: false, reason: 'recovering' }

  const id = panelID(params)
  const presentation = panelPresentation(params)
  const serialized = {
    id,
    contentComponent: params.kind,
    tabComponent: 'herder-tab',
    title: presentation.title,
    params,
  }
  const duplicate = Boolean(target.stored?.dock?.panels[id])
  let candidate: unknown
  if (!target.stored?.dock) {
    const groupID = `space-${encodeURIComponent(spaceID)}-main`
    candidate = {
      grid: { root: { type: 'branch', data: [{ type: 'leaf', data: { id: groupID, views: [id], activeView: id } }] } },
      panels: { [id]: serialized },
      activeGroup: groupID,
    }
  } else {
    const dock = target.stored.dock
    const root = dock.grid.root as GridNode
    const groupID = typeof dock.activeGroup === 'string' && groupIDs(root).has(dock.activeGroup)
      ? dock.activeGroup
      : firstGroupID(root)
    if (!groupID) return { ok: false, reason: 'canonical' }
    const activated = activatePanelInGrid(root, groupID, id)
    if (!activated.found) return { ok: false, reason: 'canonical' }
    candidate = {
      ...dock,
      panels: { ...dock.panels, [id]: serialized },
      grid: { ...dock.grid, root: activated.node },
      activeGroup: groupID,
    }
  }

  const dock = persistableDockLayout(candidate)
  if (!dock) return { ok: false, reason: 'canonical' }
  const state: LayoutWriteState = { recovering: false, lastGoodRaw: target.lastGoodRaw }
  const raw = JSON.stringify({ version: 4, dock })
  if (writeStoredSpaceLayout(storage, spaceID, raw, state) === state) return { ok: false, reason: 'write' }
  const verified = readStoredSpaceLayout(storage, spaceID)
  if (verified.problem || verified.recovering || !verified.stored?.dock?.panels[id]) return { ok: false, reason: 'write' }
  return { ok: true, duplicate }
}

export function parseLegacyLayout(raw: string | null): LegacyLayout | null {
  try {
    const value: unknown = JSON.parse(raw ?? '')
    if (!record(value) || !strings(value.openTabs) || typeof value.activeTab !== 'string' ||
      typeof value.sidebarWidth !== 'number' || !Number.isFinite(value.sidebarWidth) ||
      (value.expandedItems !== undefined && !strings(value.expandedItems)) ||
      (value.knownWorkspaceItems !== undefined && !strings(value.knownWorkspaceItems))) return null
    const openTabs = [...new Set(value.openTabs)]
    if (value.activeTab !== 'board' && !openTabs.some((name) => agentTabID(name) === value.activeTab)) return null
    return {
      openTabs, activeTab: value.activeTab, sidebarWidth: value.sidebarWidth,
      ...(value.expandedItems === undefined ? {} : { expandedItems: [...new Set(value.expandedItems)] }),
      ...(value.knownWorkspaceItems === undefined ? {} : { knownWorkspaceItems: [...new Set(value.knownWorkspaceItems)] }),
    }
  } catch {
    return null
  }
}

function sanitizeLeaf(group: UnknownRecord, keep: Set<string>): UnknownRecord | null {
  if (typeof group.id !== 'string' || !strings(group.views)) return null
  const views = group.views.filter((id) => keep.has(id))
  if (views.length === 0) return null
  const activeView = typeof group.activeView === 'string' && views.includes(group.activeView) ? group.activeView : views[0]
  const tabGroups = Array.isArray(group.tabGroups) ? group.tabGroups.flatMap((tabGroup) => {
    if (!record(tabGroup) || typeof tabGroup.id !== 'string' || !strings(tabGroup.panelIds)) return []
    const panelIds = tabGroup.panelIds.filter((id) => keep.has(id))
    return panelIds.length > 0 ? [{ ...tabGroup, panelIds }] : []
  }) : undefined
  return { ...group, views, activeView, ...(tabGroups === undefined ? {} : { tabGroups }) }
}

function pruneGridNode(value: unknown, keep: Set<string>): GridNode | null {
  if (!record(value) || (value.type !== 'leaf' && value.type !== 'branch')) return null
  if (value.type === 'leaf') {
    if (!record(value.data)) return null
    const data = sanitizeLeaf(value.data, keep)
    return data ? { type: 'leaf', data, ...(typeof value.size === 'number' ? { size: value.size } : {}), ...(typeof value.visible === 'boolean' ? { visible: value.visible } : {}) } : null
  }
  if (!Array.isArray(value.data)) return null
  const children = value.data.flatMap((child) => {
    const kept = pruneGridNode(child, keep)
    return kept ? [kept] : []
  })
  if (children.length === 0) return null
  if (children.length === 1 && children[0].type === 'leaf') return {
    ...children[0],
    ...(typeof value.size === 'number' ? { size: value.size } : {}),
    ...(typeof value.visible === 'boolean' ? { visible: value.visible } : {}),
  }
  return { type: 'branch', data: children, ...(typeof value.size === 'number' ? { size: value.size } : {}), ...(typeof value.visible === 'boolean' ? { visible: value.visible } : {}) }
}

function firstGroupID(node: GridNode): string | undefined {
  if (node.type === 'leaf') return typeof (node.data as UnknownRecord).id === 'string' ? (node.data as UnknownRecord).id as string : undefined
  for (const child of node.data as GridNode[]) {
    const id = firstGroupID(child)
    if (id) return id
  }
}

function groupIDs(node: GridNode): Set<string> {
  if (node.type === 'leaf') return new Set([String((node.data as UnknownRecord).id)])
  return new Set((node.data as GridNode[]).flatMap((child) => [...groupIDs(child)]))
}

export function pinMovedPreview(panel: { params: unknown, api: { updateParameters: (params: DockPanelParams) => void } }) {
  const params = panelParams(panel.params)
  if (!params?.preview) return false
  panel.api.updateParameters({ ...params, preview: false })
  return true
}

export function persistableDockLayout(value: unknown): SerializedDockview | null {
  try {
    const cloned = JSON.parse(JSON.stringify(value)) as UnknownRecord
    if (!record(cloned.panels) || !record(cloned.grid)) return null
    delete cloned.maximizedNode
    delete cloned.floatingGroups
    delete cloned.popoutGroups
    delete cloned.edgeGroups
    cloned.panels = Object.fromEntries(Object.entries(cloned.panels).flatMap(([id, serialized]) => {
      const canonical = canonicalSerializedPanel(id, serialized)
      return canonical ? [[id, canonical]] : []
    }))
    return validDock(cloned) ? cloned : null
  } catch {
    return null
  }
}

export function restoreDockLayout(api: Pick<DockviewApi, 'fromJSON'>, dock: SerializedDockview) {
  try {
    api.fromJSON(dock)
    return true
  } catch (error) {
    console.error('Failed to restore persisted dock layout; using the default layout.', error)
    return false
  }
}

export type ScreenIdentityState = 'checking' | 'ready' | 'mismatch'

export function screenIdentityState(params: ScreenPanelParams, board?: Board): ScreenIdentityState {
  if (!board) return 'checking'
  for (const workspace of board.workspaces) {
    for (const tab of workspace.tabs) {
      const pane = tab.panes.find((candidate) => candidate.pane_id === params.identity.paneID)
      if (!pane) continue
      return workspace.workspace_id === params.identity.workspaceID && tab.tab_id === params.identity.tabID &&
        pane.agent === params.identity.agent && pane.agent_session === params.identity.sessionID ? 'ready' : 'mismatch'
    }
  }
  return 'mismatch'
}

export function screenPanelParams(board: Board, pane: Pane, preview: boolean): ScreenPanelParams | null {
  for (const workspace of board.workspaces) {
    for (const tab of workspace.tabs) {
      const live = tab.panes.find((candidate) => candidate.pane_id === pane.pane_id)
      if (!live) continue
      return {
        kind: 'screen', pane: live, preview,
        identity: { paneID: live.pane_id, workspaceID: workspace.workspace_id, tabID: tab.tab_id, agent: live.agent, sessionID: live.agent_session },
      }
    }
  }
  return null
}
