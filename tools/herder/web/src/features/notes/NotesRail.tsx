import { appendComposerDraft } from '../../composerState'
import type { Board } from '../../types'
import type { OpenPlacement } from '../layout/openPlacement'
import { handOffSelectedNotes } from './noteHandOff.ts'
import { NotesList } from './NotesList.tsx'
import { sendAllPlan } from './notesListModel.ts'
import { useAllNotes, useNotes, useNotesStatus } from './NotesProvider'
import { liveRosterNames, noteGroupRows, noteTransferText } from './notesPresentation'

export function NotesRail({ board, onOpenAgent }: { board: Board | undefined, onOpenAgent: (name: string, placement?: OpenPlacement) => void }) {
  const notes = useAllNotes()
  const { store, announce, handOffGuard } = useNotes()
  const status = useNotesStatus()
  const agents = liveRosterNames(board)
  const groups = noteGroupRows(notes, agents)
  const handOff = (target: string, selected: typeof notes) => {
    const result = appendComposerDraft(target, selected.map(noteTransferText))
    if (!result.ok) return result
    onOpenAgent(target, { direction: 'right' })
    return { ok: true as const }
  }
  const plural = (count: number, word: string) => `${count} ${word}${count === 1 ? '' : 's'}`
  const plan = sendAllPlan(groups, new Map(groups.map(({ group }) => [group, notes.filter((note) => note.group === group)])), agents)
  // Send all runs the list's own hand-off (same function, guard, append) once per live group.
  const sendAll = () => {
    let moved = 0
    let prompts = 0
    for (const { group, notes: pending } of plan.handOffs) {
      const result = handOffSelectedNotes({ target: group, notes: pending, guard: handOffGuard, append: handOff, remove: store.delete, flush: store.flush, status: store.status })
      if (!result.ok) { announce(`${moved ? `Moved ${plural(moved, 'note')} into ${plural(prompts, 'prompt')}, then ` : ''}${group}: ${result.reason}`); return }
      moved += pending.length
      prompts += 1
    }
    announce(`Moved ${plural(moved, 'note')} into ${plural(prompts, 'prompt')}.${plan.skipped ? ` ${plural(plan.skipped, 'note')} without a live agent ${plan.skipped === 1 ? 'stays' : 'stay'}.` : ''}`)
  }
  const sendAllTitle = plan.handOffs.length ? 'Send all notes to their agents' : 'No note is assigned to a live agent'
  return <div className="notes-rail-view">
    {status.problem && <p className={`notes-storage-state${status.persistent ? '' : ' unavailable'}`} role={status.persistent ? 'status' : 'alert'}>{status.problem}</p>}
    {notes.length > 0 && <div className="notes-send-all"><button type="button" className="note-add-button notes-send-all-button" aria-label="Send all notes to their agents" title={sendAllTitle} disabled={plan.handOffs.length === 0} onClick={sendAll}>Send all</button></div>}
    <NotesList groups={groups.map(({ group, orphaned }) => ({ group, orphaned, label: group === 'general' ? 'unassigned' : group }))} agents={agents} onHandOff={handOff} />
  </div>
}
