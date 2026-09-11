import type { LifecycleProblem } from '../../api/client'

export const treeClickGuardSelector = '.tree-disclosure, .launch-agent-button, .rename-agent-button, .rename-agent-input, .group-space-button'

// nodeID is the tree node that started the rename: an agent can appear
// under several group headers, so exactly one occurrence (the initiating
// node) mounts the editor while the annotation is still written by agent
// name and every copy updates from the next frame.
export type RenameState = {
  name: string
  nodeID: string
  currentTitle: string
  value: string
  problem?: LifecycleProblem
}

export function beginRename(name: string, nodeID: string, title?: string): RenameState {
  return { name, nodeID, currentTitle: title ?? '', value: title ?? '' }
}

// editingAt returns the rename state when node is the initiating occurrence,
// else null — the one place that decides which row renders the editor.
export function editingAt(state: RenameState | null, nodeID: string): RenameState | null {
  return state && state.nodeID === nodeID ? state : null
}

export function renameValue(state: RenameState, value: string): RenameState {
  return { ...state, value, problem: undefined }
}

export function prepareRename(state: RenameState): string | null {
  const title = state.value.trim()
  return title && title !== state.currentTitle ? title : null
}
