import { useCallback, useEffect, useRef, useState } from 'react'
import { resolveFiles, type ResolveContext } from '../../api/client'
import type { FileCandidate, FileTarget, FolderTarget, ResolveResponse } from '../../types'
import { autoOpenCandidate, hasPathSignal, isConfidentResolution, isRenderedInlineCode, mentionLine, pathTokenSpanAt } from './fileResolution'
import { FileResults } from './FileResults'
import { candidateDestination } from '../folders/folderModel'
import { placementFromModifiers, type OpenPlacement } from '../layout/openPlacement'
import { useDOMEvent } from '../../shared/lifecycle'
import { fileResolveGestureEvent, noteCaptureGestureEvent } from '../../shared/selectionPopoverEvents'

type PopoverState = { left: number, top: number, mention: string, resolution: ResolveResponse }

function eventShadowRoots(event: React.MouseEvent<HTMLElement>) {
  const roots: ShadowRoot[] = []
  for (const item of event.nativeEvent.composedPath()) {
    if (!(item instanceof Node)) continue
    const root = item.getRootNode()
    if (root instanceof ShadowRoot && !roots.includes(root)) roots.push(root)
  }
  return roots
}

function caretAtPoint(x: number, y: number, shadowRoots: ShadowRoot[]): { node: Node, offset: number } | null {
  const withPosition = document as unknown as { caretPositionFromPoint?: (x: number, y: number, options?: { shadowRoots: ShadowRoot[] }) => { offsetNode: Node, offset: number } | null }
  const position = withPosition.caretPositionFromPoint?.(x, y, shadowRoots.length > 0 ? { shadowRoots } : undefined)
  if (position) return { node: position.offsetNode, offset: position.offset }
  const withRange = document as Document & { caretRangeFromPoint?: (x: number, y: number) => Range | null }
  const range = withRange.caretRangeFromPoint?.(x, y)
  return range ? { node: range.startContainer, offset: range.startOffset } : null
}

function textPoint(event: React.MouseEvent<HTMLElement>) {
  const caret = caretAtPoint(event.clientX, event.clientY, eventShadowRoots(event))
  if (caret?.node.nodeType === Node.TEXT_NODE) return caret
  const selection = document.getSelection()
  return selection?.anchorNode?.nodeType === Node.TEXT_NODE ? { node: selection.anchorNode, offset: selection.anchorOffset } : null
}

export function useTranscriptFileResolver(context: ResolveContext, enabled: boolean, onOpenFile: (target: FileTarget, placement?: OpenPlacement) => void, onOpenFolder: (target: FolderTarget, placement?: OpenPlacement) => void) {
  const [popover, setPopover] = useState<PopoverState | null>(null)
  const request = useRef<AbortController | null>(null)

  const close = useCallback(() => setPopover(null), [])
  const cancel = useCallback(() => {
    request.current?.abort()
    request.current = null
    close()
  }, [close])
  useEffect(() => () => request.current?.abort(), [])
  useEffect(() => {
    if (enabled) return
    cancel()
  }, [cancel, enabled])
  useDOMEvent<KeyboardEvent>(window, 'keydown', (event) => { if (event.key === 'Escape') close() }, undefined, Boolean(popover))
  useDOMEvent<PointerEvent>(window, 'pointerdown', (event) => {
    if (!(event.target as Element).closest('.selection-file-popover')) close()
  }, undefined, Boolean(popover))
  useDOMEvent(window, noteCaptureGestureEvent, cancel, undefined, enabled)

  const onDoubleClick = useCallback(async (event: React.MouseEvent<HTMLElement>) => {
    window.dispatchEvent(new CustomEvent(fileResolveGestureEvent))
    cancel()
    const target = event.nativeEvent.composedPath().find((item): item is Element => item instanceof Element) ?? event.target as Element
    if (target.closest('a, button, header, summary, .window-note, .entry-time')) return
    const point = textPoint(event)
    if (!point) return
    const text = point.node.textContent ?? ''
    const renderedCode = isRenderedInlineCode(target)
    const token = pathTokenSpanAt(text, point.offset, renderedCode)
    const mention = token.text
    const placement = placementFromModifiers(event)
    const codeOrQuoted = Boolean(target.closest('code, pre')) || /^[`"']/.test(mention)
    if (!mention || !hasPathSignal(mention, codeOrQuoted)) return
    const range = document.createRange()
    range.setStart(point.node, token.start)
    range.setEnd(point.node, token.end)
    const selection = document.getSelection()
    selection?.removeAllRanges()
    selection?.addRange(range)
    const rect = range.getBoundingClientRect()
    const controller = new AbortController()
    request.current = controller
    try {
      const resolution = await resolveFiles(mention, context, fetch, controller.signal)
      if (controller.signal.aborted || !enabled || !isConfidentResolution(resolution, mention)) return
      const certain = autoOpenCandidate(resolution)
      if (certain) {
        if (candidateDestination(certain) === 'folder') onOpenFolder({ root: certain.root, path: certain.path }, placement)
        else onOpenFile({ root: certain.root, path: certain.path, line: mentionLine(mention).line }, placement)
        return
      }
      setPopover({
        mention,
        resolution,
        left: Math.max(8, Math.min(rect.left, window.innerWidth - 430)),
        top: Math.max(8, Math.min(rect.bottom + 6, window.innerHeight - 320)),
      })
    } catch (error) {
      if (!(error instanceof DOMException && error.name === 'AbortError')) close()
    } finally {
      if (request.current === controller) request.current = null
    }
  }, [cancel, close, context, enabled, onOpenFile, onOpenFolder])

  const choose = (candidate: FileCandidate, event: React.MouseEvent<HTMLButtonElement>) => {
    if (!popover) return
    const placement = placementFromModifiers(event)
    if (candidateDestination(candidate) === 'folder') onOpenFolder({ root: candidate.root, path: candidate.path }, placement)
    else onOpenFile({ root: candidate.root, path: candidate.path, line: mentionLine(popover.mention).line }, placement)
    close()
  }
  const element = popover ? <aside className="selection-file-popover" role="dialog" aria-label={`Files matching ${popover.mention}`} style={{ left: popover.left, top: popover.top }}>
    <header><strong>{popover.mention}</strong><button type="button" aria-label="Close file matches" onClick={close}>×</button></header>
    <FileResults resolution={popover.resolution} onSelect={choose} limit={8} />
  </aside> : null
  return { onDoubleClick, element }
}
