import { useCallback, useEffect, useRef, useState } from 'react'
import { bindSpaceSwitcher } from '../layout/shellShortcuts'
import { idleSwitcher, reduceSwitcher, switcherRevealDelayMs, type SwitcherEvent } from '../spaces/index.ts'

// useSpaceSwitcher owns the ⌥Tab switcher: the reducer state, the delayed
// reveal and the window key bindings. mruOrder is read when ⌥Tab opens it.
export function useSpaceSwitcher({ enabled, mruOrder, switchSpace }: {
  enabled: boolean
  mruOrder: readonly string[]
  switchSpace: (id: string) => boolean
}) {
  const [state, setState] = useState(idleSwitcher)
  const stateRef = useRef(state)
  const orderRef = useRef(mruOrder)
  useEffect(() => { orderRef.current = mruOrder }, [mruOrder])

  const dispatch = useCallback((event: SwitcherEvent) => {
    const result = reduceSwitcher(stateRef.current, event)
    stateRef.current = result.state
    setState(result.state)
    if (result.commit) switchSpace(result.commit)
    return result.state
  }, [switchSpace])

  const reveal = state.phase === 'holding' && !state.shown
  useEffect(() => {
    if (!reveal) return
    const timer = window.setTimeout(() => dispatch({ type: 'show' }), switcherRevealDelayMs)
    return () => window.clearTimeout(timer)
  }, [dispatch, reveal])

  useEffect(() => bindSpaceSwitcher(window, {
    cycle: (direction) => {
      if (!enabled) return false
      return dispatch({ type: 'cycle', direction, order: orderRef.current }).phase === 'holding'
    },
    holding: () => stateRef.current.phase === 'holding',
    intent: (intent) => dispatch(intent === 'forward' || intent === 'backward' ? { type: 'step', direction: intent } : { type: intent }),
  }), [dispatch, enabled])

  const choose = useCallback((id: string) => { dispatch({ type: 'choose', id }) }, [dispatch])
  const cancel = useCallback(() => { dispatch({ type: 'cancel' }) }, [dispatch])
  return { state, choose, cancel }
}
