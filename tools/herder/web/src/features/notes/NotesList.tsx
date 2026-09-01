import { useEffect, useMemo, useRef, useState } from 'react'
import { copyPath } from '../../shared/pathCopyModel.ts'
import { copyWithHiddenTextarea } from '../../shared/PathCopyButton.tsx'
import { handOffSelectedNotes } from './noteHandOff.ts'
import {
  dragNoteIDs,
  handOffRoute,
  noteListAction,
  pruneNoteSelection,
  selectionAfterArrow,
  selectionAfterClick,
  type NoteSelection,
} from './notesListModel.ts'
import { noteSourceLabel, noteTransferText } from './notesPresentation.ts'
import type { Note } from './notesStore.ts'
import { useNotes } from './NotesProvider.tsx'
import { NotesSelector } from './NotesSelector.tsx'
import { selectorRows } from './notesSelectorModel.ts'
import { useScheduledFrame } from '../../shared/lifecycle.ts'

export type NotesListGroup = { group: string, label: string, orphaned?: boolean }
export type NotesHandOff = (target: string, notes: Note[]) => { ok: true } | { ok: false, reason: string }

const emptySelection = (): NoteSelection => ({ selected: new Set(), anchor: undefined, cursor: undefined })

export function NotesList({ groups, agents, onHandOff }: { groups: NotesListGroup[], agents: string[], onHandOff: NotesHandOff }) {
  const { store, notes: allNotes, announce, handOffGuard } = useNotes()
  const listRef = useRef<HTMLDivElement>(null)
  const scheduleFrame = useScheduledFrame()
  const notesByGroup = useMemo(() => new Map(groups.map(({ group }) => [group, allNotes.filter((note) => note.group === group)])), [allNotes, groups])
  const notes = useMemo(() => groups.flatMap(({ group }) => notesByGroup.get(group) ?? []), [groups, notesByGroup])
  const ids = useMemo(() => notes.map((note) => note.id), [notes])
  const [selection, setSelection] = useState<NoteSelection>(emptySelection)
  const [editing, setEditing] = useState<string>()
  const [problem, setProblem] = useState('')
  const [selector, setSelector] = useState<{ mode: 'assign' | 'destination', initial?: string, query?: string }>()
  const selectedNotes = notes.filter((note) => selection.selected.has(note.id))

  useEffect(() => { setSelection((current) => pruneNoteSelection(current, ids)) }, [ids])

  const focus = (id?: string) => {
    if (!id) return
    scheduleFrame(() => listRef.current?.querySelector<HTMLElement>(`[data-note-id="${CSS.escape(id)}"]`)?.focus())
  }
  const clear = () => { setSelection(emptySelection()); setEditing(undefined); setSelector(undefined) }
  const edit = (id?: string) => { if (id) setEditing(id) }
  const move = (destination: string, moving = selectedNotes) => {
    const changed = moving.filter((note) => note.group !== destination)
    if (changed.length === 0) { setSelector(undefined); return }
    const failures = changed.flatMap((note) => {
      const result = store.edit(note.id, { group: destination })
      return result.ok ? [] : [result.reason]
    })
    if (failures.length) setProblem(`${failures.length} ${failures.length === 1 ? 'note was' : 'notes were'} not moved: ${failures[0]}`)
    else { setProblem(''); announce(`Moved ${changed.length} ${changed.length === 1 ? 'note' : 'notes'} to ${destination === 'general' ? 'unassigned' : destination}.`) }
    setSelector(undefined)
  }
  const handOff = (target: string) => {
    const pending = selectedNotes
    const result = handOffSelectedNotes({ target, notes: pending, guard: handOffGuard, append: onHandOff, remove: store.delete, flush: store.flush, status: store.status })
    if (!result.ok) { setProblem(result.reason); return }
    announce(`Moved ${pending.length} ${pending.length === 1 ? 'note' : 'notes'} to ${target}’s composer.`)
    clear()
    setProblem('')
  }
  const beginHandOff = () => {
    const destinationOrder = selectorRows(allNotes, agents, 'destination').map((row) => row.value)
    const route = handOffRoute(selectedNotes, destinationOrder)
    if (route.kind === 'direct') handOff(route.target)
    else setSelector({ mode: 'destination', initial: route.initial })
  }
  const copy = async () => {
    const result = await copyPath(navigator.clipboard, selectedNotes.map(noteTransferText).join('\n\n'), copyWithHiddenTextarea)
    if (result === 'copied') { announce(`Copied ${selectedNotes.length} ${selectedNotes.length === 1 ? 'note' : 'notes'}.`); setProblem('') }
    else setProblem('These notes could not be copied. They were left unchanged.')
  }
  const remove = () => {
    const result = store.delete(selectedNotes.map((note) => note.id))
    if (!result.ok) { setProblem(result.reason); return }
    announce(`Deleted ${result.value} ${result.value === 1 ? 'note' : 'notes'}.`)
    clear()
  }
  const runAction = (action: ReturnType<typeof noteListAction>) => {
    if (!action || selection.selected.size === 0) return
    if (action === 'copy') void copy()
    else if (action === 'delete') remove()
    else if (action === 'hand-off') beginHandOff()
    else if (action === 'assign') setSelector({ mode: 'assign' })
    else if (action === 'edit') edit(selection.cursor ?? selectedNotes[0]?.id)
    else if (action === 'clear') clear()
  }

  return <div className="notes-list" ref={listRef} role="listbox" aria-multiselectable="true" tabIndex={-1} onKeyDown={(event) => {
    const target = event.target as Element
    const editable = Boolean(target.closest('input, textarea, select, button, a[href], [contenteditable="true"], .notes-selector'))
    if (!editable && (event.key === 'ArrowDown' || event.key === 'ArrowUp')) {
      const next = selectionAfterArrow(selection, ids, event.key === 'ArrowDown' ? 1 : -1, event.shiftKey)
      setSelection(next); focus(next.cursor); event.preventDefault(); return
    }
    const action = noteListAction(event, editable)
    if (action) { runAction(action); event.preventDefault() }
  }}>
    {groups.map(({ group, label, orphaned }) => {
      const groupNotes = notesByGroup.get(group) ?? []
      if (groupNotes.length === 0) return null
      return <section className="notes-list-group" key={group} aria-label={`${label} notes`}>
        <header className="notes-group-heading" data-note-drop-group={group} onDragOver={(event) => event.preventDefault()} onDrop={(event) => {
          event.preventDefault()
          try {
            const moving = JSON.parse(event.dataTransfer.getData('application/x-herder-notes')) as string[]
            move(group, notes.filter((note) => moving.includes(note.id)))
          } catch { /* ignore foreign drags */ }
        }}><strong>{label}</strong><span>{groupNotes.length}</span>{orphaned && <button type="button" onClick={() => {
          setSelection({ selected: new Set(groupNotes.map((note) => note.id)), anchor: groupNotes[0]?.id, cursor: groupNotes[0]?.id })
          setSelector({ mode: 'assign' })
        }}>not on the live roster</button>}</header>
        {groupNotes.map((note) => {
          const selected = selection.selected.has(note.id)
          return <article className={`note-card${note.source ? ' anchored' : ''}${selected ? ' selected' : ''}`} role="option" aria-selected={selected}
            data-note-id={note.id} draggable={groups.length > 1 && editing !== note.id} tabIndex={selection.cursor === note.id || !selection.cursor && note.id === ids[0] ? 0 : -1} key={note.id}
            onDragStart={(event) => event.dataTransfer.setData('application/x-herder-notes', JSON.stringify(dragNoteIDs(note.id, selection.selected)))}
            onClick={(event) => {
              if ((event.target as Element).closest('textarea, input, button')) return
              const next = selectionAfterClick(selection, ids, note.id, { command: event.metaKey || event.ctrlKey, shift: event.shiftKey })
              setSelection(next)
            }} onDoubleClick={() => edit(note.id)}>
            <div className="note-card-content">
              {note.source && <small>{noteSourceLabel(note.source)}</small>}
              {note.quote && <p className="note-card-quote">{note.quote}</p>}
              {editing === note.id ? <textarea autoFocus defaultValue={note.text} aria-label="Edit note comment" onBlur={(event) => {
                const result = store.edit(note.id, { text: event.currentTarget.value })
                if (!result.ok) setProblem(result.reason)
                else { setProblem(''); setEditing(undefined) }
              }} onKeyDown={(event) => {
                event.stopPropagation()
                if (event.key === 'Escape') { setEditing(undefined); event.preventDefault(); return }
              }} /> : <p>{note.text}</p>}
            </div>
          </article>
        })}
      </section>
    })}
    {problem && <p className="notes-problem" role="alert">{problem}</p>}
    {selection.selected.size > 0 && <div className="notes-hint-line" aria-label={`${selection.selected.size} selected`}>
      <strong>{selection.selected.size}</strong>
      <button type="button" onClick={() => void copy()}><kbd>⌘C</kbd> copy</button>
      <button type="button" onClick={beginHandOff}><kbd>⏎</kbd> composer</button>
      <button type="button" onClick={() => setSelector({ mode: 'assign' })}><kbd>A</kbd> assign</button>
      <button type="button" onClick={remove}><kbd>⌫</kbd> delete</button>
      <button type="button" aria-label="Clear selection" onClick={clear}><kbd>esc</kbd></button>
    </div>}
    {selector && <NotesSelector notes={allNotes} selected={selectedNotes} agents={agents} mode={selector.mode} initialValue={selector.initial} initialQuery={selector.query}
      onCancel={() => setSelector(undefined)} onChoose={(value) => selector.mode === 'assign' ? move(value) : handOff(value)} />}
  </div>
}
