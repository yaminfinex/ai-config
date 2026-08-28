import type { FileTarget } from '../../types'

export type FileViewMode = 'rendered' | 'source'
export type FileTab = FileTarget & { id: string, preview: boolean, viewMode: FileViewMode }

export function isMarkdownPath(path: string) {
  return /\.(?:md|markdown)$/iu.test(path)
}

function initialFileViewMode(target: FileTarget): FileViewMode {
  return isMarkdownPath(target.path) && !target.line ? 'rendered' : 'source'
}

function updateFileTab(tab: FileTab, target: FileTarget): FileTab {
  return { ...tab, ...target, viewMode: target.line ? 'source' : tab.viewMode }
}

export function fileTabID(root: string, path: string) {
  return `file:${encodeURIComponent(root)}:${encodeURIComponent(path)}`
}

export function previewFileTab(tabs: FileTab[], target: FileTarget): FileTab[] {
  const id = fileTabID(target.root, target.path)
  const existing = tabs.find((tab) => tab.id === id)
  if (existing) return tabs.map((tab) => tab.id === id ? updateFileTab(tab, target) : tab)
  const preview = { ...target, id, preview: true, viewMode: initialFileViewMode(target) }
  const index = tabs.findIndex((tab) => tab.preview)
  if (index < 0) return [...tabs, preview]
  const next = [...tabs]
  next[index] = preview
  return next
}

export function pinFileTab(tabs: FileTab[], target: FileTarget): FileTab[] {
  const id = fileTabID(target.root, target.path)
  if (!tabs.some((tab) => tab.id === id)) return [...tabs, { ...target, id, preview: false, viewMode: initialFileViewMode(target) }]
  return tabs.map((tab) => tab.id === id ? { ...updateFileTab(tab, target), preview: false } : tab)
}

export function setFileTabViewMode(tabs: FileTab[], id: string, viewMode: FileViewMode): FileTab[] {
  if (tabs.find((tab) => tab.id === id)?.viewMode === viewMode) return tabs
  return tabs.map((tab) => tab.id === id ? { ...tab, viewMode } : tab)
}

export function closeFileTab(tabs: FileTab[], id: string): FileTab[] {
  return tabs.some((tab) => tab.id === id) ? tabs.filter((tab) => tab.id !== id) : tabs
}
