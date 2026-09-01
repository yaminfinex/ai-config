import type { SpaceDefinition, SpaceResult } from './spacesModel.ts'

export type SpaceDockSource<Dock> = {
  stored: { dock: Dock | null } | null
  backup: { dock: Dock | null } | null
  problem?: boolean
}

type DockTarget = { clear: () => void }

export function restoreSpaceDock<Target extends DockTarget, Dock>(
  target: Target,
  source: SpaceDockSource<Dock>,
  restore: (target: Target, dock: Dock) => boolean,
  recoveredFromBackup: () => void,
) {
  let restored = false
  let restoreFailed = Boolean(source.problem)
  if (source.stored?.dock) {
    target.clear()
    restored = restore(target, source.stored.dock)
    restoreFailed ||= !restored
  } else target.clear()
  if (!restored && source.backup?.dock && source.backup !== source.stored) {
    target.clear()
    restored = restore(target, source.backup.dock)
    restoreFailed ||= !restored
    if (restored) recoveredFromBackup()
  }
  if (!restored && restoreFailed) target.clear()
  return { restored, restoreFailed }
}

type SwitchDependencies<Target extends DockTarget, Dock> = {
  flush: () => boolean
  suspend: () => void
  read: (id: string) => SpaceDockSource<Dock>
  withHistorySuppressed: <T>(operation: () => T) => T
  dock: Target
  restore: (target: Target, dock: Dock) => boolean
  recoveredFromBackup: (id: string) => void
  complete: (id: string) => void
  persistActive: (id: string) => boolean
  replaceStamp: () => void
  finish: (result: { restoreFailed: boolean, activeSaved: boolean }) => void
}

export function performSpaceSwitch<Target extends DockTarget, Dock>(spaceID: string, dependencies: SwitchDependencies<Target, Dock>) {
  if (!dependencies.flush()) return false
  dependencies.suspend()
  const source = dependencies.read(spaceID)
  const result = dependencies.withHistorySuppressed(() => restoreSpaceDock(
    dependencies.dock,
    source,
    dependencies.restore,
    () => dependencies.recoveredFromBackup(spaceID),
  ))
  dependencies.complete(spaceID)
  const activeSaved = dependencies.persistActive(spaceID)
  dependencies.replaceStamp()
  dependencies.finish({ restoreFailed: result.restoreFailed, activeSaved })
  return true
}

type CloseDependencies = {
  create: () => SpaceResult<SpaceDefinition>
  rollbackCreate: (id: string) => boolean
  switchTo: (id: string) => boolean
}

export function moveBeforeActiveClose(
  closingID: string,
  activeID: string | null,
  spaces: SpaceDefinition[],
  dependencies: CloseDependencies,
): SpaceResult<void> {
  if (closingID !== activeID) return { ok: true, value: undefined }
  const index = spaces.findIndex((space) => space.id === closingID)
  let neighbor = spaces[index + 1] ?? spaces[index - 1]
  let replacement: SpaceDefinition | undefined
  if (!neighbor) {
    const created = dependencies.create()
    if (!created.ok) return created
    replacement = created.value
    neighbor = replacement
  }
  try {
    if (dependencies.switchTo(neighbor.id)) return { ok: true, value: undefined }
  } catch { /* the replacement rollback below still owns convergence */ }
  if (replacement) dependencies.rollbackCreate(replacement.id)
  return { ok: false, reason: 'This space could not switch away before closing. Nothing was discarded.' }
}

type PanelWriteResult = { ok: true, duplicate: boolean } | { ok: false, reason: string }
type ExistingPanelSendDependencies<Panel> = {
  write: (spaceID: string, panel: Panel) => PanelWriteResult
  closeSource: () => void
}

export function sendPanelToExistingSpace<Panel>(
  spaceID: string,
  panel: Panel,
  dependencies: ExistingPanelSendDependencies<Panel>,
): SpaceResult<{ spaceID: string, duplicate: boolean }> {
  const written = dependencies.write(spaceID, panel)
  if (!written.ok) return { ok: false, reason: `This pane could not be sent because the target space is ${written.reason}. Nothing was discarded.` }
  dependencies.closeSource()
  return { ok: true, value: { spaceID, duplicate: written.duplicate } }
}

type NewPanelSendDependencies<Panel> = ExistingPanelSendDependencies<Panel> & {
  create: () => SpaceResult<SpaceDefinition>
  rollbackCreate: (id: string) => boolean
  flush: () => boolean
}

export function sendPanelToNewSpace<Panel>(
  panel: Panel,
  dependencies: NewPanelSendDependencies<Panel>,
): SpaceResult<{ spaceID: string, duplicate: boolean }> {
  const created = dependencies.create()
  if (!created.ok) return created
  const written = dependencies.write(created.value.id, panel)
  if (!written.ok) {
    dependencies.rollbackCreate(created.value.id)
    return { ok: false, reason: `This pane could not be sent because the new space layout could not be written. Nothing was discarded.` }
  }
  dependencies.flush()
  dependencies.closeSource()
  return { ok: true, value: { spaceID: created.value.id, duplicate: written.duplicate } }
}

type CreateAndSwitchDependencies = {
  create: () => SpaceResult<SpaceDefinition>
  rename: (id: string, name: string) => SpaceResult<SpaceDefinition>
  switchTo: (id: string) => boolean
  rollbackCreate: (id: string) => boolean
  flush: () => boolean
}

export function createAndSwitchSpace(name: string, dependencies: CreateAndSwitchDependencies): SpaceResult<SpaceDefinition> {
  const created = dependencies.create()
  if (!created.ok) return created
  const renamed = dependencies.rename(created.value.id, name)
  if (!renamed.ok) {
    dependencies.rollbackCreate(created.value.id)
    return renamed
  }
  let switched = false
  try { switched = dependencies.switchTo(created.value.id) } catch { /* rollback remains authoritative */ }
  if (!switched) {
    dependencies.rollbackCreate(created.value.id)
    return { ok: false, reason: 'This space could not be opened. Nothing was discarded.' }
  }
  dependencies.flush()
  return renamed
}

export function spaceIDInDirection(spaces: SpaceDefinition[], activeID: string | null, direction: 'previous' | 'next') {
  if (spaces.length === 0) return null
  const index = spaces.findIndex((space) => space.id === activeID)
  if (index < 0) return spaces[0].id
  return spaces[(index + (direction === 'next' ? 1 : -1) + spaces.length) % spaces.length].id
}
