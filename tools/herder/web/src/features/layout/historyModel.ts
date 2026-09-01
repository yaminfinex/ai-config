import type { DockPanelParams } from './dockLayout.ts'
import {
  panelID,
  panelParamsFromHistorySubject,
  panelParamsFromRouteLocation,
  panelRoutePath,
  panelRouteSubject,
} from '../workspace/panelRegistryModel.ts'

export type Route = { page: 'shell' } | { page: 'panel', params: DockPanelParams } | { page: 'missing' }
export type HistoryCause = 'activation' | 'merge' | 'stamp' | 'replay'
export const layoutRouteState = { herderLayoutRoute: true } as const
export const spaceQueryParam = 'space'

type LayoutRouteState = typeof layoutRouteState & { subject?: unknown }

export function isLayoutRouteState(value: unknown): value is LayoutRouteState {
  return typeof value === 'object' && value !== null &&
    'herderLayoutRoute' in value && value.herderLayoutRoute === true
}

export function spaceIDFromSearch(search: string) {
  return new URLSearchParams(search).get(spaceQueryParam)
}

export function pathWithSpace(path: string, spaceID: string | null) {
  const url = new URL(path, 'http://herder.invalid')
  if (spaceID) url.searchParams.set(spaceQueryParam, spaceID)
  else url.searchParams.delete(spaceQueryParam)
  return `${url.pathname}${url.search}`
}

function pathWithoutSpace(pathname: string, search: string) {
  return pathWithSpace(`${pathname}${search}`, null)
}

export function historyEntryForPanel(params?: DockPanelParams, spaceID: string | null = null) {
  return {
    path: pathWithSpace(params ? panelRoutePath(params) : '/', spaceID),
    state: params ? { ...layoutRouteState, subject: panelRouteSubject(params) } : layoutRouteState,
  }
}

export function routeFromLocation(pathname: string, search: string): Route {
  const params = panelParamsFromRouteLocation(pathname, search)
  if (params) return { page: 'panel', params }
  if (params === null || pathname !== '/') return { page: 'missing' }
  return { page: 'shell' }
}

export function routeFromHistory(pathname: string, search: string, state: unknown): Route {
  if (isLayoutRouteState(state) && 'subject' in state) {
    const params = panelParamsFromHistorySubject(state.subject)
    if (params && panelRoutePath(params) === pathWithoutSpace(pathname, search)) return { page: 'panel', params }
  }
  return routeFromLocation(pathname, search)
}

export function shouldReplayInitialRoute(route: Exclude<Route, { page: 'missing' }>, historyState: unknown, restored: boolean) {
  return route.page === 'panel' && (!restored || !isLayoutRouteState(historyState))
}

export function decideHistoryUpdate(currentState: unknown, next: DockPanelParams | undefined, cause: HistoryCause, suppressed: boolean, spaceID: string | null = null) {
  const entry = historyEntryForPanel(next, spaceID)
  const current = isLayoutRouteState(currentState) && 'subject' in currentState
    ? panelParamsFromHistorySubject(currentState.subject)
    : null
  const distinctSubject = next ? !current || panelID(current) !== panelID(next) : Boolean(current)
  const method = cause === 'activation' && !suppressed && distinctSubject ? 'push' : 'replace'
  return { method, entry } as const
}

export function createHistorySuppressor(
  scheduleFrame: (callback: () => void) => number,
  cancelFrame: (handle: number) => void,
) {
  let depth = 0
  const pending = new Set<number>()
  return {
    active: () => depth > 0,
    run<T>(operation: () => T) {
      depth += 1
      try {
        return operation()
      } finally {
        const handle = scheduleFrame(() => {
          pending.delete(handle)
          depth = Math.max(0, depth - 1)
        })
        pending.add(handle)
      }
    },
    dispose() {
      pending.forEach((handle) => cancelFrame(handle))
      pending.clear()
      depth = 0
    },
  }
}
