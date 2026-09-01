import type { Pane } from '../../types.ts'
import { agentTabID } from '../../previewTabs.ts'
import { fileTabID, initialFileViewMode } from '../files/fileTabs.ts'
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
type RouteLocation = { pathname: string, search: URLSearchParams }
type RouteInput = { source: 'url', location: RouteLocation } | { source: 'state', subject: unknown }
type RouteCandidate = UnknownRecord | null | undefined

type PanelRouteModel<P extends DockPanelParams> = {
  path: (params: P) => string
  subject: (params: P) => UnknownRecord
  parse: (input: RouteInput) => RouteCandidate
}

type PanelModel<P extends DockPanelParams> = {
  validate: (value: UnknownRecord) => P | null
  id: (params: P) => string
  presentation: (params: P) => PanelPresentation
  usesQuickOpenGroup?: boolean
  mergeExisting?: (current: P, next: P) => P
  route: PanelRouteModel<P>
}

type RegisteredPanelModel = {
  validate: (value: UnknownRecord) => DockPanelParams | null
  id: (params: DockPanelParams) => string
  presentation: (params: DockPanelParams) => PanelPresentation
  usesQuickOpenGroup: boolean
  mergeExisting: (current: DockPanelParams, next: DockPanelParams) => DockPanelParams
  route: {
    path: (params: DockPanelParams) => string
    subject: (params: DockPanelParams) => UnknownRecord
    parse: (input: RouteInput) => DockPanelParams | null | undefined
  }
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
    route: {
      path: (params) => model.route.path(params as P),
      subject: (params) => model.route.subject(params as P),
      parse: (input) => {
        const candidate = model.route.parse(input)
        return candidate === undefined || candidate === null ? candidate : model.validate(candidate)
      },
    },
  }
}

function stateSubject(input: RouteInput, kind: PanelKind) {
  return input.source === 'state' && record(input.subject) && input.subject.kind === kind ? input.subject : undefined
}

function knownPath(input: RouteInput, path: string) {
  return input.source === 'url' && (input.location.pathname === `/${path}` || input.location.pathname === `/${path}/`)
}

function routeValue(values: UnknownRecord | URLSearchParams, key: string) {
  return values instanceof URLSearchParams ? values.get(key) : values[key]
}

const panelModels: Record<PanelKind, RegisteredPanelModel> = {
  agent: definePanel<AgentPanelParams>({
    validate: (value) => typeof value.name === 'string' && value.name
      ? { kind: 'agent', name: value.name, preview: value.preview as boolean }
      : null,
    id: (params) => agentTabID(params.name),
    presentation: (params) => ({ title: params.name, icon: '', meta: 'unknown' }),
    route: {
      path: (params) => `/agents/${encodeURIComponent(params.name)}`,
      subject: (params) => ({ kind: 'agent', name: params.name }),
      parse: (input) => {
        if (input.source === 'state') {
          const subject = stateSubject(input, 'agent')
          return subject ? { kind: 'agent', name: subject.name, preview: true } : undefined
        }
        const match = input.location.pathname.match(/^\/agents\/([^/]+)\/?$/)
        if (!match) return undefined
        try {
          return { kind: 'agent', name: decodeURIComponent(match[1]), preview: true }
        } catch {
          return null
        }
      },
    },
  }),
  screen: definePanel<ScreenPanelParams>({
    validate: (value) => validPane(value.pane) && validIdentity(value.identity)
      ? { kind: 'screen', pane: value.pane, identity: value.identity, preview: value.preview as boolean }
      : null,
    id: (params) => `screen:${params.pane.pane_id}`,
    presentation: (params) => ({ title: params.pane.label || params.pane.pane_id, icon: '▣ ', meta: 'terminal' }),
    route: {
      path: () => '/',
      subject: (params) => ({ kind: 'screen', pane: params.pane, identity: params.identity }),
      parse: (input) => {
        const subject = stateSubject(input, 'screen')
        return subject ? { kind: 'screen', pane: subject.pane, identity: subject.identity, preview: true } : undefined
      },
    },
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
    route: {
      path: (params) => {
        const search = new URLSearchParams({ root: params.root, path: params.path })
        if (params.line) search.set('line', String(params.line))
        return `/file?${search}`
      },
      subject: (params) => ({ kind: 'file', root: params.root, path: params.path, ...(params.line ? { line: params.line } : {}) }),
      parse: (input) => {
        const values = input.source === 'state' ? stateSubject(input, 'file') : knownPath(input, 'file') ? input.location.search : undefined
        if (values === undefined) return undefined
        const root = routeValue(values, 'root')
        const path = routeValue(values, 'path')
        const rawLine = routeValue(values, 'line')
        if (root === null || path === null || typeof root !== 'string' || typeof path !== 'string') return null
        if (rawLine !== null && rawLine !== undefined) {
          if ((typeof rawLine !== 'number' && typeof rawLine !== 'string') || !/^[1-9]\d*$/.test(String(rawLine))) return null
        }
        const line = rawLine === null || rawLine === undefined ? undefined : Number(rawLine)
        return {
          kind: 'file', root, path, ...(line === undefined ? {} : { line }), preview: true,
          viewMode: initialFileViewMode({ root, path, ...(line === undefined ? {} : { line }) }),
        }
      },
    },
  }),
  folder: definePanel<FolderPanelParams>({
    validate: (value) => typeof value.root === 'string' && Boolean(value.root) && typeof value.path === 'string'
      ? { kind: 'folder', root: value.root, path: value.path, preview: value.preview as boolean }
      : null,
    id: (params) => folderTabID(params.root, params.path),
    presentation: (params) => ({ title: rootLabel(params.path) || rootLabel(params.root), icon: '▰ ', meta: 'folder · read-only' }),
    usesQuickOpenGroup: true,
    route: {
      path: (params) => `/folder?${new URLSearchParams({ root: params.root, path: params.path })}`,
      subject: (params) => ({ kind: 'folder', root: params.root, path: params.path }),
      parse: (input) => {
        const values = input.source === 'state' ? stateSubject(input, 'folder') : knownPath(input, 'folder') ? input.location.search : undefined
        if (values === undefined) return undefined
        const root = routeValue(values, 'root')
        const path = routeValue(values, 'path')
        return typeof root !== 'string' || typeof path !== 'string' ? null : { kind: 'folder', root, path, preview: true }
      },
    },
  }),
  changes: definePanel<ChangesPanelParams>({
    validate: (value) => typeof value.root === 'string' && Boolean(value.root)
      ? { kind: 'changes', root: value.root, preview: value.preview as boolean }
      : null,
    id: (params) => changesPanelID(params.root),
    presentation: (params) => ({ title: `Changes · ${rootLabel(params.root)}`, icon: '± ', meta: 'git · read-only' }),
    route: {
      path: (params) => `/changes?${new URLSearchParams({ root: params.root })}`,
      subject: (params) => ({ kind: 'changes', root: params.root }),
      parse: (input) => {
        const values = input.source === 'state' ? stateSubject(input, 'changes') : knownPath(input, 'changes') ? input.location.search : undefined
        if (values === undefined) return undefined
        const root = routeValue(values, 'root')
        return typeof root !== 'string' ? null : { kind: 'changes', root, preview: true }
      },
    },
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

export function panelRoutePath(params: DockPanelParams) {
  return panelModels[params.kind].route.path(params)
}

export function panelRouteSubject(params: DockPanelParams) {
  return panelModels[params.kind].route.subject(params)
}

export function panelParamsFromRouteLocation(pathname: string, search: string) {
  const input: RouteInput = { source: 'url', location: { pathname, search: new URLSearchParams(search) } }
  for (const model of Object.values(panelModels)) {
    const parsed = model.route.parse(input)
    if (parsed !== undefined) return parsed
  }
}

export function panelParamsFromHistorySubject(subject: unknown) {
  if (!record(subject) || typeof subject.kind !== 'string' || !(subject.kind in panelModels)) return null
  return panelModels[subject.kind as PanelKind].route.parse({ source: 'state', subject }) ?? null
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
