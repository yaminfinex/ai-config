import { useEffect, useLayoutEffect, useState } from 'react'
import { applyTheme, cycleThemePreference, persistThemePreference, readThemePreference, resolveTheme } from '../theme'

const labels = { system: 'System', light: 'Light', dark: 'Dark' } as const

export function ThemeToggle() {
  const [preference, setPreference] = useState(readThemePreference)
  const [prefersDark, setPrefersDark] = useState(() => window.matchMedia('(prefers-color-scheme: dark)').matches)
  const resolved = resolveTheme(preference, prefersDark)
  const next = cycleThemePreference(preference)

  useLayoutEffect(() => {
    applyTheme(resolved)
  }, [resolved])

  useEffect(() => {
    const media = window.matchMedia('(prefers-color-scheme: dark)')
    const update = (event: MediaQueryListEvent) => setPrefersDark(event.matches)
    setPrefersDark(media.matches)
    media.addEventListener('change', update)
    return () => media.removeEventListener('change', update)
  }, [])

  return <button className="theme-toggle" type="button"
    aria-label={`Theme: ${labels[preference]}. Activate for ${labels[next]}.`}
    title={`Theme: ${labels[preference]} · resolved ${labels[resolved].toLowerCase()} · click for ${labels[next]}`}
    onClick={() => {
      const updated = cycleThemePreference(preference)
      persistThemePreference(updated)
      applyTheme(resolveTheme(updated, prefersDark))
      setPreference(updated)
    }}>
    <span aria-hidden="true">{preference === 'system' ? '◐' : preference === 'light' ? '☀' : '☾'}</span>
    {labels[preference]}
  </button>
}
