import { useCallback, useEffect, useRef, type MutableRefObject } from 'react'
import type { DockviewApi } from 'dockview-react'
import { composerFieldId, focusComposerWhenReady } from '../../composerState'
import { panelParams } from '../layout/dockLayout'
import { arriveDecision, focusAtEnd, focusOrigin, type ArriveInput } from '../spaces/index.ts'

// The panel arriving renders over the next frames (a restored layout, a new
// tab, a zoom uncovering the dock), so the decision is retried for about a
// second before the panel takes the keys instead.
const arriveFrames = 60

export type Arrive = (origin?: ArriveInput['origin']) => void

// The quick-open search is the palette's own box, not the owner's text
// field: the palette closes on the choice.
function arrivalOrigin(): ArriveInput['origin'] {
  return focusOrigin(document.activeElement?.closest('.quick-open') ? null : document.activeElement)
}

// useArrive is the one path every arrival at an agent takes (see
// arriveDecision): once the zoom's focused panel, or the agent opened alone,
// is drawn, its composer takes the caret when the agent is writable, else
// its panel takes focus. A new arrival cancels the one still waiting.
export function useArrive(apiRef: MutableRefObject<DockviewApi | undefined>, aloneAgent: () => string | undefined): Arrive {
  const cancel = useRef<() => void>(() => undefined)
  useEffect(() => () => cancel.current(), [])
  return useCallback((from) => {
    const origin = from ?? arrivalOrigin()
    let tries = 0
    cancel.current()
    cancel.current = focusComposerWhenReady(() => {
      tries += 1
      const alone = aloneAgent()
      const root = document.querySelector(alone === undefined ? '.dv-active-group' : '.alone-agent')
      const params = alone === undefined ? panelParams(apiRef.current?.activePanel?.params) : { kind: 'agent' as const, name: alone }
      const name = params?.kind === 'agent' ? params.name : undefined
      // The arriving agent's own composer, not one a panel leaving still shows.
      const field = name === undefined ? undefined
        : [...root?.querySelectorAll<HTMLTextAreaElement>('textarea[data-composer]') ?? []].find((candidate) => candidate.id === composerFieldId(name))
      const panel = field?.closest<HTMLElement>('.agent-page') ?? root?.querySelector<HTMLElement>('.agent-page') ?? undefined
      const lastTry = tries > arriveFrames
      const decision = arriveDecision({
        // A text field focused since the arrival began is the owner's too.
        origin: origin === 'text-field' ? origin : arrivalOrigin(),
        activePanelKind: params?.kind,
        composer: field ? { disabled: field.disabled } : null,
        panel: Boolean(panel),
        lastTry,
      })
      if (decision === 'wait') return null
      if (decision === 'leave') return { focus: () => undefined }
      const target = decision === 'composer' ? field! : panel!
      // A dock still covered by the lens cannot take focus yet.
      if (!lastTry && !target.checkVisibility({ visibilityProperty: true })) return null
      return { focus: () => decision === 'composer' ? focusAtEnd(field!) : target.focus({ preventScroll: true }) }
    }, window.requestAnimationFrame.bind(window), arriveFrames, window.cancelAnimationFrame.bind(window))
  }, [aloneAgent, apiRef])
}
