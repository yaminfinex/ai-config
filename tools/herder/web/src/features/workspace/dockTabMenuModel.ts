import type { SpaceDefinition } from '../spaces/spacesModel.ts'

export type DockTabMenuItem =
  | { id: string, label: string, kind: 'space' }
  | { id: 'new', label: 'Send to new space', kind: 'new' }

export function dockTabMenuItems(spaces: SpaceDefinition[], activeSpaceID: string | null): DockTabMenuItem[] {
  return [
    ...spaces.flatMap((space): DockTabMenuItem[] => space.id === activeSpaceID ? [] : [{
      id: space.id,
      label: `Send to ${space.name}`,
      kind: 'space',
    }]),
    { id: 'new', label: 'Send to new space', kind: 'new' },
  ]
}

export function isDockTabMenuKey(event: Pick<KeyboardEvent, 'key' | 'shiftKey'>) {
  return event.key === 'ContextMenu' || event.key === 'F10' && event.shiftKey
}

export function dockTabMenuNavigationIndex(key: string, current: number, count: number) {
  if (count <= 0) return null
  if (key === 'ArrowDown') return (Math.max(current, -1) + 1) % count
  if (key === 'ArrowUp') return current <= 0 ? count - 1 : current - 1
  if (key === 'Home') return 0
  if (key === 'End') return count - 1
  return null
}
