import { useState } from 'react'
import { appendComposerDraft } from '../../composerState'
import { NotesGroup } from './NotesGroup'
import { noteTransferText, persistNotesStripCollapsed, readNotesStripCollapsed } from './notesPresentation'

export function AgentNotesStrip({ agent, agents }: { agent: string, agents: string[] }) {
  const [collapsed, setCollapsed] = useState(() => readNotesStripCollapsed(agent))
  const toggle = () => {
    const next = !collapsed
    setCollapsed(next)
    persistNotesStripCollapsed(agent, next)
  }
  return <section className="agent-notes-strip" aria-label={`${agent} notes`}>
    <button type="button" className="agent-notes-toggle" aria-expanded={!collapsed} onClick={toggle}><span aria-hidden="true">{collapsed ? '▸' : '▾'}</span> Notes</button>
    <NotesGroup group={agent} agents={agents} quickInput collapsed={collapsed} onHandOff={(target, notes) => {
      const result = appendComposerDraft(target, notes.map((note) => noteTransferText(note)))
      return result.ok ? { ok: true } : result
    }} />
  </section>
}
