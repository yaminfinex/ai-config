import {
  writeStoredLayout,
  writeStoredSpaceLayout,
  type LayoutWriteState,
} from './dockLayout.ts'
export type LayoutPersistenceTarget = { mode: 'legacy' } | { mode: 'spaces', activeSpaceID: string }

export function persistLayoutSnapshot(
  storage: Pick<Storage, 'setItem'>,
  target: LayoutPersistenceTarget,
  raw: string,
  state: LayoutWriteState,
) {
  const next = target.mode === 'spaces'
    ? writeStoredSpaceLayout(storage, target.activeSpaceID, raw, state)
    : writeStoredLayout(storage, raw, state)
  return { wrote: next !== state, state: next }
}
