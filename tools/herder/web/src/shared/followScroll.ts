export const followBottomThreshold = 48

export type FollowScrollState = {
  following: boolean
  scrollTop: number
}

type ScrollViewport = Pick<HTMLElement, 'scrollHeight' | 'scrollTop' | 'clientHeight'>

export function createFollowScrollState(): FollowScrollState {
  return { following: true, scrollTop: 0 }
}

export function isAtScrollBottom({ scrollHeight, scrollTop, clientHeight }: ScrollViewport) {
  return scrollHeight - scrollTop - clientHeight < followBottomThreshold
}

export function recordFollowScroll(state: FollowScrollState, viewport: ScrollViewport) {
  state.scrollTop = viewport.scrollTop
  state.following = isAtScrollBottom(viewport)
}

export function restoreFollowScroll(state: FollowScrollState, viewport: ScrollViewport) {
  viewport.scrollTop = state.following ? viewport.scrollHeight : state.scrollTop
}

export function resizeFollowScroll(state: FollowScrollState, viewport: ScrollViewport) {
  if (state.following) viewport.scrollTop = viewport.scrollHeight
}
