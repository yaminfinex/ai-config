import type { Pane } from '../../types.ts'
import { agentTabID } from '../../previewTabs.ts'
import { fileTabID } from '../files/fileTabs.ts'
import { rootLabel } from '../files/fileResolution.ts'
import { folderTabID } from '../folders/folderModel.ts'
import { changesPanelID } from '../git/changesModel.ts'
import type {
  AgentPanelParams,
  ChangesPanelParams,
  DockPanelParams,
  FilePanelParams,
  FolderPanelParams,
  ScreenIdentity,
  ScreenPanelParams,
} from '../layout/dockLayout.ts'

type UnknownRecord = Record<string, unknown>
export type PanelKind = DockPanelParams['kind']
export type PanelPresentation = { title: string, icon: string, meta: string }

type PanelModel<P extends DockPanelParams> = {
  validate: (value: UnknownRecord) => P | null
  id: (params: P) => string
  presentation: (params: P) => PanelPresentation
  usesQuickOpenGroup?: boolean
  mergeExisting?: (current: P, next: P) => P
}

type RegisteredPanelModel = {
  validate: (value: UnknownRecord) => DockPanelParams | null
  id: (params: DockPanelParams) => string
  presentation: (params: DockPanelParams) => PanelPresentation
  usesQuickOpenGroup: boolean
  mergeExisting: (current: DockPanelParams, next: DockPanelParams) => DockPanelParams
}

function record(value: unknown): value is UnknownRecord {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
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

function definePanel<P extends DockPanelParams>(model: PanelModel<P>): RegisteredPanelModel {
  return {
    validate: model.validate,
    id: (params) => model.id(params as P),
    presentation: (params) => model.presentation(params as P),
    usesQuickOpenGroup: model.usesQuickOpenGroup ?? false,
    mergeExisting: (current, next) => model.mergeExisting
      ? model.mergeExisting(current as P, next as P)
      : { ...next, preview: current.preview && next.preview },
  }
}

const panelModels: Record<PanelKind, RegisteredPanelModel> = {
  agent: definePanel<AgentPanelParams>({
    validate: (value) => typeof value.name === 'string' && value.name
      ? { kind: 'agent', name: value.name, preview: value.preview as boolean }
      : null,
    id: (params) => agentTabID(params.name),
    presentation: (params) => ({ title: params.name, icon: '', meta: 'unknown' }),
  }),
  screen: definePanel<ScreenPanelParams>({
    validate: (value) => validPane(value.pane) && validIdentity(value.identity)
      ? { kind: 'screen', pane: value.pane, identity: value.identity, preview: value.preview as boolean }
      : null,
    id: (params) => `screen:${params.pane.pane_id}`,
    presentation: (params) => ({ title: params.pane.label || params.pane.pane_id, icon: '▣ ', meta: 'terminal' }),
  }),
  file: definePanel<FilePanelParams>({
    validate: (value) => typeof value.root === 'string' && Boolean(value.root) && typeof value.path === 'string' &&
      (value.line === undefined || Number.isInteger(value.line) && Number(value.line) >= 1) &&
      (value.viewMode === 'rendered' || value.viewMode === 'source')
      ? {
          kind: 'file', root: value.root, path: value.path,
          ...(value.line === undefined ? {} : { line: Number(value.line) }),
          preview: value.preview as boolean, viewMode: value.viewMode,
        }
      : null,
    id: (params) => fileTabID(params.root, params.path),
    presentation: (params) => ({ title: rootLabel(params.path), icon: '◇ ', meta: 'file · read-only' }),
    usesQuickOpenGroup: true,
    mergeExisting: (current, next) => ({
      ...current, root: next.root, path: next.path,
      ...(next.line ? { line: next.line } : {}),
      preview: current.preview && next.preview,
      viewMode: next.line ? 'source' : current.viewMode,
    }),
  }),
  folder: definePanel<FolderPanelParams>({
    validate: (value) => typeof value.root === 'string' && Boolean(value.root) && typeof value.path === 'string'
      ? { kind: 'folder', root: value.root, path: value.path, preview: value.preview as boolean }
      : null,
    id: (params) => folderTabID(params.root, params.path),
    presentation: (params) => ({ title: rootLabel(params.path) || rootLabel(params.root), icon: '▰ ', meta: 'folder · read-only' }),
    usesQuickOpenGroup: true,
  }),
  changes: definePanel<ChangesPanelParams>({
    validate: (value) => typeof value.root === 'string' && Boolean(value.root)
      ? { kind: 'changes', root: value.root, preview: value.preview as boolean }
      : null,
    id: (params) => changesPanelID(params.root),
    presentation: (params) => ({ title: `Changes · ${rootLabel(params.root)}`, icon: '± ', meta: 'git · read-only' }),
  }),
}

export function panelParams(value: unknown): DockPanelParams | null {
  if (!record(value) || typeof value.kind !== 'string' || typeof value.preview !== 'boolean') return null
  if (!(value.kind in panelModels)) return null
  return panelModels[value.kind as PanelKind].validate(value)
}

export function panelID(params: DockPanelParams) {
  return panelModels[params.kind].id(params)
}

export function panelPresentation(params: DockPanelParams) {
  return panelModels[params.kind].presentation(params)
}

export function panelUsesQuickOpenGroup(params: DockPanelParams) {
  return panelModels[params.kind].usesQuickOpenGroup
}

export function mergePanelParams(current: DockPanelParams, next: DockPanelParams) {
  if (current.kind !== next.kind) return next
  return panelModels[next.kind].mergeExisting(current, next)
}

export function previewPanelToReplace<T extends { params: unknown }>(panels: T[], kind: PanelKind) {
  return panels.find((panel) => {
    const params = panelParams(panel.params)
    return params?.kind === kind && params.preview
  })
}
