import type { FileTarget } from '../../types'

export type FileTab = FileTarget & { id: string, preview: boolean }

export function fileTabID(root: string, path: string) {
  return `file:${encodeURIComponent(root)}:${encodeURIComponent(path)}`
}

export function previewFileTab(tabs: FileTab[], target: FileTarget): FileTab[] {
  const id = fileTabID(target.root, target.path)
  const existing = tabs.find((tab) => tab.id === id)
  if (existing) return tabs.map((tab) => tab.id === id ? { ...tab, ...target } : tab)
  const preview = { ...target, id, preview: true }
  const index = tabs.findIndex((tab) => tab.preview)
  if (index < 0) return [...tabs, preview]
  const next = [...tabs]
  next[index] = preview
  return next
}

export function pinFileTab(tabs: FileTab[], target: FileTarget): FileTab[] {
  const id = fileTabID(target.root, target.path)
  if (!tabs.some((tab) => tab.id === id)) return [...tabs, { ...target, id, preview: false }]
  return tabs.map((tab) => tab.id === id ? { ...tab, ...target, preview: false } : tab)
}
