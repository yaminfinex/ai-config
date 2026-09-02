import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, useSyncExternalStore, type ReactNode } from 'react'
import { createNotesStore, type Note, type NotesStatus, type NotesStore } from './notesStore'
import { createNoteHandOffGuard, type NoteHandOffGuard } from './noteHandOff'
import { allNotesSignal, groupNotesSignal, notesCountSignal, notesGroupsSignal, notesStatusSignal } from './notesSubscription'

type NotesContextValue = {
  store: NotesStore
  announce: (message: string) => void
  handOffGuard: NoteHandOffGuard
}

const NotesContext = createContext<NotesContextValue | null>(null)

export function NotesProvider({ children }: { children: ReactNode }) {
  const [store] = useState(createNotesStore)
  const [handOffGuard] = useState(createNoteHandOffGuard)
  const [toast, setToast] = useState('')
  const toastTimer = useRef<number | undefined>(undefined)
  const disposeTimer = useRef<number | undefined>(undefined)

  useEffect(() => {
    if (disposeTimer.current !== undefined) window.clearTimeout(disposeTimer.current)
    return () => {
      disposeTimer.current = window.setTimeout(() => store.dispose(), 0)
    }
  }, [store])
  const announce = useCallback((message: string) => {
    if (toastTimer.current !== undefined) window.clearTimeout(toastTimer.current)
    setToast(message)
    toastTimer.current = window.setTimeout(() => { setToast(''); toastTimer.current = undefined }, 1_800)
  }, [])
  useEffect(() => () => { if (toastTimer.current !== undefined) window.clearTimeout(toastTimer.current) }, [])
  const value = useMemo(() => ({ store, announce, handOffGuard }), [announce, handOffGuard, store])

  return <NotesContext.Provider value={value}>{children}{toast && <div className="notes-toast" role="status" aria-live="polite">{toast}</div>}</NotesContext.Provider>
}

export function useNotes() {
  const value = useContext(NotesContext)
  if (!value) throw new Error('notes context is unavailable')
  return value
}

export function useAllNotes(): Note[] {
  const { store } = useNotes()
  const signal = useMemo(() => allNotesSignal(store), [store])
  return useSyncExternalStore(signal.subscribe, signal.getSnapshot, signal.getSnapshot)
}

export function useGroupNotes(group: string): Note[] {
  const { store } = useNotes()
  const signal = useMemo(() => groupNotesSignal(store, group), [group, store])
  return useSyncExternalStore(signal.subscribe, signal.getSnapshot, signal.getSnapshot)
}

export function useNotesGroups(groups: string[]): Note[] {
  const { store } = useNotes()
  const groupKey = JSON.stringify(groups)
  const signal = useMemo(() => notesGroupsSignal(store, JSON.parse(groupKey) as string[]), [groupKey, store])
  return useSyncExternalStore(signal.subscribe, signal.getSnapshot, signal.getSnapshot)
}

export function useNotesStatus(): NotesStatus {
  const { store } = useNotes()
  const signal = useMemo(() => notesStatusSignal(store), [store])
  return useSyncExternalStore(signal.subscribe, signal.getSnapshot, signal.getSnapshot)
}

export function useNotesCount(store: NotesStore): number {
  const signal = useMemo(() => notesCountSignal(store), [store])
  return useSyncExternalStore(signal.subscribe, signal.getSnapshot, signal.getSnapshot)
}
