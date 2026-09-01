import { useLayoutEffect, useRef, useState } from 'react'
import { noteSourceLabel } from './notesPresentation.ts'
import { placeCaretAtEnd } from './noteCaptureModel.ts'
import type { Note, NoteSource } from './notesStore.ts'
import { NotesSelector } from './NotesSelector.tsx'
import { useScheduledFrame } from '../../shared/lifecycle.ts'

export type NoteCaptureDraft = { quote: string, source: NoteSource, left: number, top: number, group: string }

export function NoteCaptureChip({ capture, notes, agents, onSave, onAbandon }: {
  capture: NoteCaptureDraft
  notes: Note[]
  agents: string[]
  onSave: (group: string, comment: string) => void
  onAbandon: () => void
}) {
  const [expanded, setExpanded] = useState(false)
  const [comment, setComment] = useState('')
  const [group, setGroup] = useState(capture.group)
  const [selectorQuery, setSelectorQuery] = useState<string>()
  const commentRef = useRef<HTMLTextAreaElement>(null)
  const restore = useRef({ start: 0, end: 0 })
  const seedCaret = useRef(false)
  const scheduleFrame = useScheduledFrame()

  useLayoutEffect(() => {
    if (!expanded || !seedCaret.current || !commentRef.current) return
    seedCaret.current = false
    placeCaretAtEnd(commentRef.current)
  }, [expanded])

  const expandWith = (first = '') => {
    if (first) { setComment(first); seedCaret.current = true }
    setExpanded(true)
    if (!first) scheduleFrame(() => commentRef.current?.focus())
  }
  const rememberCaret = () => {
    const field = commentRef.current
    if (!field) return
    restore.current = { start: field.selectionStart, end: field.selectionEnd }
  }
  const openSelector = (query = '') => { rememberCaret(); setSelectorQuery(query) }
  const returnToComment = () => scheduleFrame(() => {
    const field = commentRef.current
    if (!field) return
    field.focus()
    field.setSelectionRange(restore.current.start, restore.current.end)
  })

  if (!expanded) return <aside className="note-capture-popover note-capture-minimal" role="dialog" aria-label="Capture selected text" style={{ left: capture.left, top: capture.top }}>
    <button autoFocus type="button" className="note-capture-minimal-button" onClick={() => onSave(group, '')} onKeyDown={(event) => {
      if (event.key === 'Escape') { onAbandon(); event.preventDefault(); return }
      if (event.key.length === 1 && !event.metaKey && !event.ctrlKey && !event.altKey) { expandWith(event.key); event.preventDefault() }
    }}>＋ Add note</button>
  </aside>

  return <aside className="note-capture-popover expanded" role="dialog" aria-label="Capture selected text" style={{ left: capture.left, top: capture.top }}
    onKeyDown={(event) => { if (event.key === 'Escape' && selectorQuery === undefined) { onAbandon(); event.preventDefault() } }}>
    <div className="note-capture-quote"><small>{noteSourceLabel(capture.source)}</small><span>{capture.quote}</span></div>
    <textarea ref={commentRef} aria-label="Comment on selected text" value={comment} onChange={(event) => setComment(event.target.value)} placeholder="Add a comment…"
      onKeyDown={(event) => {
        event.stopPropagation()
        if (event.key === 'Escape') { onAbandon(); event.preventDefault(); return }
        if (event.key === 'Enter' && !event.shiftKey && !event.nativeEvent.isComposing) { onSave(group, comment); event.preventDefault() }
      }} />
    <footer className="note-capture-footer">
      <button type="button" className="note-capture-target" onClick={() => openSelector()} onFocus={rememberCaret} onKeyDown={(event) => {
        if (event.key === 'Enter' || event.key === 'ArrowDown') { openSelector(); event.preventDefault(); return }
        if (event.key === 'Backspace' || event.key === 'Delete') { setGroup('general'); event.preventDefault(); return }
        if (event.key.length === 1 && /^[a-z]$/i.test(event.key) && !event.metaKey && !event.ctrlKey && !event.altKey) { openSelector(event.key); event.preventDefault() }
      }}>{group === 'general' ? 'unassigned' : `→ ${group}`}</button>
      <span className="note-capture-confirm"><kbd>esc</kbd><button type="button" className="note-capture-add" onClick={() => onSave(group, comment)}>Add ↵</button></span>
    </footer>
    {selectorQuery !== undefined && <NotesSelector notes={notes} selected={[]} agents={agents} initialValue={group} initialQuery={selectorQuery}
      onCancel={() => { setSelectorQuery(undefined); returnToComment() }} onChoose={(value) => { setGroup(value); setSelectorQuery(undefined); returnToComment() }} />}
  </aside>
}
