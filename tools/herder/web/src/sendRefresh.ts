import type { QueryClient } from '@tanstack/react-query'
import { queryKeys, sendMessage } from './api/client.ts'

type RefreshOwner = object

type SendRefreshToken = {
  owner: RefreshOwner
  agent: string
  deferred: boolean
  active: boolean
}

const activeSends = new WeakMap<RefreshOwner, Map<string, Set<SendRefreshToken>>>()

export function beginSendRefresh(owner: RefreshOwner, agent: string): SendRefreshToken {
  const token: SendRefreshToken = { owner, agent, deferred: false, active: true }
  let byAgent = activeSends.get(owner)
  if (!byAgent) {
    byAgent = new Map()
    activeSends.set(owner, byAgent)
  }
  const sends = byAgent.get(agent) ?? new Set<SendRefreshToken>()
  sends.add(token)
  byAgent.set(agent, sends)
  return token
}

// Returns true when the send completion owns the catch-up refresh. Every
// active token records the wake so a failed send can replay it after unmarking.
export function deferMessageRefresh(owner: RefreshOwner, agent: string) {
  const sends = activeSends.get(owner)?.get(agent)
  if (!sends?.size) return false
  sends.forEach((token) => { token.deferred = true })
  return true
}

export async function settleSendRefresh(token: SendRefreshToken, sent: boolean, refresh: () => unknown | Promise<unknown>) {
  if (!token.active) return
  if (sent) {
    try {
      await refresh()
    } catch {
      // The send succeeded; query errors remain owned by their observers.
    } finally {
      removeToken(token)
    }
    return
  }
  const replay = token.deferred
  removeToken(token)
  if (!replay) return
  try {
    await refresh()
  } catch {
    // The original message wake remains best-effort, matching stream invalidation.
  }
}

function removeToken(token: SendRefreshToken) {
  if (!token.active) return
  token.active = false
  const byAgent = activeSends.get(token.owner)
  const sends = byAgent?.get(token.agent)
  sends?.delete(token)
  if (sends?.size === 0) byAgent?.delete(token.agent)
  if (byAgent?.size === 0) activeSends.delete(token.owner)
}

// The one send sequence: mark the send, post it, then settle the agent/entries refresh.
// Composer and the note quick send both go through here; UI state stays with the caller.
export async function sendWithRefresh(queryClient: QueryClient, agent: string, text: string) {
  const token = beginSendRefresh(queryClient, agent)
  const refresh = () => Promise.all([
    queryClient.invalidateQueries({ queryKey: queryKeys.agent(agent), exact: true }),
    queryClient.invalidateQueries({ queryKey: queryKeys.entries(agent), exact: true }),
  ])
  try {
    const result = await sendMessage(agent, text)
    await settleSendRefresh(token, true, refresh)
    return result
  } catch (error: unknown) {
    await settleSendRefresh(token, false, refresh)
    throw error
  }
}
