import { isMacPlatform } from './shellShortcuts.ts'

export type OpenPlacement = {
  direction: 'within' | 'right'
  groupID?: string
}

export function placementFromModifiers(event: { altKey: boolean }, groupID?: string): OpenPlacement {
  return { direction: event.altKey ? 'right' : 'within', ...(groupID ? { groupID } : {}) }
}

export function placementInGroup(placement: OpenPlacement | undefined, groupID: string): OpenPlacement {
  return { direction: placement?.direction ?? 'within', groupID }
}

export function openInSideLabel(userAgent: string) {
  return `${openInSideKeys(userAgent)}: open in side split`
}

export function openInSideKeys(userAgent: string) {
  return `${isMacPlatform(userAgent) ? '⌥' : 'Alt'}+click`
}

type DockGroups = { activeGroupID?: string, firstGroupID?: string }
type NewDockTarget = { kind: 'new', groupID: string | undefined, position?: { referenceGroup: string, direction: 'within' | 'right' } }

export function dockOpenTarget(existing: undefined, placement: OpenPlacement | undefined, groups: DockGroups): NewDockTarget
export function dockOpenTarget<T>(existing: T | undefined, placement: OpenPlacement | undefined, groups: DockGroups): { kind: 'existing', panel: T } | NewDockTarget
export function dockOpenTarget(existing: unknown, placement: OpenPlacement | undefined, groups: {
  activeGroupID?: string
  firstGroupID?: string
}) {
  if (existing) return { kind: 'existing' as const, panel: existing }
  const direction = placement?.direction ?? 'within'
  const groupID = direction === 'right'
    ? groups.activeGroupID ?? placement?.groupID ?? groups.firstGroupID
    : placement?.groupID ?? groups.activeGroupID ?? groups.firstGroupID
  return {
    kind: 'new' as const,
    groupID: direction === 'within' ? groupID : undefined,
    ...(groupID ? { position: { referenceGroup: groupID, direction } } : {}),
  }
}
