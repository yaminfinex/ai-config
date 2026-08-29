import { useEffect, useRef } from 'react'
import { registerCustomTheme, type ThemeRegistration } from '@pierre/diffs'
import { File, PatchDiff, type SelectedLineRange } from '@pierre/diffs/react'
import themes from './pierre-themes.json'
import { fileLanguage } from './gitViewModel'
import { isLineCentered, MAX_LINE_SCROLL_ATTEMPTS } from './pierreScroll'
import { useThemeType } from '../../shared/themeSignal'

const themePair = { light: themes.light.name, dark: themes.dark.name }

registerCustomTheme(themes.light.name, async () => themes.light as ThemeRegistration)
registerCustomTheme(themes.dark.name, async () => themes.dark as ThemeRegistration)

export function PierreFile({ path, content, selectedLines }: { path: string, content: string, selectedLines: SelectedLineRange | null }) {
  const themeType = useThemeType()
  const scrollAttempts = useRef<{ path: string, content: string, line: number, attempts: number } | undefined>(undefined)
  const scrollFrame = useRef<number | undefined>(undefined)
  useEffect(() => () => { if (scrollFrame.current !== undefined) cancelAnimationFrame(scrollFrame.current) }, [])
  const scrollSelectedLine = (node: HTMLElement) => {
    if (!selectedLines) return
    if (scrollFrame.current !== undefined) cancelAnimationFrame(scrollFrame.current)
    scrollFrame.current = requestAnimationFrame(() => {
      scrollFrame.current = undefined
      const selection = { path, content, line: selectedLines.start }
      const previousAttempts = scrollAttempts.current
      const attempts = previousAttempts?.path === path && previousAttempts.content === content && previousAttempts.line === selectedLines.start
        ? previousAttempts.attempts
        : 0
      if (attempts >= MAX_LINE_SCROLL_ATTEMPTS) return
      const renderRoot = node.shadowRoot ?? node
      const line = renderRoot.querySelector<HTMLElement>(`[data-line="${selectedLines.start}"]`)
      const root = line?.getRootNode()
      const host = root instanceof ShadowRoot ? root.host : line
      const container = host?.closest<HTMLElement>('.file-content')
      if (!line) {
        scrollAttempts.current = { ...selection, attempts: attempts + 1 }
        return
      }
      if (container && isLineCentered(line.getBoundingClientRect(), container.getBoundingClientRect())) {
        return
      }
      line.scrollIntoView({ block: 'center' })
      scrollAttempts.current = { ...selection, attempts: attempts + 1 }
    })
  }
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
      onPostRender: scrollSelectedLine,
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
