// raiseExistingPanel is the "open an already-open panel" step of openPanel:
// merge the requested params into the panel, activate it, refresh its data,
// sync the dock. Activation is skipped when the panel is already the active
// one. dockview 8.2 answers setActive on the active panel by re-rendering it
// (group.openPanel -> contentContainer.renderPanel with asActive), and
// renderPanel detaches the content element and appends it again; a detached
// scroll container comes back at scrollTop 0, so clicking the row of the
// agent whose transcript is already active jumped the transcript to the top
// (owner bug #244405). Selection and focus are the caller's; nothing else
// about the already-active case changes.
export type ExistingPanel<P> = {
  params: unknown
  api: { updateParameters: (params: P) => void, setActive: () => void }
}

export function raiseExistingPanel<P>(
  panel: ExistingPanel<P>,
  id: string,
  activePanelID: string | undefined,
  requested: P,
  steps: {
    current: (raw: unknown) => P | null | undefined
    merge: (current: P, next: P) => P
    onActiveParamsChanged: (params: P) => void
    invalidate: (params: P) => void
    syncDock: () => void
  },
) {
  const current = steps.current(panel.params)
  const merged = current ? steps.merge(current, requested) : requested
  panel.api.updateParameters(merged)
  if (activePanelID === id) steps.onActiveParamsChanged(merged)
  else panel.api.setActive()
  steps.invalidate(requested)
  steps.syncDock()
  return merged
}
