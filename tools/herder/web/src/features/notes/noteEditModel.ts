import type { Note } from './notesStore.ts'

export type NoteEditDraft = {
  original: Note
  text: string
}

export function beginNoteEdit(note: Note): NoteEditDraft {
  return { original: note, text: note.text }
}

export function updateNoteEdit(draft: NoteEditDraft, text: string): NoteEditDraft {
  return { ...draft, text }
}

export function noteEditDisplay(draft: NoteEditDraft, current: Note | undefined): Note {
  return current ?? draft.original
}
