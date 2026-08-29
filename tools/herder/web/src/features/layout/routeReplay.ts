import type { Route } from '../../shared/navigation'

export const layoutRouteState = { herderLayoutRoute: true } as const

function isLayoutRouteState(value: unknown) {
  return typeof value === 'object' && value !== null &&
    'herderLayoutRoute' in value && value.herderLayoutRoute === true
}

export function shouldReplayInitialRoute(route: Exclude<Route, { page: 'missing' }>, historyState: unknown, restored: boolean) {
  return route.page === 'agent' && (!restored || !isLayoutRouteState(historyState))
}
