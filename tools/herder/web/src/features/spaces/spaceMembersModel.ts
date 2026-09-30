import type { SerializedDockview } from 'dockview-react'
import { initialFileViewMode } from '../files/fileTabs.ts'
import { panelID, panelParams } from '../workspace/panelRegistryModel.ts'
import { readStoredSpaceLayout, type DockPanelParams } from '../layout/dockLayout.ts'

// A space's members are the pinned agent and file tabs in its dock. Screen,
// folder and changes panels, and every preview (italic) tab, are left out.
export type SpaceMember = { kind: 'agent', name: string } | { kind: 'file', root: string, path: string }
export type SpaceMembersValue = { members: SpaceMember[], updated: number }
export type SpaceMembersRow = { key: string, members: SpaceMember[], updated: number, writeID: string, deleted: boolean }

type UnknownRecord = Record<string, unknown>

function record(value: unknown): value is UnknownRecord {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

export function memberParams(member: SpaceMember): DockPanelParams {
  return member.kind === 'agent'
    ? { kind: 'agent', name: member.name, preview: false }
    : { kind: 'file', root: member.root, path: member.path, preview: false, viewMode: initialFileViewMode({ root: member.root, path: member.path }) }
}

export function memberID(member: SpaceMember) {
  return panelID(memberParams(member))
}

export function sameMembers(left: SpaceMember[], right: SpaceMember[]) {
  return left.length === right.length && left.every((member, index) => memberID(member) === memberID(right[index]))
}

function gridPanelOrder(node: unknown, order: string[]) {
  if (!record(node)) return
  if (node.type === 'leaf') {
    if (record(node.data) && Array.isArray(node.data.views)) {
      for (const id of node.data.views) if (typeof id === 'string') order.push(id)
    }
    return
  }
  if (node.type === 'branch' && Array.isArray(node.data)) for (const child of node.data) gridPanelOrder(child, order)
}

// membersFromDock lists members in dock order: groups depth-first through the
// grid (left to right, top to bottom as Dockview serializes them), then tabs
// in each group's order. A panel the grid does not place is not a member.
export function membersFromDock(dock: SerializedDockview | null | undefined): SpaceMember[] {
  if (!dock || !record(dock.panels)) return []
  const order: string[] = []
  gridPanelOrder(record(dock.grid) ? dock.grid.root : undefined, order)
  const seen = new Set<string>()
  return order.flatMap((id): SpaceMember[] => {
    if (seen.has(id)) return []
    seen.add(id)
    const params = panelParams((dock.panels as UnknownRecord)[id] && ((dock.panels as UnknownRecord)[id] as UnknownRecord).params)
    if (!params || params.preview) return []
    if (params.kind === 'agent') return [{ kind: 'agent', name: params.name }]
    if (params.kind === 'file') return [{ kind: 'file', root: params.root, path: params.path }]
    return []
  })
}

// storedSpaceMembers reads a non-active space's saved layout without the
// recovery write readStoredSpaceLayout makes when handed setItem. An
// unreadable layout answers null so a corrupt space never publishes empty.
export function storedSpaceMembers(storage: Pick<Storage, 'getItem'>, spaceID: string): SpaceMember[] | null {
  try {
    const read = readStoredSpaceLayout({ getItem: (key) => storage.getItem(key) }, spaceID)
    if (read.problem && !read.stored) return null
    return membersFromDock(read.stored?.dock)
  } catch {
    return null
  }
}

function parseMember(value: unknown): SpaceMember | null {
  if (!record(value)) return null
  if (value.kind === 'agent') return typeof value.name === 'string' && value.name ? { kind: 'agent', name: value.name } : null
  if (value.kind === 'file') {
    return typeof value.root === 'string' && value.root && typeof value.path === 'string' && value.path
      ? { kind: 'file', root: value.root, path: value.path }
      : null
  }
  return null
}

// parseMembersRow validates a synced row. A malformed row answers null and is
// skipped; an unknown or malformed member inside a valid row is dropped so a
// newer writer adding a member kind does not hide the rest of the space.
export function parseMembersRow(row: unknown): SpaceMembersRow | null {
  if (!record(row) || typeof row.key !== 'string' || !row.key || typeof row.updated !== 'number' || !Number.isFinite(row.updated) ||
    typeof row.writeID !== 'string' || !row.writeID || typeof row.deleted !== 'boolean') return null
  if (row.deleted) return { key: row.key, members: [], updated: row.updated, writeID: row.writeID, deleted: true }
  if (!record(row.value) || !Array.isArray(row.value.members)) return null
  const seen = new Set<string>()
  const members = row.value.members.flatMap((value) => {
    const member = parseMember(value)
    if (!member || seen.has(memberID(member))) return []
    seen.add(memberID(member))
    return [member]
  })
  return { key: row.key, members, updated: row.updated, writeID: row.writeID, deleted: false }
}

export type SpaceMembersLocal = { baseline?: SpaceMember[], applied?: string }

export type SpaceMembersReconcileStore = {
  row: (spaceID: string) => SpaceMembersRow | undefined
  local: (spaceID: string) => SpaceMembersLocal
  setLocal: (spaceID: string, local: SpaceMembersLocal) => void
  publish: (spaceID: string, members: SpaceMember[]) => SpaceMembersRow
  remove: (spaceID: string) => SpaceMembersRow
}

export type SpaceMembersReconcileTarget = {
  liveSpaceIDs: string[]
  closedSpaceIDs: string[]
  read: (spaceID: string) => SpaceMember[] | null
  // add answers false when a member could not be opened (a stored layout that
  // is recovering, or storage refusing the write); the row is retried later.
  add: (spaceID: string, members: SpaceMember[]) => boolean
}

// reconcileSpaceMembers is the only place membership crosses between docks
// and rows. For each live space it first applies a remote row it has not
// applied yet (adding missing members pinned, never closing anything), then
// publishes when the dock differs from the local baseline: the membership
// this browser last published or last reconciled against. Comparing to the
// baseline instead of the row is what keeps two browsers with the same set
// in different orders from rewriting each other forever.
export function reconcileSpaceMembers(store: SpaceMembersReconcileStore, target: SpaceMembersReconcileTarget) {
  const published: string[] = []
  for (const spaceID of target.liveSpaceIDs) {
    const before = target.read(spaceID)
    if (!before) continue
    const local = store.local(spaceID)
    const row = store.row(spaceID)
    let current = before
    let baseline = local.baseline
    let applied = local.applied
    if (row && row.writeID !== applied) {
      applied = row.writeID
      if (!row.deleted) {
        const present = new Set(before.map(memberID))
        const missing = row.members.filter((member) => !present.has(memberID(member)))
        // A member that could not be opened holds the whole space back:
        // publishing now would overwrite the row with a dock that lacks it.
        if (missing.length > 0 && !target.add(spaceID, missing)) continue
        if (missing.length > 0) current = target.read(spaceID) ?? before
        // With no local change pending, the applied dock is the new baseline.
        if (baseline && sameMembers(baseline, before)) baseline = current
      }
    }
    const changed = baseline
      ? !sameMembers(baseline, current)
      : !row || row.deleted || !sameMembers(row.members, current)
    if (changed) {
      const written = store.publish(spaceID, current)
      store.setLocal(spaceID, { baseline: current, applied: written.writeID })
      published.push(spaceID)
    } else if (baseline !== local.baseline || applied !== local.applied || !baseline) {
      store.setLocal(spaceID, { baseline: baseline ?? current, applied })
    }
  }
  for (const spaceID of target.closedSpaceIDs) {
    const row = store.row(spaceID)
    if (!row || row.deleted) continue
    const written = store.remove(spaceID)
    store.setLocal(spaceID, { applied: written.writeID })
    published.push(spaceID)
  }
  return published
}
