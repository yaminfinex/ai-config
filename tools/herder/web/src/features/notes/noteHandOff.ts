import type { Note, NotesResult, NotesStatus } from './notesStore'

export type NoteHandOffGuard = {
  pending: (notes: Note[]) => Note[]
  markAppended: (notes: Note[]) => void
}

export function createNoteHandOffGuard(): NoteHandOffGuard {
  const appended = new Set<string>()
  return {
    pending: (notes) => notes.filter((note) => !appended.has(note.id)),
    markAppended: (notes) => { for (const note of notes) appended.add(note.id) },
  }
}

export function handOffSelectedNotes({ target, notes, guard, append, remove, flush, status }: {
  target: string
  notes: Note[]
  guard: NoteHandOffGuard
  append: (target: string, notes: Note[]) => { ok: true } | { ok: false, reason: string }
  remove: (ids: string[]) => NotesResult<number>
  flush: () => boolean
  status: () => NotesStatus
}): NotesResult<number> {
  const pending = guard.pending(notes)
  if (pending.length) {
    const appended = append(target, pending)
    if (!appended.ok) return appended
    guard.markAppended(pending)
  }

  const removed = remove(notes.map((note) => note.id))
  if (!removed.ok) {
    const prefix = pending.length ? 'The draft was updated, but the notes could not be removed.' : 'The draft already contains these notes; retry did not append them again.'
    return { ok: false, reason: `${prefix} ${removed.reason}` }
  }
  flush()
  const currentStatus = status()
  if (!currentStatus.persistent) {
    const prefix = pending.length ? 'The draft was updated' : 'The draft already contains these notes; retry did not append them again'
    return { ok: false, reason: `${prefix}, but the notes could not be removed durably. ${currentStatus.problem}` }
  }
  return removed
}
