import type { Refusal } from '../../types.ts'

export type LaunchTool = 'claude' | 'codex'

export type LaunchFormState = {
  tool: LaunchTool
  model: string
  modelOptions: string[]
  effort: string
  effortOptions: string[]
  tag: string
}

const models: Record<LaunchTool, string[]> = {
  claude: ['claude-opus-5-5', 'claude-fable-5-1'],
  codex: ['gpt-6-astra'],
}

const efforts: Record<LaunchTool, string[]> = {
  claude: ['low', 'medium', 'high', 'xhigh', 'max'],
  codex: ['low', 'medium', 'high', 'xhigh'],
}

const defaultEfforts: Record<LaunchTool, string> = {
  claude: 'medium',
  codex: '',
}

const modelLabels: Record<string, string> = {
  'claude-opus-5-5': 'Opus 5.5',
  'claude-fable-5-1': 'Fable 5.1',
  'gpt-6-astra': 'GPT-6 Astra',
}

export function launchModelLabel(model: string) {
  return modelLabels[model] ?? model
}

export function initialLaunchForm(tool: LaunchTool = 'claude'): LaunchFormState {
  return {
    tool,
    model: models[tool][0],
    modelOptions: [...models[tool]],
    effort: defaultEfforts[tool],
    effortOptions: [...efforts[tool]],
    tag: 'impl',
  }
}

export function changeLaunchTool(current: LaunchFormState, tool: LaunchTool): LaunchFormState {
  const defaults = initialLaunchForm(tool)
  return { ...current, tool, model: defaults.model, modelOptions: defaults.modelOptions, effort: defaults.effort, effortOptions: defaults.effortOptions }
}

export function launchRequest(form: LaunchFormState, workspace: string) {
  const effort = form.effort.trim()
  return {
    tool: form.tool,
    model: form.model.trim(),
    ...(effort ? { effort } : {}),
    tag: form.tag.trim(),
    workspace,
  }
}

export function launchRefusal(problem: Refusal) {
  return problem.detail
}

export function launchConfirmation(names: string[], workspace: string, pane: string) {
  const first = names[0]
  return {
    line: `${names.length === 1 ? `Launched ${first}` : `Launched ${names.join(', ')}`} in ${workspace} · pane ${pane}.`,
    taskLine: 'It has no task yet — send it one from its panel.',
    action: first ? { label: 'Open in this space', agent: first } : null,
  }
}

export function dialogTabTargetIndex(activeIndex: number, itemCount: number, reverse: boolean) {
  if (itemCount < 1) return null
  if (reverse && activeIndex <= 0) return itemCount - 1
  if (!reverse && (activeIndex < 0 || activeIndex >= itemCount - 1)) return 0
  return null
}
