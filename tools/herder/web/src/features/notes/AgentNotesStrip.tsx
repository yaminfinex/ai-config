import { useState } from 'react'
import { appendComposerDraft, composerFieldId } from '../../composerState'
import { NoteQuickAdd } from './NoteQuickAdd.tsx'
import { NotesList } from './NotesList.tsx'
import { noteTransferText } from './notesPresentation.ts'
import { useNotes } from './NotesProvider.tsx'
import { useScheduledFrame } from '../../shared/lifecycle.ts'

export function AgentNotesStrip({ agent, agents }: { agent: string, agents: string[] }) {
  const { notes } = useNotes()
  const scheduleFrame = useScheduledFrame()
  const [collapsed, setCollapsed] = useState(true)
  const count = notes.filter((note) => note.group === agent).length
  if (count === 0) return null
  return <section className="agent-notes-strip" aria-label={`${agent} notes`}>
    <header className="agent-notes-header">
      <button type="button" className="agent-notes-toggle" aria-expanded={!collapsed} onClick={() => setCollapsed((current) => !current)}><span aria-hidden="true">{collapsed ? '▸' : '▾'}</span> Notes <span>{count}</span></button>
      <NoteQuickAdd group={agent} label={agent} />
    </header>
    {!collapsed && <NotesList groups={[{ group: agent, label: agent }]} agents={agents} onHandOff={(target, notes) => {
      const result = appendComposerDraft(target, notes.map((note) => noteTransferText(note)))
      if (result.ok) scheduleFrame(() => document.getElementById(composerFieldId(target))?.focus())
      return result.ok ? { ok: true } : result
    }} />}
  </section>
}
