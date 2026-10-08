import { useMemo, useSyncExternalStore } from 'react'
import { composerDraftPrefix, readComposerDrafts, subscribeComposerDrafts } from '../../composerState'
import { useNotesStoreIfAny } from '../notes/NotesProvider'
import type { NotesStore } from '../notes/notesStore'
import { createNotesSelectorSignal, type NotesSelectorSignal } from '../notes/notesSubscription'
import { draftMarks, sameDraftMarks, type DraftMarks } from './draftMarksModel.ts'

// One signal per notes store, shared by every mark: a keystroke updates the
// one draft it wrote, and only a change in who has drafts re-renders. Storage
// is scanned for drafts only on subscribe and on another tab's draft write,
// never per keystroke: the scan walks every localStorage key.
const signals = new WeakMap<object, NotesSelectorSignal<DraftMarks>>()
const storeless = {}

function draftMarksSignal(store: NotesStore | null): NotesSelectorSignal<DraftMarks> {
  const key = store ?? storeless
  const existing = signals.get(key)
  if (existing) return existing
  let drafts: Record<string, string> | undefined
  const signal = createNotesSelectorSignal((listener) => {
    drafts = undefined
    const unsubscribeNotes = store?.subscribe(listener)
    const unsubscribeComposer = subscribeComposerDrafts(({ agent, text }) => {
      if (drafts) drafts[agent] = text
      listener()
    })
    // Another tab's composer drafts arrive only as storage events; another
    // tab's notes already reach the store through its own storage listener.
    const onStorage = (event: StorageEvent) => {
      if (event.key !== null && !event.key.startsWith(composerDraftPrefix)) return
      drafts = undefined
      listener()
    }
    window.addEventListener('storage', onStorage)
    return () => {
      unsubscribeNotes?.()
      unsubscribeComposer()
      window.removeEventListener('storage', onStorage)
      drafts = undefined
    }
  }, () => draftMarks(drafts ??= readComposerDrafts(), store?.list() ?? []), sameDraftMarks)
  signals.set(key, signal)
  return signal
}

// useDraftMarks is every agent with an unsent composer draft or notes.
export function useDraftMarks(): DraftMarks {
  const store = useNotesStoreIfAny()
  const signal = useMemo(() => draftMarksSignal(store), [store])
  return useSyncExternalStore(signal.subscribe, signal.getSnapshot, signal.getSnapshot)
}
