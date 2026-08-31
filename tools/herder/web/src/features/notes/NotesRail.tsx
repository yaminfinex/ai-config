import { appendComposerDraft } from '../../composerState'
import type { Board } from '../../types'
import type { OpenPlacement } from '../layout/openPlacement'
import { NotesGroup } from './NotesGroup'
import { useNotes } from './NotesProvider'
import { liveRosterNames, noteGroupRows, noteTransferText } from './notesPresentation'

export function NotesRail({ board, onOpenAgent }: { board: Board | undefined, onOpenAgent: (name: string, placement?: OpenPlacement) => void }) {
  const { notes, status } = useNotes()
  const agents = liveRosterNames(board)
  const groups = noteGroupRows(notes, agents)
  const handOff = (target: string, selected: typeof notes) => {
    const result = appendComposerDraft(target, selected.map(noteTransferText))
    if (!result.ok) return result
    onOpenAgent(target, { direction: 'right' })
    return { ok: true as const }
  }
  return <div className="notes-rail-view">
    <p className="notes-browser-label">Saved in this browser</p>
    {status.problem && <p className={`notes-storage-state${status.persistent ? '' : ' unavailable'}`} role={status.persistent ? 'status' : 'alert'}>{status.problem}</p>}
    {groups.map(({ group, orphaned }) => <NotesGroup key={group} group={group} label={group} agents={agents} orphaned={orphaned} quickInput={group === 'general'} onHandOff={handOff} />)}
  </div>
}
