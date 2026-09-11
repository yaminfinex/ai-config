import type { LifecycleProblem } from '../../api/client'

export const treeClickGuardSelector = '.tree-disclosure, .launch-agent-button, .rename-agent-button, .rename-agent-input, .group-space-button'

export type RenameState = {
  name: string
  currentTitle: string
  value: string
  problem?: LifecycleProblem
}

export function beginRename(name: string, title?: string): RenameState {
  return { name, currentTitle: title ?? '', value: title ?? '' }
}

export function renameValue(state: RenameState, value: string): RenameState {
  return { ...state, value, problem: undefined }
}

export function prepareRename(state: RenameState): string | null {
  const title = state.value.trim()
  return title && title !== state.currentTitle ? title : null
}
