import { useLayoutEffect, useRef, useState } from 'react'
import type { UIEventHandler } from 'react'
import { isAtScrollBottom } from './followScroll'

export function useFollowScroll<T extends HTMLElement>(contentVersion: unknown, presentationVersion?: unknown) {
  const viewportRef = useRef<T>(null)
  const followingRef = useRef(true)
  const [following, setFollowing] = useState(true)

  useLayoutEffect(() => {
    if (!followingRef.current) return
    const viewport = viewportRef.current
    if (viewport) viewport.scrollTop = viewport.scrollHeight
  }, [contentVersion, presentationVersion])

  const onScroll: UIEventHandler<T> = (event) => {
    const atBottom = isAtScrollBottom(event.currentTarget)
    followingRef.current = atBottom
    setFollowing(atBottom)
  }

  const jumpToBottom = () => {
    const viewport = viewportRef.current
    if (viewport) viewport.scrollTop = viewport.scrollHeight
    followingRef.current = true
    setFollowing(true)
  }

  return { viewportRef, following, onScroll, jumpToBottom }
}

export function JumpToBottomButton({ visible, onJump }: { visible: boolean, onJump: () => void }) {
  if (!visible) return null
  return <button type="button" className="jump-to-bottom" onClick={onJump}>↓ Jump to bottom</button>
}
