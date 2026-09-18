// Full-viewport view of an already-drawn diagram with pan and zoom (decisions 2 to 4).
// The overlay shows the SVG string the block rendered; nothing is rendered again and no
// user HTML reaches the DOM. Pan and zoom are one CSS transform on a wrapper; the maths
// lives in the pure helpers below, and every control goes through one `apply(action)`.
import { createElement, useEffect, useLayoutEffect, useRef, useState, type KeyboardEvent, type PointerEvent } from 'react'
import { dialogTabTargetIndex } from '../features/launch/launchModel.ts'

export type Size = { width: number, height: number }
export type Point = { x: number, y: number }
/** The transform: content is translated by (x, y) viewport pixels, then scaled about its top-left corner. */
export type View = Point & { scale: number }

const zoomBounds = { min: 0.1, max: 8 }
const zoomStep = 1.25
const panStep = 40
const fitPadding = 24
const focusableSelector = 'button:not([disabled]), [tabindex]:not([tabindex="-1"])'

const clamp = (value: number, min: number, max: number) => Math.min(max, Math.max(min, value))

/**
 * Largest scale at which the whole diagram fits the viewport with a margin. Fit always fits:
 * a diagram wider than ten viewports goes below the 0.1 zoom floor (the user can only zoom in
 * from there); the 8 ceiling still holds for tiny diagrams.
 */
export function fitScale(content: Size, viewport: Size, padding = fitPadding): number {
  if (content.width <= 0 || content.height <= 0) return 1
  const raw = Math.min((viewport.width - 2 * padding) / content.width, (viewport.height - 2 * padding) / content.height)
  return clamp(raw, Math.min(zoomBounds.min, raw), zoomBounds.max)
}

/** The view that centres the diagram in the viewport at a scale (a fit scale may sit below the zoom floor). */
export function centredView(content: Size, viewport: Size, scale: number): View {
  const clamped = clamp(scale, Math.min(zoomBounds.min, scale), zoomBounds.max)
  return { x: (viewport.width - content.width * clamped) / 2, y: (viewport.height - content.height * clamped) / 2, scale: clamped }
}

/** Changes the scale so the content under `point` (viewport coordinates) stays under it; bounded 0.1 to 8 unless already below the floor. */
export function zoomAbout(view: View, scale: number, point: Point): View {
  const clamped = clamp(scale, Math.min(zoomBounds.min, view.scale), zoomBounds.max)
  const ratio = clamped / view.scale
  return { x: point.x - (point.x - view.x) * ratio, y: point.y - (point.y - view.y) * ratio, scale: clamped }
}

/** Wheel delta (pixels, or lines on browsers that report them) to a zoom factor: a notch is about ×0.78 or ×1.28. */
export function wheelFactor(deltaY: number, deltaMode: number): number {
  const pixels = clamp(deltaMode === 1 ? deltaY * 16 : deltaY, -200, 200)
  return Math.exp(-pixels / 400)
}

/** The view after the viewport changed size: a fitted view is fitted again; a browsed view is left alone. */
export function resizedView(view: View, fitted: boolean, content: Size, viewport: Size): View {
  return fitted ? centredView(content, viewport, fitScale(content, viewport)) : view
}

export type OverlayAction = 'in' | 'out' | 'fit' | 'natural' | 'close' | { pan: Point } | { factor: number, about: Point }

/** Keyboard map: `+`/`-` zoom, `0` fit, `1` natural size, arrows pan the view, Escape closes. */
export function overlayAction(key: string): OverlayAction | undefined {
  switch (key) {
    case '+': case '=': return 'in'
    case '-': case '_': return 'out'
    case '0': return 'fit'
    case '1': return 'natural'
    case 'Escape': return 'close'
    case 'ArrowLeft': return { pan: { x: panStep, y: 0 } }
    case 'ArrowRight': return { pan: { x: -panStep, y: 0 } }
    case 'ArrowUp': return { pan: { x: 0, y: panStep } }
    case 'ArrowDown': return { pan: { x: 0, y: -panStep } }
    default: return undefined
  }
}

const zoomLabel = (scale: number) => `${Math.round(scale * 100)} %`

/**
 * Makes the application (#root) inert while the overlay is open and returns the undo. Only #root:
 * a layer that mounts later as another body child (the quick-open palette) stays interactive above
 * the overlay. A #root that was inert already is left to whoever made it so.
 */
export function coverApplication(doc: Pick<Document, 'getElementById'>): () => void {
  const app = doc.getElementById('root')
  if (!app || app.hasAttribute('inert')) return () => {}
  app.setAttribute('inert', '')
  return () => app.removeAttribute('inert')
}

export function DiagramOverlay({ svg, onClose }: { svg: string, onClose: () => void }) {
  const root = useRef<HTMLDivElement | null>(null)
  const viewport = useRef<HTMLDivElement | null>(null)
  const content = useRef<HTMLDivElement | null>(null)
  const drag = useRef<(Point & { outside: boolean, moved: boolean }) | null>(null)
  // True while the view is the fitted one (opened at fit, the fit button, `0`); any zoom or pan clears it.
  const fitted = useRef(true)
  const [view, setView] = useState<View>({ x: 0, y: 0, scale: 1 })

  const sizes = () => ({
    content: { width: content.current?.offsetWidth ?? 0, height: content.current?.offsetHeight ?? 0 },
    viewport: { width: viewport.current?.clientWidth ?? 0, height: viewport.current?.clientHeight ?? 0 },
  })
  const apply = (action: OverlayAction) => {
    const measured = sizes()
    const centre = { x: measured.viewport.width / 2, y: measured.viewport.height / 2 }
    fitted.current = action === 'fit'
    if (action === 'close') onClose()
    else if (action === 'fit') setView(centredView(measured.content, measured.viewport, fitScale(measured.content, measured.viewport)))
    else if (action === 'natural') setView(centredView(measured.content, measured.viewport, 1))
    else if (action === 'in') setView((current) => zoomAbout(current, current.scale * zoomStep, centre))
    else if (action === 'out') setView((current) => zoomAbout(current, current.scale / zoomStep, centre))
    else if ('pan' in action) setView((current) => ({ ...current, x: current.x + action.pan.x, y: current.y + action.pan.y }))
    else setView((current) => zoomAbout(current, current.scale * action.factor, action.about))
  }

  // Opening state is "fit" (decision 3); the measurement needs the SVG in the DOM, hence the layout effect.
  useLayoutEffect(() => { apply('fit') }, [svg])
  useEffect(() => {
    // Modal containment: focus moves in, the covered application is inert, the page behind does not scroll.
    root.current?.focus()
    const uncover = coverApplication(document)
    const { overflow } = document.body.style
    document.body.style.overflow = 'hidden'
    return () => {
      uncover()
      document.body.style.overflow = overflow
    }
  }, [])
  useEffect(() => {
    const element = viewport.current
    if (!element) return
    // Native listener: React registers wheel as passive, and Ctrl/⌘-wheel must not zoom the page.
    const onWheel = (event: WheelEvent) => {
      event.preventDefault()
      const box = element.getBoundingClientRect()
      apply({ factor: wheelFactor(event.deltaY, event.deltaMode), about: { x: event.clientX - box.left, y: event.clientY - box.top } })
    }
    element.addEventListener('wheel', onWheel, { passive: false })
    // A fitted view follows the viewport; a browsed view stays where the user put it.
    const observer = new ResizeObserver(() => { const measured = sizes(); setView((current) => resizedView(current, fitted.current, measured.content, measured.viewport)) })
    observer.observe(element)
    return () => { element.removeEventListener('wheel', onWheel); observer.disconnect() }
  }, [])

  const onKeyDown = (event: KeyboardEvent) => {
    if (event.key === 'Tab') {
      const items = [...(root.current?.querySelectorAll<HTMLElement>(focusableSelector) ?? [])]
      const next = dialogTabTargetIndex(items.indexOf(document.activeElement as HTMLElement), items.length, event.shiftKey)
      if (next === null) return
      event.preventDefault()
      items[next]?.focus()
      return
    }
    const action = overlayAction(event.key)
    if (action === undefined) return
    event.preventDefault()
    event.stopPropagation()
    apply(action)
  }
  const onPointerDown = (event: PointerEvent<HTMLDivElement>) => {
    if (event.pointerType === 'mouse' && event.button !== 0) return
    event.currentTarget.setPointerCapture(event.pointerId)
    // Decided here: capture retargets the later pointerup to the viewport, so its target says nothing.
    drag.current = { x: event.clientX, y: event.clientY, outside: !content.current?.contains(event.target as Node), moved: false }
  }
  const onPointerMove = (event: PointerEvent<HTMLDivElement>) => {
    const start = drag.current
    if (!start) return
    const delta = { x: event.clientX - start.x, y: event.clientY - start.y }
    if (!start.moved && Math.hypot(delta.x, delta.y) < 4) return
    start.moved = true
    start.x = event.clientX
    start.y = event.clientY
    apply({ pan: delta })
  }
  const onPointerUp = () => {
    const start = drag.current
    drag.current = null
    // A plain click that began on the backdrop closes; a drag anywhere pans; a click on the diagram does nothing.
    if (start && start.outside && !start.moved) onClose()
  }

  const button = (label: string, ariaLabel: string, action: OverlayAction) => createElement('button', { type: 'button', 'aria-label': ariaLabel, onClick: () => apply(action) }, label)
  return createElement('div', { ref: root, className: 'diagram-overlay', role: 'dialog', 'aria-modal': true, 'aria-label': 'Diagram', tabIndex: -1, onKeyDown },
    createElement('div', { className: 'diagram-overlay-toolbar', role: 'toolbar', 'aria-label': 'Diagram zoom' },
      button('−', 'Zoom out', 'out'),
      createElement('span', { className: 'diagram-overlay-zoom', 'aria-live': 'polite' }, zoomLabel(view.scale)),
      button('+', 'Zoom in', 'in'),
      button('fit', 'Fit diagram to the window', 'fit'),
      button('100 %', 'Show natural size', 'natural'),
      button('×', 'Close diagram', 'close')),
    createElement('div', { ref: viewport, className: 'diagram-overlay-viewport', onPointerDown, onPointerMove, onPointerUp, onPointerCancel: () => { drag.current = null } },
      // The block's own mermaid output (securityLevel strict), shown a second time; no user HTML (decision 7).
      createElement('div', { ref: content, className: 'diagram-overlay-content', style: { transform: `translate(${view.x}px, ${view.y}px) scale(${view.scale})` }, dangerouslySetInnerHTML: { __html: svg } })))
}
