import { useEffect, useRef } from 'react'
import { registerCustomTheme, type ThemeRegistration } from '@pierre/diffs'
import { File, PatchDiff, type SelectedLineRange } from '@pierre/diffs/react'
import themes from './pierre-themes.json'
import { fileLanguage } from './gitViewModel'
import { isLineCentered, planLineScroll, type LineScrollState } from './pierreScroll'
import { useThemeType } from '../../shared/themeSignal'

const themePair = { light: themes.light.name, dark: themes.dark.name }

registerCustomTheme(themes.light.name, async () => themes.light as ThemeRegistration)
registerCustomTheme(themes.dark.name, async () => themes.dark as ThemeRegistration)

export function PierreFile({ path, content, selectedLines }: { path: string, content: string, selectedLines: SelectedLineRange | null }) {
  const themeType = useThemeType()
  const scrollState = useRef<LineScrollState | undefined>(undefined)
  const scrollFrame = useRef<number | undefined>(undefined)
  useEffect(() => () => { if (scrollFrame.current !== undefined) cancelAnimationFrame(scrollFrame.current) }, [])
  const scrollSelectedLine = (node: HTMLElement) => {
    if (!selectedLines) return
    if (scrollFrame.current !== undefined) cancelAnimationFrame(scrollFrame.current)
    scrollFrame.current = requestAnimationFrame(() => {
      scrollFrame.current = undefined
      const line = (node.shadowRoot ?? node).querySelector<HTMLElement>(`[data-line="${selectedLines.start}"]`)
      const root = line?.getRootNode()
      const container = (root instanceof ShadowRoot ? root.host : line)?.closest<HTMLElement>('.file-content')
      const observation = !line ? 'missing'
        : container && isLineCentered(line.getBoundingClientRect(), container.getBoundingClientRect()) ? 'centered'
          : 'off-centre'
      const plan = planLineScroll(scrollState.current, { path, content, line: selectedLines.start }, observation)
      scrollState.current = plan.next
      if (plan.scroll) line?.scrollIntoView({ block: 'center' })
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
