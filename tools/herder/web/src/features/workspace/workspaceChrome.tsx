import { useEffect, useState } from 'react'
import type { IDockviewHeaderActionsProps, IWatermarkPanelProps } from 'dockview-react'
import { PanelState } from '../../shared/PanelState'
import { shortcutLabels } from '../layout/shellShortcuts'
import { useWorkspaceActionsContext } from './workspaceContext'

export function DockHeaderActions({ group, containerApi }: IDockviewHeaderActionsProps) {
  const [maximized, setMaximized] = useState(group.api.isMaximized())
  useEffect(() => {
    setMaximized(group.api.isMaximized())
    const disposable = containerApi.onDidMaximizedGroupChange(() => setMaximized(group.api.isMaximized()))
    return () => disposable.dispose()
  }, [containerApi, group])
  return <div className="dock-header-actions">
    <button type="button" className="dock-maximize" title={`${maximized ? 'Restore' : 'Maximize'} group · ${shortcutLabels(navigator.userAgent).toggleMaximize}`}
      aria-label={maximized ? 'Restore group' : 'Maximize group'} onClick={() => maximized ? group.api.exitMaximized() : group.api.maximize()}>
      <span aria-hidden="true">{maximized ? '⧉' : '□'}</span>
    </button>
  </div>
}

export function DockWatermark({ containerApi }: IWatermarkPanelProps) {
  const actions = useWorkspaceActionsContext()
  return <PanelState className="dock-watermark" title="No panels open" detail="Open an agent from the fleet sidebar or find a file or folder. Your sidebar and shortcuts are still available."><div>
    <button type="button" onClick={() => actions.showQuickOpen(containerApi.activeGroup?.id)}>Quick Open</button>
    <button type="button" onClick={actions.resetLayout}>Reset layout</button>
  </div></PanelState>
}
