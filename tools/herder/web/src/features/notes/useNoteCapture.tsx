import { useCallback, useEffect, useRef, useState } from 'react'
import { useDOMEvent, useScheduledFrame } from '../../shared/lifecycle.ts'
import { fileResolveGestureEvent, noteCaptureGestureEvent } from '../../shared/selectionPopoverEvents.ts'
import {
  capturePosition,
  captureSourceWithRange,
  isRangeSelection,
  isReservedFileResolutionSelection,
  proposedCaptureGroup,
  reserveSelectionForFileResolution,
  sharedCaptureSurface,
  type CaptureSubmitAction,
  type ReservedSelection,
} from './noteCaptureModel.ts'
import { noteTransferText } from './notesPresentation.ts'
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

// quickSend is the transcript panel's send path (the Composer's request and refresh
// markers) plus the hand-off append; without it cmd+enter still queues.
export type NoteQuickSend = {
  readOnly: string
  send: (agent: string, text: string) => Promise<void>
  append: (agent: string, text: string) => { ok: true } | { ok: false, reason: string }
}

export function useNoteCapture({ active, source, agents, quickSend }: { active: boolean, source: NoteSource, agents: string[], quickSend?: NoteQuickSend }) {
  const { store, announce } = useNotes()
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
    setCapture({ quote, source: provenSource, ...position, group: proposedCaptureGroup(source, store.lastTarget(), agents) })
    return true
  }, [active, agents, source, store])

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
  const appendToPrompt = (quick: NoteQuickSend, group: string, text: string, reason: string) => {
    const result = quick.append(group, text)
    announce(result.ok ? `${reason} — added to ${group}'s prompt instead.` : result.reason)
    return result.ok
  }
  const save = (group: string, comment: string, action: CaptureSubmitAction) => {
    if (!capture) return
    const note = { group, quote: capture.quote, text: comment, source: capture.source }
    if (capture.source.kind !== 'transcript') store.rememberTarget(group)
    if (action === 'queue' || !quickSend) {
      const result = store.add(note)
      if (!result.ok) { announce(result.reason); return }
      announce(`Saved a note in ${group === 'general' ? 'unassigned' : group}.`)
      close()
      return
    }
    const text = noteTransferText(note)
    if (action === 'append') {
      if (appendToPrompt(quickSend, group, text, quickSend.readOnly ? 'Read-only' : `${group} is not live`)) close()
      return
    }
    close()
    void quickSend.send(group, text).then(
      () => announce(`Sent a note to ${group}.`),
      (error: unknown) => appendToPrompt(quickSend, group, text, `Send failed (${error instanceof Error ? error.message : String(error)})`),
    )
  }
  const element = capture ? <NoteCaptureChip capture={capture} agents={agents} readOnly={quickSend?.readOnly ?? ''} onSave={save} onAbandon={close} /> : null
  return { containerRef, onDoubleClick, element, show, close }
}
