export function menuNavigationIndex(key: string, current: number, count: number) {
  if (count <= 0) return null
  if (key === 'ArrowDown') return (Math.max(current, -1) + 1) % count
  if (key === 'ArrowUp') return current <= 0 ? count - 1 : current - 1
  if (key === 'Home') return 0
  if (key === 'End') return count - 1
  return null
}

export type MenuKeyAction = { kind: 'dismiss' } | { kind: 'close' } | { kind: 'focus', index: number }

// A key whose target lives outside the menu belongs to whoever has focus (e.g. the quick-open
// palette): the menu dismisses itself and never touches the event.
export function menuKeyAction({ key, insideMenu, current, count }: { key: string, insideMenu: boolean, current: number, count: number }): MenuKeyAction | null {
  if (!insideMenu) return { kind: 'dismiss' }
  if (key === 'Escape') return { kind: 'close' }
  const next = menuNavigationIndex(key, current, count)
  return next === null ? null : { kind: 'focus', index: next }
}

// Focus is only a dismissal when it lands outside the menu; focus arriving on a menu item (the menu
// focusing its first item on open, or the user tabbing between items) keeps it open. A freshly opened
// menu whose tab keeps focus raises no focusin at all, so it stays open by construction.
export function menuFocusAction(insideMenu: boolean): 'keep' | 'dismiss' {
  return insideMenu ? 'keep' : 'dismiss'
}
