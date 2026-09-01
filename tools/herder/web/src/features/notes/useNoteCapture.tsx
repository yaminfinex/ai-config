import { useCallback, useEffect, useRef, useState } from 'react'
import { useDOMEvent, useScheduledFrame } from '../../shared/lifecycle.ts'
import { fileResolveGestureEvent, noteCaptureGestureEvent } from '../../shared/selectionPopoverEvents.ts'
import {
  capturePosition,
  captureSourceWithRange,
  isRangeSelection,
  isReservedFileResolutionSelection,
  reserveSelectionForFileResolution,
  sharedCaptureSurface,
  type ReservedSelection,
} from './noteCaptureModel.ts'
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

function openShadowRoots(root: ParentNode, found: ShadowRoot[] = []) {
  for (const element of root.querySelectorAll('*')) {
    const shadowRoot = element.shadowRoot
    if (!shadowRoot) continue
    found.push(shadowRoot)
    openShadowRoots(shadowRoot, found)
  }
  return found
}

function activeSelection(container: HTMLElement) {
  const roots = openShadowRoots(container)
  for (const shadowRoot of roots) {
    const selection = (shadowRoot as ShadowRoot & { getSelection?: () => Selection | null }).getSelection?.()
    if (selection?.toString().trim()) return { selection, roots }
  }
  return { selection: document.getSelection(), roots }
}

function composedRange(selection: Selection, roots: ShadowRoot[]) {
  const composed = (selection as Selection & {
    getComposedRanges?: (options: { shadowRoots: ShadowRoot[] }) => StaticRange[]
  }).getComposedRanges?.({ shadowRoots: roots })[0]
  if (!composed) return selection.getRangeAt(0)
  const range = document.createRange()
  range.setStart(composed.startContainer, composed.startOffset)
  range.setEnd(composed.endContainer, composed.endOffset)
  return range
}

function closestComposed(node: Node | null, selector: string) {
  let element = selectionElement(node)
  while (element) {
    const match = element.closest<HTMLElement>(selector)
    if (match) return match
    const root = element.getRootNode()
    element = root instanceof ShadowRoot ? root.host : null
  }
  return null
}

function captureSurface(node: Node) {
  return closestComposed(node, '[data-note-capture-content]')
}

function selectedLine(node: Node | null) {
  const raw = closestComposed(node, '[data-line]')?.dataset.line
  if (!raw || !/^\d+$/.test(raw)) return undefined
  return Number(raw)
}

export function useNoteCapture({ active, source, agents }: { active: boolean, source: NoteSource, agents: string[] }) {
  const { store, notes, announce } = useNotes()
  const containerRef = useRef<HTMLElement>(null)
  const pointer = useRef(false)
  const keyboardSelecting = useRef(false)
  const fileResolutionSelection = useRef<ReservedSelection>(null)
  const [capture, setCapture] = useState<NoteCaptureDraft | null>(null)
  const scheduleFrame = useScheduledFrame()

  const close = useCallback(() => setCapture(null), [])
  const show = useCallback(() => {
    if (!active || !containerRef.current) return false
    const { selection, roots } = activeSelection(containerRef.current)
    if (!selection || selection.isCollapsed || selection.rangeCount === 0) return false
    if (isReservedFileResolutionSelection(selection, fileResolutionSelection.current)) return false
    const range = composedRange(selection, roots)
    const surface = sharedCaptureSurface(range.startContainer, range.endContainer, captureSurface)
    if (!surface || !belongsTo(containerRef.current, surface)) return false
    const quote = range.toString().trim()
    if (!quote) return false
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
  useDOMEvent<CustomEvent>(window, fileResolveGestureEvent, close, undefined, active)
  useDOMEvent<PointerEvent>(window, 'pointerup', () => {
    if (!pointer.current) return
    pointer.current = false
    if (!containerRef.current) return
    const { selection } = activeSelection(containerRef.current)
    if (!isRangeSelection(selection)) return
    window.dispatchEvent(new CustomEvent(noteCaptureGestureEvent))
    scheduleFrame(show)
  }, undefined, active)
  useDOMEvent<PointerEvent>(window, 'pointercancel', () => { pointer.current = false }, undefined, active)
  useDOMEvent<PointerEvent>(window, 'pointerdown', (event) => {
    pointer.current = false
    const path = event.composedPath()
    const target = path.find((item): item is Element => item instanceof Element)
    if (capture && !target?.closest('.note-capture-popover')) close()
    if (!containerRef.current || !path.includes(containerRef.current)) return
    if (target?.closest('button, input, textarea, select, a, .note-capture-popover')) return
    pointer.current = true
  }, undefined, active)
  const onDoubleClick = () => {
    const selection = containerRef.current ? activeSelection(containerRef.current).selection : document.getSelection()
    fileResolutionSelection.current = reserveSelectionForFileResolution(selection, () => {
      window.dispatchEvent(new CustomEvent(fileResolveGestureEvent))
    })
    close()
  }
  const save = (group: string, comment: string) => {
    if (!capture) return
    const result = store.add({ group, quote: capture.quote, text: comment, source: capture.source })
    if (!result.ok) { announce(result.reason); return }
    announce(`Saved a note in ${group === 'general' ? 'unassigned' : group}.`)
    close()
  }
  const element = capture ? <NoteCaptureChip capture={capture} notes={notes} agents={agents} onSave={save} onAbandon={close} /> : null
  return { containerRef, onDoubleClick, element, show, close }
}
