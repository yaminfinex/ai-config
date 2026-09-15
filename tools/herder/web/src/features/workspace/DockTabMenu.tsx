import { useCallback, useEffect, useLayoutEffect, useRef, useState, type MouseEvent, type RefObject } from 'react'
import { createPortal } from 'react-dom'
import type { DockPanelParams } from '../layout/dockLayout.ts'
import { dockTabMenuFocusAction, dockTabMenuItems, dockTabMenuKeyAction, isDockTabMenuKey } from './dockTabMenuModel.ts'
import { useWorkspaceActionsContext, useWorkspaceData } from './workspaceContext.tsx'

type MenuPosition = { x: number, y: number }

function usePositionedMenu() {
  const [position, setPosition] = useState<MenuPosition | null>(null)
  const menuRef = useRef<HTMLDivElement | null>(null)
  const focusReturn = useRef<HTMLElement | null>(null)
  const sourceGuard = useRef(0)
  const close = useCallback((restore = true) => {
    sourceGuard.current += 1
    setPosition(null)
    if (restore) window.requestAnimationFrame(() => focusReturn.current?.isConnected && focusReturn.current.focus())
  }, [])
  const open = useCallback((next: MenuPosition, returnTo: HTMLElement | null) => {
    sourceGuard.current += 1
    focusReturn.current = returnTo
    setPosition({
      x: Math.max(4, Math.min(next.x, window.innerWidth - 224)),
      y: Math.max(4, Math.min(next.y, window.innerHeight - 48)),
    })
  }, [])

  useEffect(() => {
    if (!position) return
    const source = sourceGuard.current
    menuRef.current?.querySelector<HTMLElement>('[role="menuitem"]')?.focus()
    const dismiss = (event: Event) => {
      if (source !== sourceGuard.current) return
      if (event.type === 'pointerdown' && menuRef.current?.contains(event.target as Node)) return
      close(event.type !== 'pointerdown')
    }
    const onKeyDown = (event: KeyboardEvent) => {
      if (source !== sourceGuard.current) return
      const items = [...(menuRef.current?.querySelectorAll<HTMLElement>('[role="menuitem"]') ?? [])]
      const action = dockTabMenuKeyAction({
        key: event.key,
        insideMenu: Boolean(menuRef.current?.contains(event.target as Node)),
        current: items.indexOf(document.activeElement as HTMLElement),
        count: items.length,
      })
      if (!action) return
      if (action.kind === 'dismiss') { close(false); return }
      event.preventDefault()
      if (action.kind === 'close') { close(); return }
      event.stopPropagation()
      items[action.index]?.focus()
    }
    // Focus landing anywhere outside the menu (the quick-open palette taking its input, for one)
    // closes it without restoring focus, so the new owner keeps it.
    const onFocusIn = (event: FocusEvent) => {
      if (source !== sourceGuard.current) return
      if (dockTabMenuFocusAction(Boolean(menuRef.current?.contains(event.target as Node))) === 'dismiss') close(false)
    }
    document.addEventListener('focusin', onFocusIn, true)
    document.addEventListener('pointerdown', dismiss, true)
    document.addEventListener('dragstart', dismiss, true)
    document.addEventListener('scroll', dismiss, true)
    document.addEventListener('keydown', onKeyDown, true)
    return () => {
      document.removeEventListener('focusin', onFocusIn, true)
      document.removeEventListener('pointerdown', dismiss, true)
      document.removeEventListener('dragstart', dismiss, true)
      document.removeEventListener('scroll', dismiss, true)
      document.removeEventListener('keydown', onKeyDown, true)
      if (source === sourceGuard.current) sourceGuard.current += 1
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

  return { position, menuRef, open, close }
}

export function useDockTabMenu(tabRef: RefObject<HTMLDivElement | null>, sourceID: string, params: DockPanelParams) {
  const actions = useWorkspaceActionsContext()
  const data = useWorkspaceData()
  const { position, menuRef, open, close } = usePositionedMenu()

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

  const onContextMenu = useCallback((event: MouseEvent<HTMLDivElement>) => {
    event.preventDefault()
    event.stopPropagation()
    open({ x: event.clientX, y: event.clientY }, tabRef.current?.closest<HTMLElement>('.dv-tab') ?? null)
  }, [open, tabRef])

  const menu = position ? createPortal(<div ref={menuRef} className="dock-tab-menu" role="menu"
    aria-label="Send pane to space" style={{ left: position.x, top: position.y }}>
    {dockTabMenuItems(data.spaces, data.activeSpaceID, params.kind === 'agent' ? params.name : undefined).map((item) => <button type="button" role="menuitem" key={`${item.kind}:${item.id}`}
      onClick={() => {
        if (item.kind === 'reassign') {
          close(false)
          actions.showQuickOpen(undefined, { kind: 'reassign', subject: item.subject })
          return
        }
        const sent = item.kind === 'space' ? actions.sendPanelToSpace(sourceID, params, item.id) : actions.sendPanelToNewSpace(sourceID, params)
        close(!sent)
      }}>{item.label}</button>)}
  </div>, document.body) : null

  return { onContextMenu, menu }
}

export function useAgentRowMenu() {
  const actions = useWorkspaceActionsContext()
  const [subject, setSubject] = useState('')
  const positioned = usePositionedMenu()
  const open = useCallback((event: MouseEvent<HTMLElement>, nextSubject: string) => {
    event.preventDefault()
    event.stopPropagation()
    setSubject(nextSubject)
    positioned.open({ x: event.clientX, y: event.clientY }, event.currentTarget)
  }, [positioned.open])

  const menu = positioned.position ? createPortal(<div ref={positioned.menuRef} className="dock-tab-menu" role="menu" aria-label={`Actions for ${subject}`}
    style={{ left: positioned.position.x, top: positioned.position.y }}>
    <button type="button" role="menuitem" onClick={() => {
      positioned.close(false)
      actions.showQuickOpen(undefined, { kind: 'reassign', subject })
    }}>Reassign…</button>
  </div>, document.body) : null
  return { open, menu }
}
