import type { LifecycleProblem } from '../../api/client'

export const treeClickGuardSelector = '.tree-disclosure, .launch-agent-button, .rename-agent-button, .rename-agent-input'

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

export function prepareRename(state: RenameState): { title?: string } {
  const title = state.value.trim()
  return title && title !== state.currentTitle ? { title } : {}
}

export function renameRefused(state: RenameState, problem: LifecycleProblem): RenameState {
  return { ...state, problem }
}

export function cancelRename(): null {
  return null
}
