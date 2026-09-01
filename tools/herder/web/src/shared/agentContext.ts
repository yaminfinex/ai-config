import type { AgentDetail } from '../types.ts'
import { agentVitalsPresentation } from './agentVitals.ts'

export function hasRightOverflow(scrollWidth: number, clientWidth: number, scrollLeft: number) {
  return scrollWidth - clientWidth - scrollLeft > 1
}

export type AgentContextPresentation = {
  status: string
  cwd?: { display: string, full: string }
  repository?: {
    display: string
    remote?: string
    repo?: string
    branch?: string
    links?: { repository: string, branch?: string }
  }
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

function encodedPathSegment(segment: string) {
  try {
    return encodeURIComponent(decodeURIComponent(segment))
  } catch {
    return encodeURIComponent(segment)
  }
}

function remoteHTTPSBase(remote: string | undefined) {
  const value = remote?.trim()
  if (!value) return undefined
  let host = ''
  let path = ''
  if (/^[a-z][a-z0-9+.-]*:\/\//i.test(value)) {
    try {
      const parsed = new URL(value)
      if (parsed.protocol !== 'https:' && parsed.protocol !== 'ssh:') return undefined
      host = parsed.host
      path = parsed.pathname
    } catch {
      return undefined
    }
  } else {
    const scp = value.match(/^(?:[^@/\s]+@)?([^:/\s]+):(.+)$/)
    if (!scp) return undefined
    host = scp[1]
    path = scp[2]
  }
  const segments = path.replace(/^\/+|\/+$/g, '').split('/').filter(Boolean)
  if (!host || segments.length < 2) return undefined
  segments[segments.length - 1] = segments.at(-1)!.replace(/\.git$/i, '')
  if (!segments.at(-1)) return undefined
  return `https://${host}/${segments.map(encodedPathSegment).join('/')}`
}

export function repositoryBrowseLinks(remote: string | undefined, branch: string | undefined) {
  const repository = remoteHTTPSBase(remote)
  if (!repository) return undefined
  const branchPath = branch?.split('/').map((segment) => encodeURIComponent(segment)).join('/')
  return {
    repository,
    ...(branchPath ? { branch: `${repository}/tree/${branchPath}` } : {}),
  }
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
  const links = repo ? repositoryBrowseLinks(agent.git?.remote_url, branch) : undefined
  const repository = repo || branch
    ? {
        display: [repo, branch].filter(Boolean).join(' · '),
        remote: agent.git?.remote_url,
        ...(repo ? { repo } : {}),
        ...(branch ? { branch } : {}),
        ...(links ? { links } : {}),
      }
    : undefined
  return {
    status,
    cwd: agent.cwd ? { display: middleEllipsis(agent.cwd), full: agent.cwd } : undefined,
    repository,
    details,
    vitals: agentVitalsPresentation(agent),
  }
}
