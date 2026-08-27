export type BannerState = { key: string, dismissed: boolean }
export type BannerAction = { type: 'dismiss' } | { type: 'sync', key: string }

export function bannerState(state: BannerState, action: BannerAction): BannerState {
  if (action.type === 'dismiss') return { ...state, dismissed: true }
  if (action.key === state.key) return state
  return { key: action.key, dismissed: false }
}
