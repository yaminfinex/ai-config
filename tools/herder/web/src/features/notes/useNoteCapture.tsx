import { useCallback, useEffect, useRef, useState, type PointerEvent as ReactPointerEvent } from 'react'
import type { NoteSource } from './notesStore'
import { captureNoteText, capturePosition, captureSourceWithRange } from './noteCaptureModel'
import { useNotes } from './NotesProvider'
import { noteCaptureShortcutEvent, type NoteCaptureShortcutDetail } from '../../shared/noteCaptureEvent'

type Capture = { quote: string, source: NoteSource, left: number, top: number, group: string }

function selectionElement(node: Node | null) {
  if (!node) return null
  return node.nodeType === Node.ELEMENT_NODE ? node as Element : node.parentElement
}

function belongsTo(container: HTMLElement, node: Node | null) {
  if (!node) return false
  if (container.contains(node)) return true
  const root = node.getRootNode()
  return root instanceof ShadowRoot && container.contains(root.host)
}

function selectedLine(node: Node | null) {
  const raw = selectionElement(node)?.closest<HTMLElement>('[data-line]')?.dataset.line
  if (!raw || !/^\d+$/.test(raw)) return undefined
  return Number(raw)
}

export function useNoteCapture({ active, source, agents }: { active: boolean, source: NoteSource, agents: string[] }) {
  const { store, captureGroup, setCaptureGroup, announce } = useNotes()
  const containerRef = useRef<HTMLElement>(null)
  const chipRef = useRef<HTMLButtonElement>(null)
  const [capture, setCapture] = useState<Capture | null>(null)
  const [comment, setComment] = useState('')
  const [expanded, setExpanded] = useState(false)

  const close = useCallback(() => { setCapture(null); setComment(''); setExpanded(false) }, [])
  const show = useCallback(() => {
    if (!active || !containerRef.current) return false
    if (capture && comment) return false
    const selection = document.getSelection()
    if (!selection || selection.isCollapsed || selection.rangeCount === 0 || !belongsTo(containerRef.current, selection.anchorNode) || !belongsTo(containerRef.current, selection.focusNode)) return false
    const quote = selection.toString().trim()
    if (!quote) return false
    const range = selection.getRangeAt(0)
    const rect = range.getBoundingClientRect()
    const position = capturePosition(rect, window.innerWidth, window.innerHeight)
    const provenSource = captureSourceWithRange(source, selectedLine(range.startContainer), range.endOffset === 0 ? undefined : selectedLine(range.endContainer))
    setCapture({ quote, source: provenSource, ...position, group: source.kind === 'transcript' ? source.agent : captureGroup })
    setComment('')
    setExpanded(false)
    window.requestAnimationFrame(() => chipRef.current?.focus())
    return true
  }, [active, capture, captureGroup, comment, source])

  useEffect(() => {
    if (!active) close()
  }, [active, close])
  useEffect(() => {
    if (!active) return
    const shortcut = (event: Event) => {
      if (show()) (event as CustomEvent<NoteCaptureShortcutDetail>).detail.claimed = true
    }
    window.addEventListener(noteCaptureShortcutEvent, shortcut)
    return () => window.removeEventListener(noteCaptureShortcutEvent, shortcut)
  }, [active, show])
  useEffect(() => {
    if (!capture) return
    const pointer = (event: PointerEvent) => {
      if (!comment && !(event.target as Element).closest('.note-capture-popover')) close()
    }
    window.addEventListener('pointerdown', pointer)
    return () => window.removeEventListener('pointerdown', pointer)
  }, [capture, close, comment])

  const save = () => {
    if (!capture) return
    const result = store.add({ group: capture.group, text: captureNoteText(capture.quote, comment), source: capture.source })
    if (!result.ok) { announce(result.reason); return }
    announce(`Saved a note in ${capture.group}.`)
    close()
  }
  const onPointerUp = (event: ReactPointerEvent<HTMLElement>) => {
    const target = event.nativeEvent.composedPath().find((item): item is Element => item instanceof Element)
    if (target?.closest('button, input, textarea, select, a, .note-capture-popover')) return
    window.setTimeout(show, 0)
  }
  const groups = [...new Set(['general', ...agents, capture?.group ?? captureGroup])]
  const element = capture ? <aside className={`note-capture-popover${expanded ? ' expanded' : ''}`} role="dialog" aria-label="Capture selected text" style={{ left: capture.left, top: capture.top }}
    onKeyDown={(event) => {
      if (event.key === 'Escape') { close(); event.preventDefault(); return }
      if (!expanded && event.key.length === 1 && !event.metaKey && !event.ctrlKey && !event.altKey) {
        setExpanded(true); setComment(event.key); event.preventDefault()
      }
    }}>
    <div className="note-capture-chip"><button ref={chipRef} type="button" onClick={save} aria-label={`Capture quote in ${capture.group}`}>+ → {capture.group}</button>
      <select aria-label="Note destination" value={capture.group} onChange={(event) => {
        const group = event.target.value
        setCapture((current) => current ? { ...current, group } : current)
        setCaptureGroup(group)
      }}>{groups.map((group) => <option value={group} key={group}>{group}</option>)}</select></div>
    {expanded && <textarea autoFocus aria-label="Comment on selected text" value={comment} onChange={(event) => setComment(event.target.value)} placeholder="Add a comment…"
      onKeyDown={(event) => {
        if (event.key === 'Escape') { close(); event.preventDefault(); return }
        if (event.key === 'Enter' && !event.shiftKey && !event.nativeEvent.isComposing) { save(); event.preventDefault() }
      }} />}
  </aside> : null
  return { containerRef, onPointerUp, element, show }
}
