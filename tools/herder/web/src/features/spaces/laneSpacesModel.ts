import type { Board, Lane } from '../../types.ts'
import { plainRecord, readBrowserRecord, writeBrowserRecord } from './browserRecord.ts'
import type { SpaceMember } from './spaceMembersModel.ts'
import { spaceLayoutBackupKey, spaceLayoutKey, type SpaceDefinition } from './spacesModel.ts'

// A lane is a space: every lane on the board is listed as space
// `lane:<id>` with the lane's name, ahead of the user's own spaces. Layout,
// members, read markers and lens prefs key by space id, so nothing else
// stores lanes. A lane with no live member is not on the board, so its
// space drops out until a seat joins. Renaming it renames the lane.
const laneSpacePrefix = 'lane:'

export function laneSpaceID(laneID: string) {
  return `${laneSpacePrefix}${laneID}`
}

// laneOfSpace is the lane id of a lane space, null for the user's own.
export function laneOfSpace(spaceID: string | null | undefined) {
  return spaceID?.startsWith(laneSpacePrefix) ? spaceID.slice(laneSpacePrefix.length) : null
}

export function laneSpaces(board: Board | undefined): SpaceDefinition[] {
  const lanes = board?.lanes ?? []
  return lanes.map((lane, index) => ({ id: laneSpaceID(lane.id), name: lane.name, order: index - lanes.length, created: 0, updated: 0 }))
}

// laneRoster is the lane's members, lead first.
export function laneRoster(lane: Pick<Lane, 'lead' | 'members'>) {
  return lane.members.includes(lane.lead) ? [lane.lead, ...lane.members.filter((name) => name !== lane.lead)] : lane.members
}

// withLaneRosters gives each lane space its roster (lead first) ahead of its
// pinned members, so the lens's visible agent and peek default to the lead
// and the badge counts every member.
export function withLaneRosters(membersBySpace: Record<string, SpaceMember[]>, lanes: readonly Pick<Lane, 'id' | 'lead' | 'members'>[] = []): Record<string, SpaceMember[]> {
  if (lanes.length === 0) return membersBySpace
  const result = { ...membersBySpace }
  for (const lane of lanes) {
    const id = laneSpaceID(lane.id)
    const roster = laneRoster(lane)
    const pinned = (membersBySpace[id] ?? []).filter((member) => member.kind !== 'agent' || !roster.includes(member.name))
    result[id] = [...roster.map((name) => ({ kind: 'agent' as const, name })), ...pinned]
  }
  return result
}

// The one-time move re-keys the owner's spaces that became lanes. The move
// script writes {old space id: lane id}; each browser applies a pair once and
// records it here. The members row and the old space row are server state
// (moved only while the old row is live, so a second browser skips them);
// the layout and the lens tier are this browser's (moved only into an
// empty key).
export const laneMoveKey = 'herder.web.laneMove.v1'

export type LaneMoveTarget = {
  hasSpace: (id: string) => boolean
  moveMembers: (from: string, to: string) => void
  closeSpace: (id: string) => void
  moveLayout: (from: string, to: string) => void
  moveTier: (from: string, to: string) => void
}

export function readLaneMovesApplied(storage: Pick<Storage, 'getItem'>) {
  const parse = (raw: string | null) => {
    try {
      const value: unknown = JSON.parse(raw ?? '')
      return plainRecord(value) && value.version === 1 && Array.isArray(value.applied) ? value.applied.filter((id): id is string => typeof id === 'string') : null
    } catch {
      return null
    }
  }
  const { value, state } = readBrowserRecord(storage, laneMoveKey, parse)
  return { applied: value ?? [], state }
}

// pendingLaneMoves are the pairs this browser has not applied yet.
export function pendingLaneMoves(moves: Record<string, string>, applied: readonly string[]) {
  return Object.entries(moves).filter(([from, lane]) => from && lane && !applied.includes(from))
}

// runLaneMoves applies each pending pair once and returns every applied id.
export function runLaneMoves(moves: Record<string, string>, applied: readonly string[], target: LaneMoveTarget) {
  const done = [...applied]
  for (const [from, lane] of pendingLaneMoves(moves, applied)) {
    const to = laneSpaceID(lane)
    if (target.hasSpace(from)) {
      target.moveMembers(from, to)
      target.closeSpace(from)
    }
    target.moveLayout(from, to)
    target.moveTier(from, to)
    done.push(from)
  }
  return done
}

export function writeLaneMovesApplied(storage: Pick<Storage, 'getItem' | 'setItem'>, applied: readonly string[]) {
  writeBrowserRecord(storage, laneMoveKey, JSON.stringify({ version: 1, applied }), readLaneMovesApplied(storage).state)
}

// moveStoredLayout moves a space's saved dock (and its backup) to another
// space id that has none.
export function moveStoredLayout(storage: Pick<Storage, 'getItem' | 'setItem' | 'removeItem'>, from: string, to: string) {
  if (storage.getItem(spaceLayoutKey(to)) !== null) return
  for (const key of [spaceLayoutBackupKey, spaceLayoutKey]) {
    const raw = storage.getItem(key(from))
    if (raw === null) continue
    storage.setItem(key(to), raw)
    storage.removeItem(key(from))
  }
}

// rekeyRecord moves record[from] to record[to] unless to already has one.
export function rekeyRecord<T>(record: Readonly<Record<string, T>>, from: string, to: string): Record<string, T> {
  if (!(from in record)) return record
  const { [from]: value, ...rest } = record
  return to in rest ? rest : { ...rest, [to]: value }
}
