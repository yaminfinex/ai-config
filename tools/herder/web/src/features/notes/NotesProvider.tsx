import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { createNotesStore, type Note, type NotesStatus, type NotesStore } from './notesStore'

type NotesContextValue = {
  store: NotesStore
  notes: Note[]
  status: NotesStatus
  captureGroup: string
  setCaptureGroup: (group: string) => void
  announce: (message: string) => void
}

const NotesContext = createContext<NotesContextValue | null>(null)

export function NotesProvider({ children }: { children: ReactNode }) {
  const [store] = useState(createNotesStore)
  const [notes, setNotes] = useState(() => store.list())
  const [status, setStatus] = useState(() => store.status())
  const [captureGroup, setCaptureGroup] = useState('general')
  const [toast, setToast] = useState('')
  const toastTimer = useRef<number | undefined>(undefined)
  const disposeTimer = useRef<number | undefined>(undefined)

  useEffect(() => {
    if (disposeTimer.current !== undefined) window.clearTimeout(disposeTimer.current)
    const refresh = () => { setNotes(store.list()); setStatus(store.status()) }
    const unsubscribe = store.subscribe(refresh)
    return () => {
      unsubscribe()
      disposeTimer.current = window.setTimeout(() => store.dispose(), 0)
    }
  }, [store])
  const announce = useCallback((message: string) => {
    if (toastTimer.current !== undefined) window.clearTimeout(toastTimer.current)
    setToast(message)
    toastTimer.current = window.setTimeout(() => { setToast(''); toastTimer.current = undefined }, 1_800)
  }, [])
  useEffect(() => () => { if (toastTimer.current !== undefined) window.clearTimeout(toastTimer.current) }, [])
  const value = useMemo(() => ({ store, notes, status, captureGroup, setCaptureGroup, announce }), [announce, captureGroup, notes, status, store])

  return <NotesContext.Provider value={value}>{children}{toast && <div className="notes-toast" role="status" aria-live="polite">{toast}</div>}</NotesContext.Provider>
}

export function useNotes() {
  const value = useContext(NotesContext)
  if (!value) throw new Error('notes context is unavailable')
  return value
}
