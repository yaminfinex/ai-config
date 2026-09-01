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
