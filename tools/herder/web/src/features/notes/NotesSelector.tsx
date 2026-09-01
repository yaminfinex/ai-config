import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import type { Note } from './notesStore.ts'
import { filterSelectorRows, selectorBackspace, selectorInitialValue, selectorMove, selectorRows, type NotesSelectorMode } from './notesSelectorModel.ts'

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
  const effectiveHighlight = filtered.some((row) => row.value === highlighted) ? highlighted : filtered[0]?.value ?? ''
  const inputRef = useRef<HTMLInputElement>(null)
  const optionRefs = useRef(new Map<string, HTMLButtonElement>())

  useEffect(() => {
    if (effectiveHighlight !== highlighted) setHighlighted(effectiveHighlight)
  }, [effectiveHighlight, highlighted])
  useLayoutEffect(() => { inputRef.current?.focus() }, [])
  useLayoutEffect(() => {
    optionRefs.current.get(effectiveHighlight)?.scrollIntoView({ block: 'nearest' })
  }, [effectiveHighlight, query])

  const move = (direction: -1 | 1) => {
    setHighlighted(selectorMove(filtered, effectiveHighlight, direction))
  }

  return <div className="notes-selector" role="dialog" aria-label={mode === 'destination' ? 'Choose composer destination' : 'Assign notes'}
    onKeyDown={(event) => {
      const focusedOption = (event.target as Element).closest('[role="option"]')
      if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); onCancel(); return }
      if (event.key === 'ArrowDown' || event.key === 'ArrowUp') { event.preventDefault(); move(event.key === 'ArrowDown' ? 1 : -1); return }
      if (event.key === 'Backspace') {
        const result = selectorBackspace(query, effectiveHighlight, mode)
        if (result.handled) { event.preventDefault(); setHighlighted(result.highlighted); return }
      }
      if (event.key === 'Enter' && effectiveHighlight && !focusedOption) { event.preventDefault(); onChoose(effectiveHighlight) }
    }}>
    <input ref={inputRef} autoFocus aria-label="Filter agents" placeholder={mode === 'destination' ? 'send to…' : 'assign to…'} value={query} onChange={(event) => {
      const next = event.target.value
      setQuery(next)
      const match = filterSelectorRows(rows, next)[0]
      if (match) setHighlighted(match.value)
    }} />
    <div role="listbox" aria-label="Agents">
      {filtered.map((row) => <button ref={(node) => { if (node) optionRefs.current.set(row.value, node); else optionRefs.current.delete(row.value) }} type="button" role="option" aria-selected={row.value === effectiveHighlight} className={row.value === effectiveHighlight ? 'highlighted' : ''}
        key={row.value} onPointerMove={() => setHighlighted(row.value)} onClick={() => onChoose(row.value)}>
        <span>{row.label}</span>{row.value === 'general' && <kbd>⌫</kbd>}
      </button>)}
    </div>
  </div>
}
