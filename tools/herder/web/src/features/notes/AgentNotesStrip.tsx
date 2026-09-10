import { useEffect, useState } from 'react'
import { appendComposerDraft, composerFieldId } from '../../composerState'
import { NoteQuickAdd } from './NoteQuickAdd.tsx'
import { NotesList } from './NotesList.tsx'
import { noteTransferText } from './notesPresentation.ts'
import { useGroupNotes } from './NotesProvider.tsx'
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
  const count = notes.length
  if (count === 0 && !editing) return null
  return <section className="agent-notes-strip" aria-label={`${agent} notes`}>
    <header className="agent-notes-header">
      <button type="button" className="agent-notes-toggle" aria-expanded={!collapsed} onClick={() => setCollapsed((current) => !current)}><span aria-hidden="true">{collapsed ? '▸' : '▾'}</span> Notes <span>{count}</span></button>
      <NoteQuickAdd group={agent} label={agent} />
    </header>
    {!collapsed && <NotesList groups={[{ group: agent, label: agent }]} agents={agents} focusRequest={expandedBy} returnTo={agent} onEditingChange={setEditing} onHandOff={(target, notes) => {
      const result = appendComposerDraft(target, notes.map((note) => noteTransferText(note)))
      if (result.ok) scheduleFrame(() => document.getElementById(composerFieldId(target))?.focus())
      return result.ok ? { ok: true } : result
    }} />}
  </section>
}
