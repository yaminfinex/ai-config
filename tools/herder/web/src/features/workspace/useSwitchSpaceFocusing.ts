import { useCallback, useEffect, useRef, type MutableRefObject } from 'react'
import type { DockviewApi } from 'dockview-react'
import { focusComposerWhenReady } from '../../composerState'
import { panelParams } from '../layout/dockLayout'
import { focusAtEnd, focusOrigin, switchFocusDecision } from '../spaces/index.ts'

// useSwitchSpaceFocusing wraps switchSpace for the deliberate switches (rail
// click, ⌥Tab switcher, ⇧⌥←/→): after the new layout mounts, the active
// agent's composer takes focus with the caret after its draft, as decided by
// switchFocusModel. The restored panels render over the next frames, so the
// decision is retried until the composer appears.
const composerFrames = 60

export function useSwitchSpaceFocusing(apiRef: MutableRefObject<DockviewApi | undefined>, switchSpace: (id: string) => boolean) {
  const cancel = useRef<() => void>(() => undefined)
  useEffect(() => () => cancel.current(), [])
  return useCallback((spaceID: string) => {
    const origin = focusOrigin(document.activeElement)
    if (!switchSpace(spaceID)) return false
    cancel.current()
    cancel.current = focusComposerWhenReady(() => {
      const api = apiRef.current
      const field = document.querySelector<HTMLTextAreaElement>('.dv-active-group textarea[data-composer]')
      const decision = switchFocusDecision({
        // A text field focused since the switch began is the owner's too.
        origin: origin === 'text-field' ? origin : focusOrigin(document.activeElement),
        activePanelKind: panelParams(api?.activePanel?.params)?.kind,
        composer: field ? { disabled: field.disabled, visible: Boolean(api?.activeGroup?.api.isVisible) && field.getClientRects().length > 0 } : null,
      })
      if (decision === 'wait') return null
      return { focus: () => { if (decision === 'focus' && field) focusAtEnd(field) } }
    }, requestAnimationFrame, composerFrames, cancelAnimationFrame)
    return true
  }, [apiRef, switchSpace])
}
