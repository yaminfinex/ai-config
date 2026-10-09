import { useLayoutEffect, useRef, useState } from 'react'
import type { UIEventHandler } from 'react'
import { createFollowScrollState, recordFollowScroll, rememberedFollowScroll, resizeFollowScroll, restoredFollowScroll, restoreFollowScroll } from './followScroll'
import { useDOMEvent, useSizeObserver } from './lifecycle'

export const followScrollCommandEvent = 'herder:follow-scroll-command'
export type FollowScrollCommand = 'top' | 'bottom'

// memoryKey keeps the follow state across a remount; without one it starts
// following the bottom.
export function useFollowScroll<T extends HTMLElement>(contentVersion: unknown, presentationVersion?: unknown, active = true, memoryKey?: string) {
  const viewportRef = useRef<T>(null)
  const [initial] = useState(() => memoryKey === undefined ? createFollowScrollState() : rememberedFollowScroll(memoryKey))
  const followingRef = useRef(initial)
  const restorePendingRef = useRef(!initial.following)
  const [following, setFollowing] = useState(initial.following)

  useLayoutEffect(() => {
    if (!active) return
    const viewport = viewportRef.current
    if (!viewport) return
    if (followingRef.current.following) resizeFollowScroll(followingRef.current, viewport)
    else if (restorePendingRef.current) restorePendingRef.current = !restoredFollowScroll(followingRef.current, viewport)
  }, [active, contentVersion, presentationVersion])

  useLayoutEffect(() => {
    if (!active) return
    const viewport = viewportRef.current
    if (viewport) restoreFollowScroll(followingRef.current, viewport)
  }, [active])

  useSizeObserver(viewportRef, (viewport) => resizeFollowScroll(followingRef.current, viewport), active)

  const onScroll: UIEventHandler<T> = (event) => {
    recordFollowScroll(followingRef.current, event.currentTarget)
    setFollowing(followingRef.current.following)
  }

  const jumpToBottom = () => {
    const viewport = viewportRef.current
    if (viewport) viewport.scrollTop = viewport.scrollHeight
    followingRef.current.following = true
    setFollowing(true)
  }

  const jumpToTop = () => {
    const viewport = viewportRef.current
    if (viewport) viewport.scrollTop = 0
    followingRef.current.scrollTop = 0
    followingRef.current.following = false
    setFollowing(false)
  }

  useDOMEvent<CustomEvent<FollowScrollCommand>>(viewportRef, followScrollCommandEvent, (event) => {
    if (event.detail === 'top') jumpToTop()
    else if (event.detail === 'bottom') jumpToBottom()
  }, undefined, active)

  return { viewportRef, following, onScroll, jumpToTop, jumpToBottom }
}

// Top navigation is shortcut-only (Alt+ArrowUp). A dedicated button was tried
// and removed: it floated over transcript content and was rarely useful.
export function ScrollJumpButtons({ bottomVisible, onBottom }: { bottomVisible: boolean, onBottom: () => void }) {
  if (!bottomVisible) return null
  return <div className="scroll-jump-buttons">
    <button type="button" onClick={onBottom}>↓ Jump to bottom</button>
  </div>
}
