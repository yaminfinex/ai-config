// Full-viewport view of an already-drawn diagram with pan and zoom (decisions 2 to 4).
// The overlay shows the SVG string the block rendered; nothing is rendered again and no
// user HTML reaches the DOM. Pan and zoom are one CSS transform on a wrapper; the maths
// lives in the pure helpers below.
import { createElement, useEffect, useLayoutEffect, useRef, useState, type KeyboardEvent, type PointerEvent } from 'react'

export type Size = { width: number, height: number }
export type Point = { x: number, y: number }
/** The transform: content is translated by (x, y) viewport pixels, then scaled about its top-left corner. */
export type View = Point & { scale: number }

export const zoomBounds = { min: 0.1, max: 8 } as const
export const zoomStep = 1.25
export const panStep = 40
export const fitPadding = 24

export const clamp = (value: number, min: number, max: number) => Math.min(max, Math.max(min, value))
export const clampScale = (scale: number) => clamp(scale, zoomBounds.min, zoomBounds.max)

/** Largest scale (within bounds) at which the whole diagram fits the viewport with a margin. */
export function fitScale(content: Size, viewport: Size, padding = fitPadding): number {
  if (content.width <= 0 || content.height <= 0) return 1
  return clampScale(Math.min((viewport.width - 2 * padding) / content.width, (viewport.height - 2 * padding) / content.height))
}

/** The view that centres the diagram in the viewport at a scale. */
export function centredView(content: Size, viewport: Size, scale: number): View {
  const clamped = clampScale(scale)
  return { x: (viewport.width - content.width * clamped) / 2, y: (viewport.height - content.height * clamped) / 2, scale: clamped }
}

/** Changes the scale so the content under `point` (viewport coordinates) stays under it. */
export function zoomAbout(view: View, scale: number, point: Point): View {
  const clamped = clampScale(scale)
  const ratio = clamped / view.scale
  return { x: point.x - (point.x - view.x) * ratio, y: point.y - (point.y - view.y) * ratio, scale: clamped }
}

/** Wheel delta (pixels, or lines on browsers that report them) to a zoom factor: a notch is about ×0.78 or ×1.28. */
export function wheelFactor(deltaY: number, deltaMode: number): number {
  const pixels = clamp(deltaMode === 1 ? deltaY * 16 : deltaY, -200, 200)
  return Math.exp(-pixels / 400)
}

export type OverlayAction = 'in' | 'out' | 'fit' | 'natural' | 'close' | Point

/** Keyboard map: `+`/`-` zoom, `0` fit, `1` natural size, arrows pan the view, Escape closes. */
export function overlayAction(key: string): OverlayAction | undefined {
  switch (key) {
    case '+': case '=': return 'in'
    case '-': case '_': return 'out'
    case '0': return 'fit'
    case '1': return 'natural'
    case 'Escape': return 'close'
    case 'ArrowLeft': return { x: panStep, y: 0 }
    case 'ArrowRight': return { x: -panStep, y: 0 }
    case 'ArrowUp': return { x: 0, y: panStep }
    case 'ArrowDown': return { x: 0, y: -panStep }
    default: return undefined
  }
}

const zoomLabel = (scale: number) => `${Math.round(scale * 100)} %`

export function DiagramOverlay({ svg, onClose }: { svg: string, onClose: () => void }) {
  const root = useRef<HTMLDivElement | null>(null)
  const viewport = useRef<HTMLDivElement | null>(null)
  const content = useRef<HTMLDivElement | null>(null)
  const drag = useRef<(Point & { moved: boolean }) | null>(null)
  const [view, setView] = useState<View>({ x: 0, y: 0, scale: 1 })

  const sizes = () => ({
    content: { width: content.current?.offsetWidth ?? 0, height: content.current?.offsetHeight ?? 0 },
    viewport: { width: viewport.current?.clientWidth ?? 0, height: viewport.current?.clientHeight ?? 0 },
  })
  const show = (scale: number | 'fit') => {
    const measured = sizes()
    setView(centredView(measured.content, measured.viewport, scale === 'fit' ? fitScale(measured.content, measured.viewport) : scale))
  }
  const zoomBy = (factor: number) => {
    const { viewport: box } = sizes()
    setView((current) => zoomAbout(current, current.scale * factor, { x: box.width / 2, y: box.height / 2 }))
  }
  const panBy = (delta: Point) => setView((current) => ({ ...current, x: current.x + delta.x, y: current.y + delta.y }))

  // Opening state is "fit" (decision 3); the measurement needs the SVG in the DOM, hence the layout effect.
  useLayoutEffect(() => { show('fit') }, [svg])
  useEffect(() => {
    root.current?.focus()
    const { overflow } = document.body.style
    document.body.style.overflow = 'hidden'
    return () => { document.body.style.overflow = overflow }
  }, [])
  useEffect(() => {
    // Native listener: React registers wheel as passive, and Ctrl/⌘-wheel must not zoom the page.
    const element = viewport.current
    if (!element) return
    const onWheel = (event: WheelEvent) => {
      event.preventDefault()
      const box = element.getBoundingClientRect()
      const factor = wheelFactor(event.deltaY, event.deltaMode)
      setView((current) => zoomAbout(current, current.scale * factor, { x: event.clientX - box.left, y: event.clientY - box.top }))
    }
    element.addEventListener('wheel', onWheel, { passive: false })
    return () => element.removeEventListener('wheel', onWheel)
  }, [])

  const onKeyDown = (event: KeyboardEvent) => {
    const action = overlayAction(event.key)
    if (action === undefined) return
    event.preventDefault()
    event.stopPropagation()
    if (action === 'close') onClose()
    else if (action === 'in') zoomBy(zoomStep)
    else if (action === 'out') zoomBy(1 / zoomStep)
    else if (action === 'fit') show('fit')
    else if (action === 'natural') show(1)
    else panBy(action)
  }
  const onPointerDown = (event: PointerEvent<HTMLDivElement>) => {
    if (event.pointerType === 'mouse' && event.button !== 0) return
    event.currentTarget.setPointerCapture(event.pointerId)
    drag.current = { x: event.clientX, y: event.clientY, moved: false }
  }
  const onPointerMove = (event: PointerEvent<HTMLDivElement>) => {
    const start = drag.current
    if (!start) return
    const delta = { x: event.clientX - start.x, y: event.clientY - start.y }
    if (!start.moved && Math.hypot(delta.x, delta.y) < 4) return
    start.moved = true
    start.x = event.clientX
    start.y = event.clientY
    panBy(delta)
  }
  const onPointerUp = (event: PointerEvent<HTMLDivElement>) => {
    const start = drag.current
    drag.current = null
    // A plain click on the backdrop (outside the diagram) closes; a drag anywhere pans.
    if (start && !start.moved && !content.current?.contains(event.target as Node)) onClose()
  }

  const button = (label: string, ariaLabel: string, onClick: () => void) => createElement('button', { type: 'button', 'aria-label': ariaLabel, onClick }, label)
  return createElement('div', { ref: root, className: 'diagram-overlay', role: 'dialog', 'aria-modal': true, 'aria-label': 'Diagram', tabIndex: -1, onKeyDown },
    createElement('div', { className: 'diagram-overlay-toolbar', role: 'toolbar', 'aria-label': 'Diagram zoom' },
      button('−', 'Zoom out', () => zoomBy(1 / zoomStep)),
      createElement('span', { className: 'diagram-overlay-zoom', 'aria-live': 'polite' }, zoomLabel(view.scale)),
      button('+', 'Zoom in', () => zoomBy(zoomStep)),
      button('fit', 'Fit diagram to the window', () => show('fit')),
      button('100 %', 'Show natural size', () => show(1)),
      button('×', 'Close diagram', onClose)),
    createElement('div', { ref: viewport, className: 'diagram-overlay-viewport', onPointerDown, onPointerMove, onPointerUp, onPointerCancel: () => { drag.current = null } },
      // The block's own mermaid output (securityLevel strict), shown a second time; no user HTML (decision 7).
      createElement('div', { ref: content, className: 'diagram-overlay-content', style: { transform: `translate(${view.x}px, ${view.y}px) scale(${view.scale})` }, dangerouslySetInnerHTML: { __html: svg } })))
}
