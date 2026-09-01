import { useEffect, useMemo, useState } from 'react'
import type { Note } from './notesStore.ts'
import { filterSelectorRows, selectorBackspace, selectorInitialValue, selectorRows, type NotesSelectorMode } from './notesSelectorModel.ts'

export function NotesSelector({ notes, selected, agents, mode = 'assign', initialValue, initialQuery = '', onChoose, onCancel }: {
  notes: Note[]
  selected: Note[]
  agents: string[]
  mode?: NotesSelectorMode
  initialValue?: string
  initialQuery?: string
  onChoose: (value: string) => void
  onCancel: () => void
}) {
  const rows = useMemo(() => selectorRows(notes, agents, mode), [agents, mode, notes])
  const initial = initialValue && rows.some((row) => row.value === initialValue) ? initialValue : selectorInitialValue(selected, rows, mode)
  const [query, setQuery] = useState(initialQuery)
  const [highlighted, setHighlighted] = useState(initial)
  const filtered = filterSelectorRows(rows, query)

  useEffect(() => {
    if (filtered.some((row) => row.value === highlighted)) return
    setHighlighted(filtered[0]?.value ?? '')
  }, [filtered, highlighted])

  const move = (direction: -1 | 1) => {
    if (filtered.length === 0) return
    const index = filtered.findIndex((row) => row.value === highlighted)
    const next = Math.max(0, Math.min(filtered.length - 1, (index < 0 ? 0 : index) + direction))
    setHighlighted(filtered[next]?.value ?? '')
  }

  return <div className="notes-selector" role="dialog" aria-label={mode === 'destination' ? 'Choose composer destination' : 'Assign notes'}
    onKeyDown={(event) => {
      const focusedOption = (event.target as Element).closest('[role="option"]')
      if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); onCancel(); return }
      if (event.key === 'ArrowDown' || event.key === 'ArrowUp') { event.preventDefault(); move(event.key === 'ArrowDown' ? 1 : -1); return }
      if (event.key === 'Backspace') {
        const result = selectorBackspace(query, highlighted, mode)
        if (result.handled) { event.preventDefault(); setHighlighted(result.highlighted); return }
      }
      if (event.key === 'Enter' && highlighted && !focusedOption) { event.preventDefault(); onChoose(highlighted) }
    }}>
    <input autoFocus aria-label="Filter agents" placeholder={mode === 'destination' ? 'send to…' : 'assign to…'} value={query} onChange={(event) => {
      const next = event.target.value
      setQuery(next)
      const match = filterSelectorRows(rows, next)[0]
      if (match) setHighlighted(match.value)
    }} />
    <div role="listbox" aria-label="Agents">
      {filtered.map((row) => <button type="button" role="option" aria-selected={row.value === highlighted} className={row.value === highlighted ? 'highlighted' : ''}
        key={row.value} onPointerMove={() => setHighlighted(row.value)} onClick={() => onChoose(row.value)}>
        <span>{row.label}</span>{row.value === 'general' && <kbd>⌫</kbd>}
      </button>)}
    </div>
  </div>
}
