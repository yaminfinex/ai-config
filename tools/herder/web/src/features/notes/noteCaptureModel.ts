import type { NoteSource } from './notesStore'

type SelectionLike = {
  isCollapsed: boolean
  anchorNode: unknown | null
  anchorOffset: number
  focusNode: unknown | null
  focusOffset: number
}

export type ReservedSelection = Omit<SelectionLike, 'isCollapsed'> | null

export function isRangeSelection(selection: SelectionLike | null): selection is SelectionLike {
  return Boolean(selection && !selection.isCollapsed && (selection.anchorNode !== selection.focusNode || selection.anchorOffset !== selection.focusOffset))
}

export function reserveSelectionForFileResolution(selection: SelectionLike | null, dispatch: () => void): ReservedSelection {
  const reserved = selection ? {
    anchorNode: selection.anchorNode,
    anchorOffset: selection.anchorOffset,
    focusNode: selection.focusNode,
    focusOffset: selection.focusOffset,
  } : null
  dispatch()
  return reserved
}

export function isReservedFileResolutionSelection(selection: SelectionLike | null, reserved: ReservedSelection) {
  return Boolean(selection && reserved
    && selection.anchorNode === reserved.anchorNode
    && selection.anchorOffset === reserved.anchorOffset
    && selection.focusNode === reserved.focusNode
    && selection.focusOffset === reserved.focusOffset)
}

export function sharedCaptureSurface<NodeType, SurfaceType>(start: NodeType, end: NodeType, resolve: (node: NodeType) => SurfaceType | null) {
  const surface = resolve(start)
  return surface !== null && surface === resolve(end) ? surface : null
}

export function capturePosition(rect: Pick<DOMRect, 'left' | 'bottom'>, viewportWidth: number, viewportHeight: number) {
  return {
    left: Math.max(8, Math.min(rect.left, viewportWidth - 340)),
    top: Math.max(8, Math.min(rect.bottom + 6, viewportHeight - 328)),
  }
}

export function captureSourceWithRange(source: NoteSource, start?: number, end?: number): NoteSource {
  if (source.kind === 'transcript' || start === undefined || end === undefined) return source
  return { ...source, start: Math.min(start, end), end: Math.max(start, end) }
}

export function placeCaretAtEnd(field: Pick<HTMLTextAreaElement, 'value' | 'focus' | 'setSelectionRange'>) {
  field.focus()
  field.setSelectionRange(field.value.length, field.value.length)
}

export type CaptureSubmitAction = 'queue' | 'send' | 'append'

// Decides what Enter does in the capture chip. Plain Enter queues; cmd/ctrl+Enter
// sends to a live agent, falls back to appending to its prompt, and still queues
// when there is no agent to send to. Shift+Enter and IME composition are left alone.
export function captureSubmitAction(
  event: Pick<KeyboardEvent, 'key' | 'metaKey' | 'ctrlKey' | 'shiftKey'> & { isComposing?: boolean },
  context: { group: string, readOnly: string, live: boolean },
): CaptureSubmitAction | null {
  if (event.key !== 'Enter' || event.shiftKey || event.isComposing) return null
  if (!(event.metaKey || event.ctrlKey) || context.group === 'general') return 'queue'
  return context.readOnly || !context.live ? 'append' : 'send'
}
