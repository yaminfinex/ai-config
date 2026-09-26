import { useEffect, useState } from 'react'
import { appendComposerDraft, composerFieldId } from '../../composerState'
import { NoteQuickAdd } from './NoteQuickAdd.tsx'
import { NotesList } from './NotesList.tsx'
import { noteTransferText } from './notesPresentation.ts'
import { handOffSelectedNotes } from './noteHandOff.ts'
import { sendAllToComposerLabel } from './notesListModel.ts'
import type { Note } from './notesStore.ts'
import { useGroupNotes, useNotes } from './NotesProvider.tsx'
import { useScheduledFrame } from '../../shared/lifecycle.ts'

export function AgentNotesStrip({ agent, agents, focusRequest }: { agent: string, agents: string[], focusRequest: number }) {
  const notes = useGroupNotes(agent)
  const scheduleFrame = useScheduledFrame()
  const [collapsed, setCollapsed] = useState(true)
  const [editing, setEditing] = useState(false)
  // One-shot: the request un-collapses the strip, the list lands on it (child effects run first), then it clears so no later remount replays it.
  const [expandedBy, setExpandedBy] = useState(0)
  useEffect(() => {
    if (!focusRequest) return
    setCollapsed(false)
    setExpandedBy(focusRequest)
  }, [focusRequest])
  useEffect(() => { if (expandedBy) setExpandedBy(0) }, [expandedBy])
  const { store, announce, handOffGuard } = useNotes()
  const appendToComposer = (target: string, handed: Note[]) => {
    const result = appendComposerDraft(target, handed.map((note) => noteTransferText(note)))
    if (result.ok) scheduleFrame(() => document.getElementById(composerFieldId(target))?.focus())
    return result.ok ? { ok: true as const } : result
  }
  // Send all runs the list's own hand-off (same function, guard, append) over every note of this agent, in list order.
  const sendAll = () => {
    const pending = notes
    const result = handOffSelectedNotes({ target: agent, notes: pending, guard: handOffGuard, append: appendToComposer, remove: store.delete, flush: store.flush, status: store.status })
    announce(result.ok ? `Moved ${pending.length} ${pending.length === 1 ? 'note' : 'notes'} to ${agent}’s composer.` : result.reason)
  }
  const count = notes.length
  if (count === 0 && !editing) return null
  return <section className="agent-notes-strip" aria-label={`${agent} notes`}>
    <header className="agent-notes-header">
      <button type="button" className="agent-notes-toggle" aria-expanded={!collapsed} onClick={() => setCollapsed((current) => !current)}><span aria-hidden="true">{collapsed ? '▸' : '▾'}</span> Notes <span>{count}</span></button>
      {count > 0 && <button type="button" className="note-add-button notes-send-all-button" aria-label={sendAllToComposerLabel(count)} title={sendAllToComposerLabel(count)} onClick={sendAll}>Send all</button>}
      <NoteQuickAdd group={agent} label={agent} />
    </header>
    {!collapsed && <NotesList groups={[{ group: agent, label: agent }]} agents={agents} focusRequest={expandedBy} returnTo={agent} onEditingChange={setEditing} onHandOff={appendToComposer} />}
  </section>
}
