import { useRef, useState } from 'react'
import { useNotes } from './NotesProvider.tsx'
import { useScheduledFrame } from '../../shared/lifecycle.ts'

export function NoteQuickAdd({ group, label }: { group: string, label: string }) {
  const { store, announce } = useNotes()
  const [open, setOpen] = useState(false)
  const [problem, setProblem] = useState('')
  const inputRef = useRef<HTMLInputElement>(null)
  const scheduleFrame = useScheduledFrame()
  const reveal = () => {
    setOpen(true)
    scheduleFrame(() => inputRef.current?.focus())
  }
  const save = () => {
    const input = inputRef.current
    if (!input) return
    const result = store.add({ group, text: input.value })
    if (!result.ok) { setProblem(result.reason); return }
    announce(`Saved a note in ${label}.`)
    setProblem('')
    setOpen(false)
  }
  return <span className="note-quick-add">
    <button type="button" className="note-add-button" aria-label={`Add note to ${label}`} aria-expanded={open} onClick={reveal}>+</button>
    {open && <span className="note-quick-add-popover">
      <input ref={inputRef} aria-label={`New note for ${label}`} placeholder="Add a note…" onBlur={(event) => { if (!event.currentTarget.value.trim()) setOpen(false) }} onKeyDown={(event) => {
        if (event.key === 'Escape') { setOpen(false); event.preventDefault(); return }
        if (event.key === 'Enter' && !event.nativeEvent.isComposing) { save(); event.preventDefault() }
      }} />
      {problem && <span className="notes-problem" role="alert">{problem}</span>}
    </span>}
  </span>
}
