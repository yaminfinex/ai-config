import { apiProblem } from '../api/client.ts'

export function failureBanner(source: string, error: unknown) {
  const failure = apiProblem(error)
  return { ...failure, source, detail: `${failure.problem.error}: ${failure.problem.detail}` }
}
