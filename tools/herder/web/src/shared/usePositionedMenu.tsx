import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react'
import { menuFocusAction, menuKeyAction } from './menuModel.ts'

type MenuPosition = { x: number, y: number }

// usePositionedMenu is the one lifecycle every context menu shares: it
// clamps the menu into the window, focuses its first item, and dismisses it
// on an outside pointer, a drag, a scroll, focus leaving or Escape.
export function usePositionedMenu() {
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
      const action = menuKeyAction({
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
      if (menuFocusAction(Boolean(menuRef.current?.contains(event.target as Node))) === 'dismiss') close(false)
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
