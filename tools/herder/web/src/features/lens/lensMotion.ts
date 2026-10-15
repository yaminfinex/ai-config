import { morphInFrames, morphInMs, morphOutFrames, morphOutMs, motionTiming, swipeFrames, swipeMs, type Bounds } from './lensMotionModel.ts'

// The lens motion drawn on the page (see lensMotionModel). Each runs on the
// dock host, the one element the lens, a zoom and an agent alone share, and
// only ever animates paint: the view, focus and keys have already moved.

const reduced = () => window.matchMedia('(prefers-reduced-motion: reduce)').matches
const host = () => document.querySelector<HTMLElement>('.dock-host')
const cardOf = (spaceID: string) => document.querySelector<HTMLElement>(`.lens-card[data-space-id="${CSS.escape(spaceID)}"]`)
const bounds = (element: Element): Bounds => element.getBoundingClientRect()

// A new motion replaces the one still running.
let running: Animation | null = null
function play(target: HTMLElement, frames: Keyframe[], duration: number, done?: () => void) {
  running?.cancel()
  const animation = target.animate(frames, motionTiming(duration))
  running = animation
  const settle = () => {
    if (running === animation) running = null
    done?.()
  }
  animation.addEventListener('finish', settle)
  animation.addEventListener('cancel', settle)
}

// cardBounds is the space's card as the lens draws it now; read it before the zoom covers the lens.
export function cardBounds(spaceID: string): Bounds | null {
  if (reduced()) return null
  const card = cardOf(spaceID)
  return card ? bounds(card) : null
}

// morphIn grows the zoom out of the card it was opened from.
export function morphIn(card: Bounds | null) {
  const target = host()
  if (!card || !target || reduced()) return
  play(target, morphInFrames(bounds(target), card), morphInMs)
}

// morphOut shrinks the zoom back into its card over the lens just drawn:
// the dock stays shown above the lens (data-morph) until it lands. The lens
// already has the keys.
export function morphOut(spaceID: string) {
  const target = host()
  if (!target || reduced()) return
  target.dataset.morph = 'out'
  window.requestAnimationFrame(() => {
    const dock = target.querySelector<HTMLElement>(':scope > .dv-react-wrapper, :scope > .dv-dockview, :scope > div:not(.lens, .alone-agent)')
    const card = cardOf(spaceID)
    if (!dock || !card) { delete target.dataset.morph; return }
    play(dock, morphOutFrames(bounds(target), bounds(card)), morphOutMs, () => { delete target.dataset.morph })
  })
}

// swipe slides the next zoom in from the side it lies on.
export function swipe(by: -1 | 1) {
  const target = host()
  if (!target || reduced()) return
  play(target, swipeFrames(by), swipeMs)
}
