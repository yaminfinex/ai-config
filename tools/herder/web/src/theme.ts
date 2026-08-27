export type ThemePreference = 'system' | 'light' | 'dark'
export type ResolvedTheme = Exclude<ThemePreference, 'system'>

type ThemeStorage = Pick<Storage, 'getItem' | 'setItem' | 'removeItem'>
type ThemeRoot = Pick<HTMLElement, 'setAttribute'>

export const themeStorageKey = 'herder.web.theme.v1'

export function readThemePreference(storage: Pick<ThemeStorage, 'getItem'> | null = browserStorage()): ThemePreference {
  try {
    const stored = storage?.getItem(themeStorageKey)
    return stored === 'light' || stored === 'dark' ? stored : 'system'
  } catch {
    return 'system'
  }
}

export function persistThemePreference(preference: ThemePreference, storage: Pick<ThemeStorage, 'setItem' | 'removeItem'> | null = browserStorage()) {
  try {
    if (!storage) return
    if (preference === 'system') storage.removeItem(themeStorageKey)
    else storage.setItem(themeStorageKey, preference)
  } catch {
    // Theme persistence is best-effort when browser storage is unavailable.
  }
}

export function resolveTheme(preference: ThemePreference, prefersDark: boolean): ResolvedTheme {
  return preference === 'system' ? prefersDark ? 'dark' : 'light' : preference
}

export function cycleThemePreference(preference: ThemePreference): ThemePreference {
  return preference === 'system' ? 'light' : preference === 'light' ? 'dark' : 'system'
}

export function applyTheme(theme: ResolvedTheme, root: ThemeRoot = document.documentElement) {
  root.setAttribute('data-theme', theme)
}

function browserStorage(): ThemeStorage | null {
  try {
    return window.localStorage
  } catch {
    return null
  }
}
