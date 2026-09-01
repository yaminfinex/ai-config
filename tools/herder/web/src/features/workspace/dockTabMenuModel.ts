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
