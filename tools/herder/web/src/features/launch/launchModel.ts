import type { Refusal } from '../../types.ts'

export type LaunchTool = 'claude' | 'codex'

export type LaunchFormState = {
  tool: LaunchTool
  model: string
  modelOptions: string[]
  tag: string
  repo: string
  branch: string
  branchHelp: string
}

const models: Record<LaunchTool, string[]> = {
  claude: ['opus', 'sonnet', 'haiku'],
  codex: ['gpt-5.4', 'gpt-5.4-mini'],
}

export function initialLaunchForm(tool: LaunchTool = 'claude'): LaunchFormState {
  return {
    tool,
    model: tool === 'codex' ? '' : models[tool][0],
    modelOptions: [...models[tool]],
    tag: 'impl',
    repo: '',
    branch: '',
    branchHelp: 'A fresh worktree is created for the agent.',
  }
}

export function changeLaunchTool(current: LaunchFormState, tool: LaunchTool): LaunchFormState {
  const defaults = initialLaunchForm(tool)
  return { ...current, tool, model: defaults.model, modelOptions: defaults.modelOptions }
}

export function launchRequest(form: LaunchFormState) {
  return {
    tool: form.tool,
    model: form.model.trim(),
    tag: form.tag.trim(),
    repo: form.repo.trim(),
    branch: form.branch.trim(),
  }
}

export function launchRefusal(problem: Refusal) {
  return problem.detail
}

export function launchConfirmation(names: string[]) {
  const first = names[0]
  return {
    line: names.length === 1 ? `Launched ${first}.` : `Launched ${names.join(', ')}.`,
    action: first ? { label: 'Open in this space', agent: first } : null,
  }
}
