import type { AgentDetail } from '../types.ts'
import { agentVitalsPresentation } from './agentVitals.ts'

export function hasRightOverflow(scrollWidth: number, clientWidth: number, scrollLeft: number) {
  return scrollWidth - clientWidth - scrollLeft > 1
}

export type AgentContextPresentation = {
  status: string
  cwd?: { display: string, full: string }
  repository?: { display: string, remote?: string }
  details: string[]
  vitals: string[]
}

export function middleEllipsis(value: string, maximum = 44) {
  if (value.length <= maximum) return value
  if (maximum < 3) return value.slice(0, maximum)
  const available = maximum - 1
  const tail = Math.ceil(available * 0.6)
  return `${value.slice(0, available - tail)}…${value.slice(-tail)}`
}

export function repoNameFromRemote(remote: string | undefined) {
  const value = remote?.trim()
  if (!value) return undefined
  let path = ''
  if (/^[a-z][a-z0-9+.-]*:\/\//i.test(value)) {
    try {
      path = new URL(value).pathname
    } catch {
      return undefined
    }
  } else {
    const scp = value.match(/^[^/\s]+:(.+)$/)
    if (scp) path = scp[1]
    else if (value.includes('/')) path = value
    else return undefined
  }
  const name = path.replace(/\/+$/, '').split('/').at(-1)?.replace(/\.git$/, '')
  return name && name !== '.' && name !== '..' ? name : undefined
}

function gapLabel(gap: string) {
  return gap.toLowerCase().includes('pane') ? 'no pane' : 'gap'
}

export function agentContextPresentation(agent: AgentDetail, liveStatus: string): AgentContextPresentation {
  const status = liveStatus !== '-' ? liveStatus : agent.bus_status
  const retired = status === 'retired' || agent.bus_status === 'retired'
  const details = [
    retired ? 'read-only' : agent.pane?.pane_id ?? 'unplaced',
    agent.herdr_status !== '-' && agent.herdr_status.toLowerCase() !== 'idle' ? `herdr ${agent.herdr_status}` : '',
    !retired && agent.gap !== '-' ? gapLabel(agent.gap) : '',
  ].filter(Boolean)
  const repo = repoNameFromRemote(agent.git?.remote_url)
  const branch = agent.git?.branch
  const repository = repo || branch
    ? { display: [repo, branch].filter(Boolean).join(' · '), remote: agent.git?.remote_url }
    : undefined
  return {
    status,
    cwd: agent.cwd ? { display: middleEllipsis(agent.cwd), full: agent.cwd } : undefined,
    repository,
    details,
    vitals: agentVitalsPresentation(agent),
  }
}
