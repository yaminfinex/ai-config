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

export type Route = { page: 'shell' } | { page: 'agent', name: string } | { page: 'missing' }

export function currentRoute(): Route {
  const match = window.location.pathname.match(/^\/agents\/([^/]+)\/?$/)
  if (match) {
    try {
      return { page: 'agent', name: decodeURIComponent(match[1]) }
    } catch {
      return { page: 'missing' }
    }
  }
  return window.location.pathname === '/' ? { page: 'shell' } : { page: 'missing' }
}
