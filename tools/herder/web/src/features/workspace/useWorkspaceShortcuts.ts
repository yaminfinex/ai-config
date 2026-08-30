import { useEffect, type MutableRefObject } from 'react'
import type { DockviewApi } from 'dockview-react'
import { followScrollCommandEvent, type FollowScrollCommand } from '../../shared/useFollowScroll'
import { bindShellShortcuts } from '../layout/shellShortcuts'

export function useWorkspaceShortcuts({
  apiRef,
  shortcutReference,
  setShortcutReference,
  showQuickOpen,
  closePanel,
}: {
  apiRef: MutableRefObject<DockviewApi | undefined>
  shortcutReference: boolean
  setShortcutReference: (open: boolean) => void
  showQuickOpen: () => void
  closePanel: (id: string) => void
}) {
  useEffect(() => {
    const scrollActivePanel = (command: FollowScrollCommand) => {
      const viewport = document.querySelector('.dv-active-group [data-follow-scroll]')
      if (!viewport) return false
      viewport.dispatchEvent(new CustomEvent(followScrollCommandEvent, { detail: command }))
      return true
    }
    const switchTab = (direction: 'previous' | 'next') => {
      const api = apiRef.current
      if (!api?.activeGroup || api.activeGroup.panels.length === 0) return false
      const panels = api.activeGroup.panels
      const index = panels.findIndex((panel) => panel.id === api.activeGroup?.activePanel?.id)
      panels[(index + (direction === 'next' ? 1 : -1) + panels.length) % panels.length]?.api.setActive()
      return true
    }
    return bindShellShortcuts(window, {
      quickOpen: showQuickOpen,
      closePanel: () => {
        const panel = apiRef.current?.activePanel
        if (!panel) return false
        closePanel(panel.id)
        return true
      },
      openShortcutReference: () => setShortcutReference(true),
      closeShortcutReference: () => {
        if (!shortcutReference) return false
        setShortcutReference(false)
        return true
      },
      switchTab,
      focusFleet: () => {
        const item = document.querySelector<HTMLElement>('.fleet-tree [role="treeitem"]')
        if (!item) return false
        item.focus()
        return true
      },
      focusComposer: () => {
        const composer = document.querySelector<HTMLTextAreaElement>('.dv-active-group textarea[data-composer]')
        if (!composer) return false
        composer.focus()
        return true
      },
      goToTop: () => scrollActivePanel('top'),
      goToBottom: () => scrollActivePanel('bottom'),
      toggleMaximize: () => {
        const group = apiRef.current?.activeGroup
        if (!group) return false
        if (group.api.isMaximized()) group.api.exitMaximized()
        else group.api.maximize()
        return true
      },
    }, navigator.userAgent)
  }, [apiRef, closePanel, setShortcutReference, shortcutReference, showQuickOpen])
}
