import { useEffect, useMemo, useRef, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { getEntries, queryKeys } from '../../api/client.ts'
import type { EntriesPage } from '../../types.ts'
import {
  cardAnswer,
  cardTail,
  dueCardReads,
  keepCardReads,
  landCardRead,
  lastAnswer,
  noCardReads,
  startCardReads,
  type CardRead,
  type CardReads,
  type CardTurn,
} from './cardTextModel.ts'

// useCardText is the text cards' last answers. An agent whose transcript is
// already in the entries cache (the event stream keeps it live) answers from
// there; every other carded agent gets one tail read per turn, at most four
// in flight. answer(agent) is null until one has loaded.
const cardedMap = (carded: readonly CardRead[]) => new Map<string, CardTurn>(carded.map((read) => [read.agent, read.turn]))

export function useCardText(carded: readonly CardRead[], onBoard: (agent: string) => boolean) {
  const queryClient = useQueryClient()
  // The carded agents (agent → current turn), kept while they read the same; a read lands against the latest.
  const cardedKey = carded.map((read) => `${read.agent}\u0000${read.turn ?? ''}`).join('\u0001')
  const [wanted, setWanted] = useState(() => ({ key: cardedKey, map: cardedMap(carded) }))
  if (wanted.key !== cardedKey) setWanted({ key: cardedKey, map: cardedMap(carded) })
  const wantedRef = useRef(wanted.map)
  const [reads, setReads] = useState<CardReads>(noCardReads)
  const readsRef = useRef(reads)

  // A cached transcript's update redraws the cards that answer from it.
  const [cacheTick, setCacheTick] = useState(0)
  useEffect(() => queryClient.getQueryCache().subscribe((event) => {
    const [kind, agent] = event.query.queryKey as readonly unknown[]
    if (kind === 'entries' && typeof agent === 'string' && wantedRef.current.has(agent) && (event.type === 'updated' || event.type === 'added' || event.type === 'removed')) setCacheTick((tick) => tick + 1)
  }), [queryClient])
  const cached = useMemo(() => {
    void cacheTick
    const answers = new Map<string, string | null>()
    for (const agent of wanted.map.keys()) {
      const page = queryClient.getQueryData<EntriesPage>(queryKeys.entries(agent))
      if (page?.entries?.length) answers.set(agent, lastAnswer(page.entries))
    }
    return answers
  }, [cacheTick, queryClient, wanted])

  // A read lands against the cards as they are then, so one in flight
  // when they change still holds its slot until it lands (or is dropped).
  const mounted = useRef(true)
  const cachedRef = useRef(cached)
  const pump = useRef(() => {})
  useEffect(() => {
    mounted.current = true
    const commit = (next: CardReads) => {
      if (next === readsRef.current) return
      readsRef.current = next
      setReads(next)
    }
    pump.current = () => {
      if (!mounted.current) return
      const due = dueCardReads(readsRef.current, new Map([...wantedRef.current].filter(([agent]) => !cachedRef.current.has(agent))))
      if (due.length === 0) return
      commit(startCardReads(readsRef.current, due))
      for (const read of due) {
        void getEntries(read.agent, { limit: cardTail }).then((page) => page.entries ?? [], () => null).then((entries) => {
          if (!mounted.current) return
          commit(landCardRead(readsRef.current, read, entries, wantedRef.current))
          pump.current()
        })
      }
    }
    return () => { mounted.current = false }
  }, [])
  useEffect(() => {
    wantedRef.current = wanted.map
    cachedRef.current = cached
    const kept = keepCardReads(readsRef.current, onBoard)
    if (kept !== readsRef.current) {
      readsRef.current = kept
      setReads(kept)
    }
    pump.current()
  }, [cached, onBoard, wanted])

  return useMemo(() => (agent: string) => cached.has(agent) ? cached.get(agent) ?? cardAnswer(reads, agent) : cardAnswer(reads, agent), [cached, reads])
}
