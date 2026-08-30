import { routeFromLocation, type Route } from '../features/layout/historyModel'

export function navigate(path: string) {
  window.history.pushState({}, '', path)
  window.dispatchEvent(new PopStateEvent('popstate'))
}

export function AppLink({ to, className, children }: { to: string, className?: string, children: React.ReactNode }) {
  return <a href={to} className={className} onClick={(event) => {
    if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return
    event.preventDefault()
    navigate(to)
  }}>{children}</a>
}

export function currentRoute(): Route {
  return routeFromLocation(window.location.pathname, window.location.search)
}

export type { Route }
