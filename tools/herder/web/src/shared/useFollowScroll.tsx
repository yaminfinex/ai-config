import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import type { UIEventHandler } from 'react'
import { createFollowScrollState, recordFollowScroll, resizeFollowScroll, restoreFollowScroll } from './followScroll'

export const followScrollCommandEvent = 'herder:follow-scroll-command'
export type FollowScrollCommand = 'top' | 'bottom'

export function useFollowScroll<T extends HTMLElement>(contentVersion: unknown, presentationVersion?: unknown, active = true) {
  const viewportRef = useRef<T>(null)
  const followingRef = useRef(createFollowScrollState())
  const [following, setFollowing] = useState(true)

  useLayoutEffect(() => {
    if (!active || !followingRef.current.following) return
    const viewport = viewportRef.current
    if (viewport) resizeFollowScroll(followingRef.current, viewport)
  }, [active, contentVersion, presentationVersion])

  useLayoutEffect(() => {
    if (!active) return
    const viewport = viewportRef.current
    if (viewport) restoreFollowScroll(followingRef.current, viewport)
  }, [active])

  useLayoutEffect(() => {
    if (!active || typeof ResizeObserver === 'undefined') return
    const viewport = viewportRef.current
    if (!viewport) return
    const observer = new ResizeObserver(() => resizeFollowScroll(followingRef.current, viewport))
    observer.observe(viewport)
    return () => observer.disconnect()
  }, [active])

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

  useEffect(() => {
    if (!active) return
    const viewport = viewportRef.current
    if (!viewport) return
    const onCommand = (event: Event) => {
      if ((event as CustomEvent<FollowScrollCommand>).detail === 'top') jumpToTop()
      else if ((event as CustomEvent<FollowScrollCommand>).detail === 'bottom') jumpToBottom()
    }
    viewport.addEventListener(followScrollCommandEvent, onCommand)
    return () => viewport.removeEventListener(followScrollCommandEvent, onCommand)
  }, [active])

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
