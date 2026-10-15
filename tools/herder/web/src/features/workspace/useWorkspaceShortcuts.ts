import { useEffect, type MutableRefObject } from 'react'
import type { DockviewApi } from 'dockview-react'
import { followScrollCommandEvent, type FollowScrollCommand } from '../../shared/useFollowScroll'
import { panelParams } from '../layout/dockLayout'
import { bindShellShortcuts } from '../layout/shellShortcuts'
import type { Arrive } from './useArrive'

export function useWorkspaceShortcuts({
  apiRef,
  docked,
  shortcutReference,
  setShortcutReference,
  showQuickOpen,
  closePanel,
  toggleNotesRail,
  stepSpace,
  toggleRead,
  arrive,
}: {
  apiRef: MutableRefObject<DockviewApi | undefined>
  // docked answers whether a dock is on screen: on the lens the dock keys are inert.
  docked: () => boolean
  shortcutReference: boolean
  setShortcutReference: (open: boolean) => void
  showQuickOpen: () => void
  closePanel: (id: string) => void
  toggleNotesRail: () => void
  // ⇧⌥←/→ in a zoom: the previous or next space in lens order.
  stepSpace: (by: -1 | 1) => boolean
  toggleRead: (name: string) => void
  // ⌥←/→ arrives at the tab it shows, as a click on it does.
  arrive: Arrive
}) {
  useEffect(() => {
    const scrollActivePanel = (command: FollowScrollCommand) => {
      if (!docked()) return false
      const viewport = document.querySelector('.dv-active-group [data-follow-scroll]')
      if (!viewport) return false
      viewport.dispatchEvent(new CustomEvent(followScrollCommandEvent, { detail: command }))
      return true
    }
    const switchTab = (direction: 'previous' | 'next') => {
      const api = apiRef.current
      if (!docked() || !api?.activeGroup || api.activeGroup.panels.length === 0) return false
      const panels = api.activeGroup.panels
      const index = panels.findIndex((panel) => panel.id === api.activeGroup?.activePanel?.id)
      panels[(index + (direction === 'next' ? 1 : -1) + panels.length) % panels.length]?.api.setActive()
      arrive()
      return true
    }
    return bindShellShortcuts(window, {
      quickOpen: showQuickOpen,
      closePanel: () => {
        const panel = apiRef.current?.activePanel
        if (!docked() || !panel) return false
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
      switchSpace: (direction) => stepSpace(direction === 'next' ? 1 : -1),
      focusFleet: () => {
        if (!docked()) return false
        const item = document.querySelector<HTMLElement>('.fleet-tree [role="treeitem"]')
        if (!item) return false
        item.focus()
        return true
      },
      toggleNotesRail,
      focusComposer: () => {
        if (!docked()) return false
        const composer = document.querySelector<HTMLTextAreaElement>('.dv-active-group textarea[data-composer]')
        if (!composer) return false
        composer.focus()
        return true
      },
      goToTop: () => scrollActivePanel('top'),
      goToBottom: () => scrollActivePanel('bottom'),
      toggleMaximize: () => {
        const group = apiRef.current?.activeGroup
        if (!docked() || !group) return false
        if (group.api.isMaximized()) group.api.exitMaximized()
        else group.api.maximize()
        return true
      },
      toggleRead: () => {
        const params = panelParams(apiRef.current?.activePanel?.params)
        if (!docked() || params?.kind !== 'agent') return false
        toggleRead(params.name)
        return true
      },
    }, navigator.userAgent)
  }, [apiRef, arrive, closePanel, docked, setShortcutReference, shortcutReference, showQuickOpen, stepSpace, toggleNotesRail, toggleRead])
}
