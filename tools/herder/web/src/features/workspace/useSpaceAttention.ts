import { useEffect, useMemo, useRef, useState, type MutableRefObject } from 'react'
import type { DockviewApi } from 'dockview-react'
import type { Board } from '../../types'
import { useDOMEvent } from '../../shared/lifecycle'
import { panelParams } from '../layout/dockLayout'
import {
  dwelledAgents,
  markViewedRead,
  nextDwellDelay,
  nextViewing,
  readMarkersKey,
  parseReadMarkers,
  pruneReadMarkers,
  readReadMarkers,
  seedReadMarkers,
  spaceAttention,
  storedSpaceAgents,
  viewedAgents,
  writeReadMarkers,
  type SpaceAttention,
  type SpaceDefinition,
  type ViewingState,
} from '../spaces/index.ts'

function documentVisible() {
  return typeof document === 'undefined' || document.visibilityState === 'visible'
}

// useSpaceAttention derives each space's waiting/blocked agents from the
// live dock (active space), the stored layouts (other spaces), the fleet
// board and this browser's read markers.
export function useSpaceAttention({ apiRef, revision, board, spaces, activeSpaceID, activeAgents }: {
  apiRef: MutableRefObject<DockviewApi | undefined>
  revision: number
  board: Board | undefined
  spaces: SpaceDefinition[]
  activeSpaceID: string | null
  activeAgents: string[]
}) {
  const [initialMarkers] = useState(() => readReadMarkers(localStorage))
  const [markers, setMarkers] = useState(initialMarkers.markers)
  const markerState = useRef(initialMarkers.state)
  const [visible, setVisible] = useState(documentVisible)
  const [storageTick, setStorageTick] = useState(0)
  const [viewing, setViewing] = useState<ViewingState>({})
  const [dwellTick, setDwellTick] = useState(0)

  useDOMEvent(document, 'visibilitychange', () => setVisible(documentVisible()))
  useDOMEvent<StorageEvent>(window, 'storage', (event) => {
    if (event.key === readMarkersKey) {
      const next = parseReadMarkers(event.newValue)
      if (next) {
        markerState.current = { recovering: false, lastGoodRaw: event.newValue }
        setMarkers(next)
      }
    } else if (event.key?.startsWith('herder.web.layout.v4')) setStorageTick((tick) => tick + 1)
  })

  const activeKey = activeAgents.join('\n')
  const openBySpace = useMemo(() => {
    const result: Record<string, string[]> = {}
    for (const space of spaces) {
      result[space.id] = space.id === activeSpaceID ? activeKey.split('\n').filter(Boolean) : storedSpaceAgents(localStorage, space.id)
    }
    return result
    // revision covers stored layouts rewritten by a switch or a send-to-space.
  }, [activeKey, activeSpaceID, revision, spaces, storageTick])

  const api = apiRef.current
  const viewedKey = viewedAgents((api?.groups ?? []).map((group) => {
    const params = panelParams(group.activePanel?.params)
    return { visible: group.api.isVisible, activeAgent: params?.kind === 'agent' ? params.name : undefined }
  }), visible).join('\n')
  useEffect(() => {
    setViewing((previous) => nextViewing(previous, viewedKey.split('\n').filter(Boolean), Date.now()))
  }, [viewedKey])
  useEffect(() => {
    const delay = nextDwellDelay(viewing, Date.now())
    if (delay === null) return
    const timer = window.setTimeout(() => setDwellTick((tick) => tick + 1), delay)
    return () => window.clearTimeout(timer)
  }, [dwellTick, viewing])

  useEffect(() => {
    const open = Object.values(openBySpace).flat()
    setMarkers((current) => {
      const seeded = seedReadMarkers(pruneReadMarkers(current, board, open), board, open)
      return markViewedRead(seeded, board, dwelledAgents(viewing, Date.now()))
    })
  }, [board, dwellTick, openBySpace, viewing])
  useEffect(() => {
    markerState.current = writeReadMarkers(localStorage, markers, markerState.current)
  }, [markers])

  const attention = useMemo(() => Object.fromEntries(Object.entries(openBySpace)
    .map(([id, agents]) => [id, spaceAttention(board, agents, markers)])) as Record<string, SpaceAttention>, [board, markers, openBySpace])
  return { attention }
}
