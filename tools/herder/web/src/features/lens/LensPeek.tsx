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
import { Composer } from '../composer/Composer'
import { useNotes } from '../notes/NotesProvider'
import { queueComposerNote } from '../notes/noteQueue'
import { LensGlyph } from './LensGlyph.tsx'
import type { LensStatus } from './lensModel.ts'
import { maxPeekWidth, minPeekWidth, peekWidthFromKey, peekWindow, resizedPeekWidth } from './peekModel.ts'

// PeekTarget is what the peek shows: the selected card's space and its
// visible agent, null when the card has none on the board.
export type PeekTarget = { space: string, agent: string | null, status: LensStatus | null, title: string }

// PeekReply is the peek's reply to its agent: / opens it, focus asks it
// for the keys again.
export type PeekReply = { agent: string, focus: number }

// focusLens gives the keys to the cards.
export function focusLens() {
  document.querySelector<HTMLElement>('.lens')?.focus({ preventScroll: true })
}

// leavePeek gives the keys back to the cards when they are in the peek or
// on its resize edge (esc).
export function leavePeek() {
  if (!document.querySelector('.lens') || !document.activeElement?.closest('.lens-peek, .lens-peek-resizer')) return false
  focusLens()
  return true
}

const noop = () => undefined

// ReplyComposer is the zoom's Composer at the foot of the peek: the same
// draft, send and note queue. ⌘⏎ sends, and once it lands onSent's answer
// marks the agent read and closes it; esc closes it with the draft kept. A
// failed send keeps the text open here and says why.
function ReplyComposer({ reply, identityReadOnly, onViewer, onSent, onLeave }: {
  reply: PeekReply
  identityReadOnly: string
  onViewer: (viewer: string) => void
  onSent: (agent: string) => () => void
  onLeave: () => void
}) {
  const { agent, focus } = reply
  const box = useRef<HTMLDivElement | null>(null)
  const [problem, setProblem] = useState('')
  const { store, announce } = useNotes()
  useEffect(() => { box.current?.querySelector<HTMLTextAreaElement>('textarea[data-composer]')?.focus() }, [focus])
  return <div className="lens-peek-reply" ref={box} onKeyDown={(event) => { if (event.key === 'Escape') onLeave() }}>
    {problem && <p className="inline-error lens-peek-problem" role="alert">{problem}</p>}
    <Composer name={agent} identityReadOnly={identityReadOnly} onNotesFocus={noop} onViewer={onViewer} onProblem={setProblem} onSend={noop}
      onSent={() => onSent(agent)} onQueue={(text) => {
        const result = queueComposerNote(store, agent, text)
        if (!result.ok) return result
        announce(`Queued a note for ${agent}.`)
        return { ok: true }
      }} />
  </div>
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

// LensPeek is the lens's right pane: a look at the selected card's visible
// agent, read-only but for the reply / opens at its foot. It never takes
// focus on its own; a click inside still leaves the keys with the lens, and
// esc brings them back from a link or button tabbed to. Its left edge
// resizes it.
export function LensPeek({ target, width, onWidth, mentionMatcher, onOpenAgent, reply, identityReadOnly, onViewer, onReplySent, onLeaveReply }: {
  target: PeekTarget | null
  width: number
  onWidth: (width: number) => void
  mentionMatcher: AgentMentionMatcher
  onOpenAgent: (name: string) => void
  reply: PeekReply | null
  identityReadOnly: string
  onViewer: (viewer: string) => void
  onReplySent: (agent: string) => () => void
  onLeaveReply: () => void
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
        {reply?.agent === target.agent && <ReplyComposer key={reply.agent} reply={reply} identityReadOnly={identityReadOnly} onViewer={onViewer} onSent={onReplySent} onLeave={onLeaveReply} />}
      </> : <p className="lens-peek-none">{target ? 'No agent on the board' : 'No space selected'}</p>}
    </aside>
  </>
}
