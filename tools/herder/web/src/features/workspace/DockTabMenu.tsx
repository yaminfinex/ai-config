import { useCallback, useEffect, useLayoutEffect, useRef, useState, type MouseEvent, type RefObject } from 'react'
import { createPortal } from 'react-dom'
import type { DockPanelParams } from '../layout/dockLayout.ts'
import { dockTabMenuItems, dockTabMenuNavigationIndex, isDockTabMenuKey } from './dockTabMenuModel.ts'
import { useWorkspaceActionsContext, useWorkspaceData } from './workspaceContext.tsx'

type MenuPosition = { x: number, y: number }

export function useDockTabMenu(tabRef: RefObject<HTMLDivElement | null>, sourceID: string, params: DockPanelParams) {
  const actions = useWorkspaceActionsContext()
  const data = useWorkspaceData()
  const [position, setPosition] = useState<MenuPosition | null>(null)
  const menuRef = useRef<HTMLDivElement | null>(null)
  const focusReturn = useRef<HTMLElement | null>(null)
  const close = useCallback((restore = true) => {
    setPosition(null)
    if (restore) window.requestAnimationFrame(() => focusReturn.current?.isConnected && focusReturn.current.focus())
  }, [])
  const open = useCallback((next: MenuPosition, returnTo: HTMLElement | null) => {
    focusReturn.current = returnTo
    setPosition({
      x: Math.max(4, Math.min(next.x, window.innerWidth - 224)),
      y: Math.max(4, Math.min(next.y, window.innerHeight - 48)),
    })
  }, [])

  useEffect(() => {
    const tab = tabRef.current?.closest<HTMLElement>('.dv-tab')
    if (!tab) return
    const onKeyDown = (event: KeyboardEvent) => {
      if (!isDockTabMenuKey(event)) return
      event.preventDefault()
      event.stopPropagation()
      const rect = tab.getBoundingClientRect()
      open({ x: rect.left, y: rect.bottom }, tab)
    }
    tab.addEventListener('keydown', onKeyDown)
    return () => tab.removeEventListener('keydown', onKeyDown)
  }, [open, tabRef])

  useEffect(() => {
    if (!position) return
    menuRef.current?.querySelector<HTMLElement>('[role="menuitem"]')?.focus()
    const dismiss = (event: Event) => {
      if (event.type === 'pointerdown' && menuRef.current?.contains(event.target as Node)) return
      close(event.type !== 'pointerdown')
    }
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') { event.preventDefault(); close(); return }
      const items = [...(menuRef.current?.querySelectorAll<HTMLElement>('[role="menuitem"]') ?? [])]
      const next = dockTabMenuNavigationIndex(event.key, items.indexOf(document.activeElement as HTMLElement), items.length)
      if (next === null) return
      event.preventDefault()
      event.stopPropagation()
      items[next]?.focus()
    }
    document.addEventListener('pointerdown', dismiss, true)
    document.addEventListener('dragstart', dismiss, true)
    document.addEventListener('scroll', dismiss, true)
    document.addEventListener('keydown', onKeyDown, true)
    return () => {
      document.removeEventListener('pointerdown', dismiss, true)
      document.removeEventListener('dragstart', dismiss, true)
      document.removeEventListener('scroll', dismiss, true)
      document.removeEventListener('keydown', onKeyDown, true)
    }
  }, [close, position])
  useLayoutEffect(() => {
    if (!position || !menuRef.current) return
    const rect = menuRef.current.getBoundingClientRect()
    const next = {
      x: Math.max(4, Math.min(position.x, window.innerWidth - rect.width - 4)),
      y: Math.max(4, Math.min(position.y, window.innerHeight - rect.height - 4)),
    }
    if (next.x !== position.x || next.y !== position.y) setPosition(next)
  }, [position])

  const onContextMenu = useCallback((event: MouseEvent<HTMLDivElement>) => {
    event.preventDefault()
    event.stopPropagation()
    open({ x: event.clientX, y: event.clientY }, tabRef.current?.closest<HTMLElement>('.dv-tab') ?? null)
  }, [open, tabRef])

  const menu = position ? createPortal(<div ref={menuRef} className="dock-tab-menu" role="menu"
    aria-label="Send pane to space" style={{ left: position.x, top: position.y }}>
    {dockTabMenuItems(data.spaces, data.activeSpaceID).map((item) => <button type="button" role="menuitem" key={`${item.kind}:${item.id}`}
      onClick={() => {
        const sent = item.kind === 'space'
          ? actions.sendPanelToSpace(sourceID, params, item.id)
          : actions.sendPanelToNewSpace(sourceID, params)
        close(!sent)
      }}>{item.label}</button>)}
  </div>, document.body) : null

  return { onContextMenu, menu }
}
