import { useCallback, useEffect, useRef, useState, type PointerEvent as ReactPointerEvent } from 'react'
import { useDOMEvent, useScheduledFrame } from '../../shared/lifecycle.ts'
import { fileResolveGestureEvent, noteCaptureGestureEvent } from '../../shared/selectionPopoverEvents.ts'
import { captureNoteText, capturePosition, captureSourceWithRange } from './noteCaptureModel.ts'
import { NoteCaptureChip, type NoteCaptureDraft } from './NoteCaptureChip.tsx'
import type { NoteSource } from './notesStore.ts'
import { useNotes } from './NotesProvider.tsx'

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
  const { store, notes, announce } = useNotes()
  const containerRef = useRef<HTMLElement>(null)
  const pointer = useRef<{ x: number, y: number } | undefined>(undefined)
  const keyboardSelecting = useRef(false)
  const suppressUntil = useRef(0)
  const [capture, setCapture] = useState<NoteCaptureDraft | null>(null)
  const scheduleFrame = useScheduledFrame()

  const close = useCallback(() => setCapture(null), [])
  const show = useCallback(() => {
    if (!active || !containerRef.current || performance.now() < suppressUntil.current) return false
    const selection = document.getSelection()
    if (!selection || selection.isCollapsed || selection.rangeCount === 0 || !belongsTo(containerRef.current, selection.anchorNode) || !belongsTo(containerRef.current, selection.focusNode)) return false
    if (selectionElement(selection.anchorNode)?.closest('.note-capture-popover') || selectionElement(selection.focusNode)?.closest('.note-capture-popover')) return false
    const quote = selection.toString().trim()
    if (!quote) return false
    const range = selection.getRangeAt(0)
    const rect = range.getBoundingClientRect()
    const position = capturePosition(rect, window.innerWidth, window.innerHeight)
    const provenSource = captureSourceWithRange(source, selectedLine(range.startContainer), range.endOffset === 0 ? undefined : selectedLine(range.endContainer))
    setCapture({ quote, source: provenSource, ...position, group: source.kind === 'transcript' ? source.agent : 'general' })
    return true
  }, [active, source])

  useEffect(() => { if (!active) close() }, [active, close])
  useDOMEvent<KeyboardEvent>(document, 'keydown', (event) => {
    if (event.shiftKey && (event.key === 'ArrowLeft' || event.key === 'ArrowRight' || event.key === 'ArrowUp' || event.key === 'ArrowDown')) keyboardSelecting.current = true
  }, undefined, active)
  useDOMEvent<KeyboardEvent>(document, 'keyup', (event) => {
    if (event.key === 'Shift' && keyboardSelecting.current) {
      keyboardSelecting.current = false
      scheduleFrame(show)
    }
  }, undefined, active)
  useDOMEvent<Event>(document, 'selectionchange', () => {
    if (!active || pointer.current || keyboardSelecting.current) return
    scheduleFrame(() => { if (!pointer.current && !keyboardSelecting.current) show() })
  }, undefined, active)
  useDOMEvent<CustomEvent>(window, fileResolveGestureEvent, () => {
    suppressUntil.current = performance.now() + 500
    close()
  }, undefined, active)
  useDOMEvent<PointerEvent>(window, 'pointerdown', (event) => {
    if (!capture || (event.target as Element).closest('.note-capture-popover')) return
    close()
  }, undefined, Boolean(capture))

  const onPointerDown = (event: ReactPointerEvent<HTMLElement>) => {
    pointer.current = undefined
    const target = event.nativeEvent.composedPath().find((item): item is Element => item instanceof Element)
    if (target?.closest('button, input, textarea, select, a, .note-capture-popover')) return
    event.currentTarget.setPointerCapture(event.pointerId)
    pointer.current = { x: event.clientX, y: event.clientY }
  }
  const onPointerUp = (event: ReactPointerEvent<HTMLElement>) => {
    const start = pointer.current
    pointer.current = undefined
    if (!start || Math.hypot(event.clientX - start.x, event.clientY - start.y) < 3) return
    window.dispatchEvent(new CustomEvent(noteCaptureGestureEvent))
    scheduleFrame(show)
  }
  const onPointerCancel = () => { pointer.current = undefined }
  const save = (group: string, comment: string) => {
    if (!capture) return
    const result = store.add({ group, text: captureNoteText(capture.quote, comment), source: capture.source })
    if (!result.ok) { announce(result.reason); return }
    announce(`Saved a note in ${group === 'general' ? 'unassigned' : group}.`)
    close()
  }
  const element = capture ? <NoteCaptureChip capture={capture} notes={notes} agents={agents} onSave={save} onAbandon={close} /> : null
  return { containerRef, onPointerDown, onPointerUp, onPointerCancel, element, show, close }
}
