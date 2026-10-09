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

// A remounted transcript restores a held position once it has something to
// scroll; before its entries arrive there is nothing to restore into.
export function restoredFollowScroll(state: FollowScrollState, viewport: ScrollViewport) {
  restoreFollowScroll(state, viewport)
  return viewport.scrollHeight > viewport.clientHeight
}

// Follow state outlives a remount: a space switch rebuilds every panel, and a
// transcript comes back where it was left. Closing the panel forgets it.
// Bounded, least recently used first.
const rememberedLimit = 200
const remembered = new Map<string, FollowScrollState>()

export function rememberedFollowScroll(key: string): FollowScrollState {
  const state = remembered.get(key) ?? createFollowScrollState()
  remembered.delete(key)
  remembered.set(key, state)
  for (const oldest of remembered.keys()) {
    if (remembered.size <= rememberedLimit) break
    remembered.delete(oldest)
  }
  return state
}

export function forgetFollowScroll(key: string) {
  remembered.delete(key)
}

export function resizeFollowScroll(state: FollowScrollState, viewport: ScrollViewport) {
  if (state.following) viewport.scrollTop = viewport.scrollHeight
}
