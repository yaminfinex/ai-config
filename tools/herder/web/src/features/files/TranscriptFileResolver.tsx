import { useCallback, useEffect, useRef, useState } from 'react'
import { resolveFiles } from '../../api/client'
import type { FileCandidate, FileTarget, ResolveResponse } from '../../types'
import { autoOpenCandidate, hasPathSignal, isConfidentResolution, isRenderedInlineCode, mentionLine, pathTokenSpanAt } from './fileResolution'
import { FileResults } from './FileResults'

type PopoverState = { left: number, top: number, mention: string, resolution: ResolveResponse }

function caretAtPoint(x: number, y: number): { node: Node, offset: number } | null {
  const withPosition = document as Document & { caretPositionFromPoint?: (x: number, y: number) => { offsetNode: Node, offset: number } | null }
  const position = withPosition.caretPositionFromPoint?.(x, y)
  if (position) return { node: position.offsetNode, offset: position.offset }
  const withRange = document as Document & { caretRangeFromPoint?: (x: number, y: number) => Range | null }
  const range = withRange.caretRangeFromPoint?.(x, y)
  return range ? { node: range.startContainer, offset: range.startOffset } : null
}

function textPoint(event: React.MouseEvent<HTMLElement>) {
  const caret = caretAtPoint(event.clientX, event.clientY)
  if (caret?.node.nodeType === Node.TEXT_NODE) return caret
  const selection = document.getSelection()
  return selection?.anchorNode?.nodeType === Node.TEXT_NODE ? { node: selection.anchorNode, offset: selection.anchorOffset } : null
}

export function useTranscriptFileResolver(agent: string, enabled: boolean, onOpenFile: (target: FileTarget) => void) {
  const [popover, setPopover] = useState<PopoverState | null>(null)
  const request = useRef<AbortController | null>(null)

  const close = useCallback(() => setPopover(null), [])
  useEffect(() => () => request.current?.abort(), [])
  useEffect(() => {
    if (enabled) return
    request.current?.abort()
    request.current = null
    close()
  }, [close, enabled])
  useEffect(() => {
    if (!popover) return
    const keydown = (event: KeyboardEvent) => { if (event.key === 'Escape') close() }
    const pointer = (event: PointerEvent) => { if (!(event.target as Element).closest('.selection-file-popover')) close() }
    window.addEventListener('keydown', keydown)
    window.addEventListener('pointerdown', pointer)
    return () => { window.removeEventListener('keydown', keydown); window.removeEventListener('pointerdown', pointer) }
  }, [close, popover])

  const onDoubleClick = useCallback(async (event: React.MouseEvent<HTMLElement>) => {
    request.current?.abort()
    request.current = null
    close()
    const target = event.target as Element
    if (target.closest('a, button, header, summary, .window-note, .entry-time')) return
    const point = textPoint(event)
    if (!point) return
    const text = point.node.textContent ?? ''
    const renderedCode = isRenderedInlineCode(target)
    const token = pathTokenSpanAt(text, point.offset, renderedCode)
    const mention = token.text
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
      const resolution = await resolveFiles(mention, agent, fetch, controller.signal)
      if (controller.signal.aborted || !enabled || !isConfidentResolution(resolution, mention)) return
      const certain = autoOpenCandidate(resolution)
      if (certain) {
        onOpenFile({ root: certain.root, path: certain.path, line: mentionLine(mention).line })
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
  }, [agent, close, enabled, onOpenFile])

  const choose = (candidate: FileCandidate) => {
    if (!popover) return
    onOpenFile({ root: candidate.root, path: candidate.path, line: mentionLine(popover.mention).line })
    close()
  }
  const element = popover ? <aside className="selection-file-popover" role="dialog" aria-label={`Files matching ${popover.mention}`} style={{ left: popover.left, top: popover.top }}>
    <header><strong>{popover.mention}</strong><button type="button" aria-label="Close file matches" onClick={close}>×</button></header>
    <FileResults resolution={popover.resolution} onSelect={choose} limit={8} />
  </aside> : null
  return { onDoubleClick, element }
}
