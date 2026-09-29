import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { bindSpaceSwitcher } from '../layout/shellShortcuts'
import {
  idleSwitcher,
  mruSpaceIDs,
  readSpaceMRU,
  reduceSwitcher,
  touchSpaceMRU,
  writeSpaceMRU,
  type SpaceDefinition,
  type SwitcherEvent,
} from '../spaces/index.ts'

// useSpaceSwitcher owns the ⌥Tab switcher: the persisted MRU space order,
// the reducer state and the window key bindings. The
// MRU order is read when ⌥Tab opens it.
export function useSpaceSwitcher({ enabled, spaces, activeSpaceID, switchSpace }: {
  enabled: boolean
  spaces: SpaceDefinition[]
  activeSpaceID: string | null
  switchSpace: (id: string) => boolean
}) {
  const [initialMRU] = useState(() => readSpaceMRU(localStorage))
  const [mru, setMRU] = useState<readonly string[]>(initialMRU.order)
  const mruState = useRef(initialMRU.state)
  useEffect(() => {
    if (activeSpaceID) setMRU((order) => touchSpaceMRU(order, activeSpaceID))
  }, [activeSpaceID])
  useEffect(() => {
    mruState.current = writeSpaceMRU(localStorage, mru, mruState.current)
  }, [mru])
  const mruOrder = useMemo(() => mruSpaceIDs(mru, spaces, activeSpaceID), [activeSpaceID, mru, spaces])

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
