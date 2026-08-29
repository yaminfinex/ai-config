import { useSyncExternalStore } from 'react'

export type ThemeType = 'light' | 'dark'

export function themeType(root: Pick<HTMLElement, 'dataset'> = document.documentElement): ThemeType {
  return root.dataset.theme === 'light' ? 'light' : 'dark'
}

const subscribers = new Set<() => void>()
let observer: MutationObserver | undefined

function subscribe(onChange: () => void) {
  subscribers.add(onChange)
  if (!observer) {
    observer = new MutationObserver(() => subscribers.forEach((notify) => notify()))
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ['data-theme'] })
  }
  return () => {
    subscribers.delete(onChange)
    if (subscribers.size === 0) {
      observer?.disconnect()
      observer = undefined
    }
  }
}

export function useThemeType(): ThemeType {
  return useSyncExternalStore<ThemeType>(subscribe, () => themeType(), () => 'dark')
}
