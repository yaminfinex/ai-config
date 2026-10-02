import type { SpaceDefinition } from '../spaces/spacesModel.ts'

export type DockTabMenuItem =
  | { id: string, label: string, kind: 'space' }
  | { id: 'new', label: 'Send to new space', kind: 'new' }
  | { id: 'reassign', label: 'Reassign…', kind: 'reassign', subject: string }
  | { id: 'unread', label: 'Mark unread', kind: 'unread', subject: string }

export function dockTabMenuItems(spaces: SpaceDefinition[], activeSpaceID: string | null, subject?: string): DockTabMenuItem[] {
  return [
    ...spaces.flatMap((space): DockTabMenuItem[] => space.id === activeSpaceID ? [] : [{
      id: space.id,
      label: `Send to ${space.name}`,
      kind: 'space',
    }]),
    { id: 'new', label: 'Send to new space', kind: 'new' },
    ...subject ? [
      { id: 'reassign' as const, label: 'Reassign…' as const, kind: 'reassign' as const, subject },
      { id: 'unread' as const, label: 'Mark unread' as const, kind: 'unread' as const, subject },
    ] : [],
  ]
}

export function isDockTabMenuKey(event: Pick<KeyboardEvent, 'key' | 'shiftKey'>) {
  return event.key === 'ContextMenu' || event.key === 'F10' && event.shiftKey
}

export {
  menuFocusAction as dockTabMenuFocusAction,
  menuKeyAction as dockTabMenuKeyAction,
  menuNavigationIndex as dockTabMenuNavigationIndex,
  type MenuKeyAction as DockTabMenuKeyAction,
} from '../../shared/menuModel.ts'
