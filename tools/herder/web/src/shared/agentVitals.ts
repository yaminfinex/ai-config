import type { AgentDetail } from '../types.ts'

function tokenCount(value: number) {
  if (value < 1_000) return `${value} tokens`
  return `${Math.round(value / 1_000)}k tokens`
}

export function agentVitalsPresentation(agent: Pick<AgentDetail, 'model' | 'context_usage'>): string[] {
  const segments: string[] = []
  if (agent.model) segments.push(agent.model)
  if (agent.context_usage) {
    let context = tokenCount(agent.context_usage.used_tokens)
    if (agent.context_usage.window_tokens !== undefined && agent.context_usage.used_percent !== undefined) {
      const left = Math.round(Math.max(0, Math.min(100, 100 - agent.context_usage.used_percent)))
      context += ` · ${left}% left`
    }
    segments.push(context)
  }
  return segments
}
