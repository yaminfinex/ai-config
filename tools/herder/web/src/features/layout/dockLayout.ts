import type { DockviewApi, SerializedDockview } from 'dockview-react'
import type { Board, Pane } from '../../types'
import { fileTabID, type FileViewMode } from '../files/fileTabs.ts'
import { agentTabID } from '../../previewTabs.ts'
import { folderTabID } from '../folders/folderModel.ts'
import { changesPanelID } from '../git/changesModel.ts'

export const layoutStorageKey = 'herder.web.layout.v2'
export const layoutStorageBackupKey = 'herder.web.layout.v2.last-good'
export const legacyLayoutStorageKey = 'herder.web.layout.v1'

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
  version: 2
  dock: SerializedDockview | null
  sidebarWidth: number
  expandedItems?: string[]
  knownWorkspaceItems?: string[]
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

function validPane(value: unknown): value is Pane {
  return record(value) && typeof value.pane_id === 'string' && typeof value.agent === 'string' &&
    typeof value.tool === 'string' && typeof value.herdr_status === 'string' &&
    typeof value.bus_status === 'string' && typeof value.gap === 'string'
}

function validIdentity(value: unknown): value is ScreenIdentity {
  return record(value) && typeof value.paneID === 'string' && typeof value.workspaceID === 'string' &&
    typeof value.tabID === 'string' && typeof value.agent === 'string' &&
    (value.sessionID === undefined || typeof value.sessionID === 'string')
}

export function panelParams(value: unknown): DockPanelParams | null {
  if (!record(value) || typeof value.kind !== 'string' || typeof value.preview !== 'boolean') return null
  if (value.kind === 'agent') return typeof value.name === 'string' && value.name ? { kind: 'agent', name: value.name, preview: value.preview } : null
  if (value.kind === 'screen') return validPane(value.pane) && validIdentity(value.identity)
    ? { kind: 'screen', pane: value.pane, identity: value.identity, preview: value.preview }
    : null
  if (value.kind === 'folder') return typeof value.root === 'string' && Boolean(value.root) && typeof value.path === 'string'
    ? { kind: 'folder', root: value.root, path: value.path, preview: value.preview }
    : null
  if (value.kind === 'changes') return typeof value.root === 'string' && Boolean(value.root)
    ? { kind: 'changes', root: value.root, preview: value.preview }
    : null
  if (value.kind !== 'file' || typeof value.root !== 'string' || !value.root ||
    typeof value.path !== 'string' || (value.line !== undefined && (!Number.isInteger(value.line) || Number(value.line) < 1)) ||
    (value.viewMode !== 'rendered' && value.viewMode !== 'source')) return null
  return {
    kind: 'file', root: value.root, path: value.path,
    ...(value.line === undefined ? {} : { line: Number(value.line) }), preview: value.preview, viewMode: value.viewMode,
  }
}

function expectedPanelID(params: DockPanelParams) {
  if (params.kind === 'agent') return agentTabID(params.name)
  if (params.kind === 'screen') return `screen:${params.pane.pane_id}`
  if (params.kind === 'folder') return folderTabID(params.root, params.path)
  if (params.kind === 'changes') return changesPanelID(params.root)
  return fileTabID(params.root, params.path)
}

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
    if (!params || serialized.contentComponent !== params.kind || expectedPanelID(params) !== id) return false
  }
  return validGridNode(value.grid.root, panelIDs)
}

type ParsedStoredLayout = { layout: StoredLayout, salvaged: boolean }

function validSerializedPanel(id: string, serialized: unknown) {
  if (!record(serialized) || serialized.id !== id) return false
  const params = panelParams(serialized.params)
  return Boolean(params && serialized.contentComponent === params.kind && expectedPanelID(params) === id)
}

function sanitizeStoredDock(value: unknown): { dock: SerializedDockview | null, salvaged: boolean } | null {
  if (value === null) return { dock: null, salvaged: false }
  if (!record(value) || !record(value.panels) || !record(value.grid) || !record(value.grid.root) || value.grid.root.type !== 'branch') return null
  let salvaged = 'floatingGroups' in value || 'popoutGroups' in value || 'edgeGroups' in value
  const panels = Object.fromEntries(Object.entries(value.panels).flatMap(([id, serialized]) => {
    if (validSerializedPanel(id, serialized)) return [[id, serialized]]
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
    if (!record(value) || value.version !== 2 ||
      typeof value.sidebarWidth !== 'number' || !Number.isFinite(value.sidebarWidth) ||
      (value.expandedItems !== undefined && !strings(value.expandedItems)) ||
      (value.knownWorkspaceItems !== undefined && !strings(value.knownWorkspaceItems))) return null
    const result = sanitizeStoredDock(value.dock)
    if (!result) return null
    return { layout: {
      version: 2, dock: result.dock, sidebarWidth: value.sidebarWidth,
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
