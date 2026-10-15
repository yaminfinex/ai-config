// The lens's motion (spec §3 Transitions), all ease-out-quint:
// - zoom in: the zoom grows out of the card's bounds, 280 ms, radius 8 → 0,
//   opacity 0.3 → 1;
// - zoom out: the reverse back into the card, 200 ms;
// - n, N and ⇧⌥←/→ between zooms: a swipe, 240 ms, in from 30% to the side
//   with a fade.
// None of it runs under prefers-reduced-motion, and none of it holds focus
// or input back: the view and the keys change at once, the motion only
// draws over it.

export const easeOutQuint = 'cubic-bezier(0.22, 1, 0.36, 1)'
export const morphInMs = 280
export const morphOutMs = 200
export const swipeMs = 240
export const cardRadius = 8

export type Bounds = { top: number, right: number, bottom: number, left: number }

export type MotionFrame = { clipPath?: string, opacity: number, transform?: string }

// cardClip is the clip that shows only the card's bounds of the zoom's host.
export function cardClip(host: Bounds, card: Bounds, radius: number): string {
  const inset = [card.top - host.top, host.right - card.right, host.bottom - card.bottom, card.left - host.left]
    .map((side) => `${Math.max(0, Math.round(side))}px`)
  return `inset(${inset.join(' ')} round ${radius}px)`
}

const fullClip = 'inset(0px 0px 0px 0px round 0px)'

export function morphInFrames(host: Bounds, card: Bounds): MotionFrame[] {
  return [{ clipPath: cardClip(host, card, cardRadius), opacity: 0.3 }, { clipPath: fullClip, opacity: 1 }]
}

export function morphOutFrames(host: Bounds, card: Bounds): MotionFrame[] {
  return [...morphInFrames(host, card)].reverse()
}

// swipeFrames slides the next zoom in from the side it lies on: the right
// for a step forward, the left for a step back.
export function swipeFrames(by: -1 | 1): MotionFrame[] {
  return [{ transform: `translateX(${by * 30}%)`, opacity: 0 }, { transform: 'translateX(0)', opacity: 1 }]
}

export function motionTiming(duration: number): KeyframeAnimationOptions {
  return { duration, easing: easeOutQuint }
}
