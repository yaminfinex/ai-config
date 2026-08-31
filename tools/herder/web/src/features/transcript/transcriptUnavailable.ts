import { apiProblem } from '../../api/client.ts'

export type TranscriptUnavailable = {
  title: string
  detail: string
  parent?: string
}

export function transcriptUnavailable(error: unknown, parent: string | undefined): TranscriptUnavailable | null {
  if (!error) return null
  const { response, problem } = apiProblem(error)
  if (response?.status !== 409 || problem.error !== 'no independent transcript') return null
  if (parent) return {
    title: 'No independent transcript for this subagent',
    detail: `This subagent has no independent transcript. Open its parent, ${parent}.`,
    parent,
  }
  return {
    title: 'No independent transcript for this subagent',
    detail: 'This subagent has no independent transcript of its own.',
  }
}
