type DockTabHistoryKeyEvent = Pick<KeyboardEvent, 'key' | 'metaKey' | 'ctrlKey' | 'target' | 'stopPropagation'>

export function preserveDockTabBrowserHistory(event: DockTabHistoryKeyEvent) {
  const target = event.target as { closest?: (selector: string) => unknown } | null
  if ((event.metaKey || event.ctrlKey) && (event.key === 'ArrowLeft' || event.key === 'ArrowRight') && target?.closest?.('.dv-tab')) {
    event.stopPropagation()
  }
}
