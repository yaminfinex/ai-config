import { useMemo, useState } from 'react'
import { useDOMEvent } from '../../shared/lifecycle'
import { storedSpaceAgents, type SpaceDefinition } from '../spaces/index.ts'

// useSpaceAgents names each space's open agents: the live dock's for the
// active space, the stored layout's for the others (rewritten by a switch,
// a send-to-space or another tab).
export function useSpaceAgents({ revision, spaces, activeSpaceID, activeAgents }: {
  revision: number
  spaces: SpaceDefinition[]
  activeSpaceID: string | null
  activeAgents: string[]
}) {
  const [storageTick, setStorageTick] = useState(0)
  useDOMEvent<StorageEvent>(window, 'storage', (event) => {
    if (event.key?.startsWith('herder.web.layout.v4')) setStorageTick((tick) => tick + 1)
  })
  const activeKey = activeAgents.join('\n')
  return useMemo(() => {
    const result: Record<string, string[]> = {}
    for (const space of spaces) {
      result[space.id] = space.id === activeSpaceID ? activeKey.split('\n').filter(Boolean) : storedSpaceAgents(localStorage, space.id)
    }
    return result
    // revision covers stored layouts rewritten by a switch or a send-to-space;
    // storageTick covers another tab's.
  }, [activeKey, activeSpaceID, revision, spaces, storageTick])
}
