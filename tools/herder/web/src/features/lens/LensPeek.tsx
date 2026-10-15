import { useEffect, useRef, useState, type MouseEvent } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { entriesQueryOptions } from '../../api/queries'
import { startPointerDrag } from '../../shared/lifecycle'
import { transcriptNotice } from '../../shared/loadingPresentation'
import { useFollowScroll } from '../../shared/useFollowScroll'
import type { AgentMentionMatcher } from '../../shared/agentMentions'
import { dividerIndex, useReadMarker } from '../spaces/index.ts'
import { TranscriptEntries } from '../transcript/TranscriptEntries'
import { openInSideLabel } from '../layout/openPlacement'
import { LensGlyph } from './LensGlyph.tsx'
import type { LensStatus } from './lensModel.ts'
import { maxPeekWidth, minPeekWidth, peekWidthFromKey, peekWindow, resizedPeekWidth } from './peekModel.ts'

// PeekTarget is what the peek shows: the selected card's space and its
// visible agent, null when the card has none on the board.
export type PeekTarget = { space: string, agent: string | null, status: LensStatus | null, title: string }

// leavePeek gives the keys back to the cards when they are in the peek or
// on its resize edge (esc).
export function leavePeek() {
  const lens = document.querySelector<HTMLElement>('.lens')
  if (!lens || !document.activeElement?.closest('.lens-peek, .lens-peek-resizer')) return false
  lens.focus({ preventScroll: true })
  return true
}

function PeekTranscript({ agent, mentionMatcher, onOpenAgent }: { agent: string, mentionMatcher: AgentMentionMatcher, onOpenAgent: (name: string) => void }) {
  const queryClient = useQueryClient()
  // The shared entries cache: the event stream keeps it live, as for a panel.
  const entriesQuery = useQuery(entriesQueryOptions(queryClient, agent))
  const all = entriesQuery.data?.entries ?? []
  // The unread line where this browser's marker stands; the peek never moves it.
  const marker = useReadMarker(agent)
  const shown = peekWindow(all, dividerIndex(all, entriesQuery.data?.sessionId, marker?.pos ?? null))
  const follow = useFollowScroll<HTMLDivElement>(shown.entries)
  const [now, setNow] = useState(Date.now())
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 30_000)
    return () => window.clearInterval(timer)
  }, [])
  const notice = transcriptNotice(entriesQuery.isPending, entriesQuery.error?.message ?? '')
  const openMention = (name: string, event: MouseEvent<HTMLElement>) => {
    event.stopPropagation()
    onOpenAgent(name)
  }
  return <div className="lens-peek-body transcript" aria-label={`${agent} transcript`} ref={follow.viewportRef} onScroll={follow.onScroll}>
    {notice ? <p className="empty" role={notice.tone === 'error' ? 'alert' : 'status'}>{notice.detail}</p>
      : shown.entries.length === 0 ? <p className="empty">No entries yet.</p>
        : <TranscriptEntries entries={shown.entries} agentName={agent} now={now} showSystem={false} cleanView mentionMatcher={mentionMatcher}
          onOpenAgent={openMention} sideHint={openInSideLabel(navigator.userAgent)} dividerAt={shown.dividerAt} />}
  </div>
}

// LensPeek is the lens's right pane: a read-only look at the selected card's
// visible agent. It never takes focus on its own; a click inside still
// leaves the keys with the lens, and esc brings them back from a link or
// button tabbed to. Its left edge resizes it.
export function LensPeek({ target, width, onWidth, mentionMatcher, onOpenAgent }: {
  target: PeekTarget | null
  width: number
  onWidth: (width: number) => void
  mentionMatcher: AgentMentionMatcher
  onOpenAgent: (name: string) => void
}) {
  const disposeDrag = useRef<(() => void) | null>(null)
  useEffect(() => () => disposeDrag.current?.(), [])
  const startResize = (event: React.PointerEvent<HTMLDivElement>) => {
    const startX = event.clientX
    const lensWidth = event.currentTarget.parentElement?.getBoundingClientRect().width ?? 0
    disposeDrag.current?.()
    disposeDrag.current = startPointerDrag(event, (moveEvent) => onWidth(resizedPeekWidth(width, lensWidth, moveEvent.clientX - startX)))
  }
  const percent = Math.round(width * 100)
  return <>
    <div className="lens-peek-resizer" role="separator" aria-label="Resize peek" aria-orientation="vertical"
      aria-valuemin={minPeekWidth * 100} aria-valuemax={maxPeekWidth * 100} aria-valuenow={percent} tabIndex={0}
      onPointerDown={startResize} onKeyDown={(event) => {
        if (event.key !== 'ArrowLeft' && event.key !== 'ArrowRight') return
        onWidth(peekWidthFromKey(width, event.key))
        event.preventDefault()
      }} />
    <aside className="lens-peek" aria-label="Peek" style={{ width: `${width * 100}%` }}>
      {target?.agent ? <>
        <header className="lens-peek-head">
          <LensGlyph status={target.status} /><strong className="lens-peek-agent">{target.agent}</strong>
          <span className="lens-peek-space">{target.space}</span>
          {target.title && <span className="lens-peek-title">{target.title}</span>}
        </header>
        <PeekTranscript key={target.agent} agent={target.agent} mentionMatcher={mentionMatcher} onOpenAgent={onOpenAgent} />
      </> : <p className="lens-peek-none">{target ? 'No agent on the board' : 'No space selected'}</p>}
    </aside>
  </>
}
