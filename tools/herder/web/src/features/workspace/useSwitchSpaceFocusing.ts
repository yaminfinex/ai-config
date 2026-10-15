import { useCallback } from 'react'
import { focusOrigin } from '../spaces/index.ts'
import type { Arrive } from './useArrive'

// useSwitchSpaceFocusing wraps switchSpace for the switches inside a zoom
// (the Spaces list, the ⌥Tab switcher, a ⌘K space row): the new space's
// focused agent is arrived at like any other (useArrive). The origin is read
// before the old layout unmounts. ⌘K passes from 'quick-open' (its search box
// is not the owner's text field), and choosing the space already showing
// there arrives at its agent again.
export function useSwitchSpaceFocusing(switchSpace: (id: string) => boolean, activeSpaceID: () => string | null, arrive: Arrive) {
  return useCallback((spaceID: string, from?: 'quick-open') => {
    const origin = from ?? focusOrigin(document.activeElement)
    const refocus = from === 'quick-open' && activeSpaceID() === spaceID
    if (!refocus && !switchSpace(spaceID)) return false
    arrive(origin)
    return true
  }, [activeSpaceID, arrive, switchSpace])
}
