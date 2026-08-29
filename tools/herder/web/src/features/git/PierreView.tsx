import { useEffect, useState } from 'react'
import { registerCustomTheme, type ThemeRegistration } from '@pierre/diffs'
import { File, PatchDiff, type SelectedLineRange } from '@pierre/diffs/react'
import themes from './pierre-themes.json'
import { fileLanguage } from './gitViewModel'

const themePair = { light: themes.light.name, dark: themes.dark.name }

registerCustomTheme(themes.light.name, async () => themes.light as ThemeRegistration)
registerCustomTheme(themes.dark.name, async () => themes.dark as ThemeRegistration)

function currentTheme() {
  return document.documentElement.dataset.theme === 'light' ? 'light' : 'dark'
}

function useThemeType() {
  const [themeType, setThemeType] = useState<'light' | 'dark'>(currentTheme)
  useEffect(() => {
    const observer = new MutationObserver(() => setThemeType(currentTheme()))
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ['data-theme'] })
    return () => observer.disconnect()
  }, [])
  return themeType
}

function scrollSelectedLine(node: HTMLElement, selectedLines: SelectedLineRange | null) {
  if (!selectedLines) return
  requestAnimationFrame(() => node.querySelector<HTMLElement>(`[data-line="${selectedLines.start}"]`)?.scrollIntoView({ block: 'center' }))
}

export function PierreFile({ path, content, selectedLines }: { path: string, content: string, selectedLines: SelectedLineRange | null }) {
  const themeType = useThemeType()
  return <File
    file={{ name: path, contents: content, lang: fileLanguage(path) }}
    selectedLines={selectedLines}
    disableWorkerPool
    options={{
      theme: themePair,
      themeType,
      preferredHighlighter: 'shiki-js',
      disableFileHeader: true,
      overflow: 'scroll',
      onPostRender: (node) => scrollSelectedLine(node, selectedLines),
    }}
  />
}

export function PierrePatch({ patch, selectedLines }: { patch: string, selectedLines?: SelectedLineRange | null }) {
  const themeType = useThemeType()
  return <PatchDiff
    patch={patch}
    selectedLines={selectedLines}
    disableWorkerPool
    options={{
      theme: themePair,
      themeType,
      preferredHighlighter: 'shiki-js',
      disableFileHeader: true,
      diffStyle: 'unified',
      overflow: 'scroll',
    }}
  />
}
