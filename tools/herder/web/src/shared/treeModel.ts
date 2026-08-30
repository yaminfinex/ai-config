export const treeIndentInset = 5
export const treeIndentStep = 16

export function treeIndent(depth: number) {
  return treeIndentInset + Math.max(0, depth) * treeIndentStep
}

export type TreeKeyIntent = 'first' | 'last' | 'previous' | 'next' | 'expand' | 'child' | 'collapse' | 'parent' | 'primary'

export function treeKeyIntent(key: string, expandable: boolean, expanded: boolean): TreeKeyIntent | null {
  if (key === 'Home') return 'first'
  if (key === 'End') return 'last'
  if (key === 'ArrowUp') return 'previous'
  if (key === 'ArrowDown') return 'next'
  if (key === 'ArrowRight' && expandable) return expanded ? 'child' : 'expand'
  if (key === 'ArrowLeft') return expandable && expanded ? 'collapse' : 'parent'
  if (key === 'Enter' || key === ' ') return 'primary'
  return null
}

export function treeParentIndex(levels: number[], index: number) {
  const level = levels[index]
  if (level === undefined) return -1
  for (let candidate = index - 1; candidate >= 0; candidate--) {
    if ((levels[candidate] ?? level) < level) return candidate
  }
  return -1
}

export function treeChildIndex(levels: number[], index: number) {
  const level = levels[index]
  return level !== undefined && (levels[index + 1] ?? level) > level ? index + 1 : -1
}
