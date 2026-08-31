import { useEffect, useMemo, useRef, useState } from 'react'
import type { Note } from './notesStore'
import { noteSourceLabel, noteTransferText, selectionAfterGesture, type SelectionGesture } from './notesPresentation'
import { useNotes } from './NotesProvider'
import { handOffSelectedNotes } from './noteHandOff'
import { copyPath } from '../../shared/pathCopyModel'
import { copyWithHiddenTextarea } from '../../shared/PathCopyButton'

export type NotesHandOff = (target: string, notes: Note[]) => { ok: true } | { ok: false, reason: string }

export function NotesGroup({ group, label = group, agents, orphaned = false, quickInput = false, collapsed = false, onHandOff }: {
  group: string
  label?: string
  agents: string[]
  orphaned?: boolean
  quickInput?: boolean
  collapsed?: boolean
  onHandOff: NotesHandOff
}) {
  const { store, notes: allNotes, announce, handOffGuard } = useNotes()
  const notes = useMemo(() => allNotes.filter((note) => note.group === group), [allNotes, group])
  const ids = useMemo(() => notes.map((note) => note.id), [notes])
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [anchor, setAnchor] = useState<string>()
  const [editing, setEditing] = useState<string>()
  const [problem, setProblem] = useState('')
  const [target, setTarget] = useState(group === 'general' || orphaned ? '' : group)
  const quickRef = useRef<HTMLInputElement>(null)
  const selectedNotes = notes.filter((note) => selected.has(note.id))

  useEffect(() => {
    const present = new Set(ids)
    setSelected((current) => {
      const next = new Set([...current].filter((id) => present.has(id)))
      return next.size === current.size ? current : next
    })
  }, [ids])
  useEffect(() => { if (group !== 'general' && !orphaned) setTarget(group) }, [group, orphaned])
  useEffect(() => {
    if (selected.size === 0) return
    const escape = (event: KeyboardEvent) => {
      if (event.key !== 'Escape') return
      setSelected(new Set())
      setAnchor(undefined)
      event.preventDefault()
    }
    window.addEventListener('keydown', escape)
    return () => window.removeEventListener('keydown', escape)
  }, [selected.size])

  const select = (id: string, gesture: SelectionGesture) => {
    const next = selectionAfterGesture(selected, ids, id, gesture, anchor)
    setSelected(next.selected)
    setAnchor(next.anchor)
  }
  const clearSelection = () => { setSelected(new Set()); setAnchor(undefined) }
  const saveQuick = () => {
    const input = quickRef.current
    if (!input) return
    const result = store.add({ group, text: input.value })
    if (!result.ok) { setProblem(result.reason); return }
    input.value = ''
    setProblem('')
    announce(`Saved a note in ${label}.`)
  }
  const saveEdit = (note: Note, value: string) => {
    const result = store.edit(note.id, { text: value })
    if (!result.ok) { setProblem(result.reason); return false }
    setEditing(undefined)
    setProblem('')
    return true
  }
  const move = (destination: string) => {
    if (!destination) return
    const failures = selectedNotes.flatMap((note) => {
      const result = store.edit(note.id, { group: destination })
      return result.ok ? [] : [result.reason]
    })
    if (failures.length) {
      const moved = selectedNotes.length - failures.length
      if (moved) announce(`Moved ${moved} of ${selectedNotes.length} notes to ${destination}.`)
      setProblem(`${failures.length} ${failures.length === 1 ? 'note was' : 'notes were'} not moved: ${failures[0]}`)
      clearSelection()
      return
    }
    announce(`Moved ${selectedNotes.length} ${selectedNotes.length === 1 ? 'note' : 'notes'} to ${destination}.`)
    clearSelection()
  }
  const handOff = () => {
    if (!target) { setProblem('Choose a live agent for this hand-off.'); return }
    const result = handOffSelectedNotes({ target, notes: selectedNotes, guard: handOffGuard, append: onHandOff, remove: store.delete, flush: store.flush, status: store.status })
    if (!result.ok) { setProblem(result.reason); return }
    announce(`Moved ${selectedNotes.length} ${selectedNotes.length === 1 ? 'note' : 'notes'} to ${target}’s composer.`)
    clearSelection()
    setProblem('')
  }
  const copy = async () => {
    // navigator.clipboard is undefined on insecure origins (the owner's tailnet
    // origin); copyPath falls back to the hidden-textarea legacy copy there.
    const state = await copyPath(navigator.clipboard, selectedNotes.map(noteTransferText).join('\n\n'), copyWithHiddenTextarea)
    if (state === 'copied') {
      announce(`Copied ${selectedNotes.length} ${selectedNotes.length === 1 ? 'note' : 'notes'}.`)
      setProblem('')
    } else {
      setProblem('These notes could not be copied. They were left unchanged.')
    }
  }
  const remove = () => {
    const result = store.delete(selectedNotes.map((note) => note.id))
    if (!result.ok) { setProblem(result.reason); return }
    announce(`Deleted ${result.value} ${result.value === 1 ? 'note' : 'notes'}.`)
    clearSelection()
  }

  return <section className={`notes-group${selected.size ? ' selecting' : ''}${collapsed ? ' collapsed' : ''}`} aria-label={`${label} notes`}>
    {!collapsed && <header className="notes-group-heading"><strong>{label}</strong><span>{notes.length}</span>{orphaned && <em>not on the live roster</em>}</header>}
    {quickInput && <div className="notes-quick-row"><input ref={quickRef} data-notes-quick-input={group === 'general' ? 'general' : undefined}
      aria-label={`Quick note for ${label}`} placeholder={`Add a note to ${label}…`} onKeyDown={(event) => {
        if (event.key === 'Enter' && !event.nativeEvent.isComposing) { event.preventDefault(); saveQuick() }
      }} /><button type="button" onClick={saveQuick}>Add</button></div>}
    {!collapsed && selected.size > 0 && <div className="notes-action-bar" role="toolbar" aria-label={`${selected.size} selected`} onKeyDown={(event) => {
      if (event.key === 'Escape') { clearSelection(); event.preventDefault() }
    }}>
      <strong>{selected.size} selected</strong>
      <button type="button" onClick={() => void copy()}>Copy</button>
      <label>Move <select value="" onChange={(event) => move(event.target.value)}><option value="">Choose…</option><option value="general">general</option>{agents.filter((agent) => agent !== group).map((agent) => <option value={agent} key={agent}>{agent}</option>)}</select></label>
      <button type="button" onClick={remove}>Delete</button>
      {(group === 'general' || orphaned) && <label>Agent <select value={target} onChange={(event) => setTarget(event.target.value)}><option value="">Choose…</option>{agents.map((agent) => <option value={agent} key={agent}>{agent}</option>)}</select></label>}
      <button type="button" onClick={handOff}>Hand to composer</button>
    </div>}
    {problem && <p className="notes-problem" role="alert">{problem}</p>}
    {!collapsed && <div className="notes-list" role="listbox" aria-multiselectable="true">
      {notes.map((note, index) => <article className={`note-row${selected.has(note.id) ? ' selected' : ''}`} role="option" aria-selected={selected.has(note.id)} tabIndex={index === 0 ? 0 : -1} key={note.id}
        onClick={(event) => {
          if ((event.target as Element).closest('button, input, textarea, select')) return
          if (event.metaKey || event.ctrlKey || event.shiftKey) select(note.id, event.shiftKey ? 'range' : 'toggle')
          else setEditing(note.id)
        }} onKeyDown={(event) => {
          if (event.key === 'Escape') { clearSelection(); setEditing(undefined); event.preventDefault(); return }
          if (event.key === ' ') { select(note.id, event.shiftKey ? 'range' : 'toggle'); event.preventDefault(); return }
          if (event.key !== 'ArrowDown' && event.key !== 'ArrowUp') return
          const rows = [...event.currentTarget.parentElement!.querySelectorAll<HTMLElement>('.note-row')]
          rows[(index + (event.key === 'ArrowDown' ? 1 : -1) + rows.length) % rows.length]?.focus()
          event.preventDefault()
        }}>
        <label className="note-select"><input type="checkbox" aria-label={`Select note ${index + 1}`} checked={selected.has(note.id)} onChange={(event) => select(note.id, event.nativeEvent instanceof MouseEvent && event.nativeEvent.shiftKey ? 'range' : 'toggle')} /></label>
        <div className="note-content">
          {note.source && <small>{noteSourceLabel(note.source)}</small>}
          {editing === note.id ? <textarea autoFocus defaultValue={note.text} aria-label="Edit note" onBlur={(event) => { saveEdit(note, event.currentTarget.value) }} onKeyDown={(event) => {
            if (event.key === 'Escape') { setEditing(undefined); event.preventDefault(); return }
            if (event.key !== 'Enter' || event.shiftKey || event.nativeEvent.isComposing) return
            event.preventDefault()
            saveEdit(note, event.currentTarget.value)
          }} /> : <p>{note.text}</p>}
        </div>
      </article>)}
      {notes.length === 0 && <p className="notes-empty">No notes in this group.</p>}
    </div>}
  </section>
}
