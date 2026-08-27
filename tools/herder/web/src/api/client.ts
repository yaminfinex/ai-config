import type { AgentDetail, Board, EntriesPage, LifecycleResult, Refusal } from '../types'

export type Fetcher = typeof fetch

export const queryKeys = {
  fleet: ['fleet'] as const,
  viewer: ['viewer'] as const,
  agent: (name: string) => ['agent', name] as const,
  entries: (name: string) => ['entries', name] as const,
  stream: ['stream'] as const,
  screen: (paneID: string) => ['screen', paneID] as const,
}

export type LifecycleProblem = {
  inline?: string
  readOnly?: string
  banner?: string
}

export async function refusal(response: Response): Promise<Refusal> {
  try {
    const body = await response.json() as Partial<Refusal>
    return {
      error: body.error ?? `HTTP ${response.status}`,
      detail: body.detail ?? response.statusText,
    }
  } catch {
    return { error: `HTTP ${response.status}`, detail: response.statusText }
  }
}

async function requestJSON<T>(path: string, init?: RequestInit, fetcher: Fetcher = fetch): Promise<T> {
  const response = await fetcher(path, init)
  if (!response.ok) {
    const problem = await refusal(response)
    throw Object.assign(new Error(problem.detail), { response, problem })
  }
  return response.json() as Promise<T>
}

export function apiProblem(error: unknown): { response?: Response, problem: Refusal } {
  if (error instanceof Error && 'problem' in error) {
    return {
      response: 'response' in error ? error.response as Response : undefined,
      problem: error.problem as Refusal,
    }
  }
  return { problem: { error: 'request failed', detail: error instanceof Error ? error.message : String(error) } }
}

export function getFleet(fetcher?: Fetcher) {
  return requestJSON<Board>('/api/fleet', undefined, fetcher)
}

export function getViewer(fetcher?: Fetcher) {
  return requestJSON<{ viewer: string }>('/api/viewer', undefined, fetcher)
}

export function getAgent(name: string, fetcher?: Fetcher) {
  return requestJSON<AgentDetail>(`/api/agents/${encodeURIComponent(name)}`, undefined, fetcher)
}

export function getEntries(name: string, options: { from?: number, limit: number, sessionId?: string }, fetcher?: Fetcher) {
  const query = new URLSearchParams({ limit: String(options.limit) })
  if (options.from !== undefined) query.set('from', String(options.from))
  if (options.sessionId) query.set('sessionId', options.sessionId)
  return requestJSON<EntriesPage>(`/api/agents/${encodeURIComponent(name)}/entries?${query}`, undefined, fetcher)
}

export function sendMessage(name: string, text: string, fetcher?: Fetcher) {
  return requestJSON<{ sent: boolean, to: string, from: string }>(`/api/agents/${encodeURIComponent(name)}/message`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ text }),
  }, fetcher)
}

export type SpawnRequest = {
  from_pane: string
  shape: 'pane' | 'tab' | 'worktree'
  tool: 'claude' | 'codex'
  tag: string
  prompt: string
  branch?: string
}

export function spawnAgent(body: SpawnRequest, fetcher?: Fetcher) {
  return requestJSON<LifecycleResult>('/api/spawn', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  }, fetcher)
}

export function lifecycleProblem(error: unknown): LifecycleProblem {
  const { response, problem } = apiProblem(error)
  if (response?.status === 409 && problem.error === 'attribution required') {
    return { readOnly: `Connect via Tailscale to continue. ${problem.detail}` }
  }
  if (response?.status === 502) return { banner: problem.detail }
  if (response?.status === 409) return { inline: problem.detail }
  return { inline: `${problem.error}: ${problem.detail}` }
}

export function viewerReadOnlyMessage(problem: Refusal, status: number | undefined) {
  if (status === 409 && problem.error === 'attribution required') return `Connect via Tailscale to send. ${problem.detail}`
  if (status === 409 && problem.error === 'sender refused') {
    return `Sender collision: this viewer identity maps to a web sender name already reserved or in use. ${problem.detail}`
  }
  return `Viewer identity is unavailable. ${problem.detail}`
}
