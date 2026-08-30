import type { DockviewApi } from 'dockview-react'

type Listener<T> = T extends (listener: infer L) => unknown ? L : never

export function subscribeToDock(api: DockviewApi, handlers: {
  layout: Listener<DockviewApi['onDidLayoutChange']>
  activePanel: Listener<DockviewApi['onDidActivePanelChange']>
  removePanel: Listener<DockviewApi['onDidRemovePanel']>
  movePanel: Listener<DockviewApi['onDidMovePanel']>
}) {
  const disposables = [
    api.onDidLayoutChange(handlers.layout),
    api.onDidActivePanelChange(handlers.activePanel),
    api.onDidRemovePanel(handlers.removePanel),
    api.onDidMovePanel(handlers.movePanel),
  ]
  return () => disposables.forEach((disposable) => disposable.dispose())
}
