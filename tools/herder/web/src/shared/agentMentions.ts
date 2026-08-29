import { createElement, Fragment, memo, useMemo, type MouseEvent, type ReactNode } from 'react'
import type { Board, Row } from '../types.ts'

export type AgentMentionToken = string | { text: string, name: string }

export type AgentMentionMatcher = {
  version: string
  resolve: (alias: string) => string | undefined
  tokenize: (text: string) => AgentMentionToken[]
}

export type AgentMentionOpen = (name: string, event: MouseEvent<HTMLElement>) => void

const baseName = /(?:^|-)([a-z]{4})$/u
const agentBoundary = '\\p{L}\\p{N}_@-'
const rosterMatchers = new Map<string, AgentMentionMatcher>()

function escaped(value: string) {
  return value.replace(/[.*+?^${}()|[\]\\]/gu, '\\$&')
}

function liveAgentNames(board: Board | undefined) {
  const names = new Set<string>()
  const add = (row: Row) => {
    if (row.agent && row.agent !== '-') names.add(row.agent)
    row.subagents?.forEach(add)
  }
  board?.workspaces.forEach((workspace) => workspace.tabs.forEach((tab) => tab.panes.forEach(add)))
  board?.unplaced.forEach(add)
  return [...names].sort()
}

export function agentMentionMatcher(board: Board | undefined): AgentMentionMatcher {
  const canonical = liveAgentNames(board)
  const claims = new Map<string, Set<string>>()
  const claim = (alias: string, name: string) => {
    const names = claims.get(alias) ?? new Set<string>()
    names.add(name)
    claims.set(alias, names)
  }
  canonical.forEach((name) => {
    const base = name.match(baseName)?.[1]
    if (base) claim(base, name)
  })
  const aliases = new Map(canonical.map((name) => [name, name] as const))
  claims.forEach((names, alias) => {
    if (!aliases.has(alias) && names.size === 1) aliases.set(alias, [...names][0])
  })
  const version = JSON.stringify([...aliases.entries()])
  const cachedMatcher = rosterMatchers.get(version)
  if (cachedMatcher) return cachedMatcher
  const ordered = [...aliases.keys()].sort((left, right) => right.length - left.length || left.localeCompare(right))
  const pattern = ordered.length > 0
    ? new RegExp(`(^|[^${agentBoundary}])(@?(?:${ordered.map(escaped).join('|')}))(?![${agentBoundary}])`, 'gu')
    : null
  const cache = new Map<string, AgentMentionToken[]>()

  const matcher: AgentMentionMatcher = {
    version,
    resolve(alias) {
      const label = alias.startsWith('@') ? alias.slice(1) : alias
      return aliases.get(label)
    },
    tokenize(text) {
      const cached = cache.get(text)
      if (cached) return cached
      if (!pattern) return [text]
      pattern.lastIndex = 0
      const tokens: AgentMentionToken[] = []
      let cursor = 0
      for (const match of text.matchAll(pattern)) {
        const prefix = match[1]
        const label = match[2]
        const start = (match.index ?? 0) + prefix.length
        const end = start + label.length
        const before = text[start - 1]
        const after = text[end]
        const pathAdjacent = before === '/' || before === '\\' || after === '/' || after === '\\' ||
          after === '.' && /[\p{L}\p{N}]/u.test(text[end + 1] ?? '')
        if (pathAdjacent) continue
        if (start > cursor) tokens.push(text.slice(cursor, start))
        const name = matcher.resolve(label)
        if (name) tokens.push({ text: label, name })
        else tokens.push(label)
        cursor = end
      }
      if (cursor < text.length) tokens.push(text.slice(cursor))
      const result = tokens.some((token) => typeof token !== 'string') ? tokens : [text]
      if (cache.size >= 1000) cache.clear()
      cache.set(text, result)
      return result
    },
  }
  if (rosterMatchers.size >= 8) rosterMatchers.delete(rosterMatchers.keys().next().value ?? '')
  rosterMatchers.set(version, matcher)
  return matcher
}

export const AgentMentionText = memo(function AgentMentionText({ text, matcher, onOpen, sideHint }: {
  text: string
  matcher: AgentMentionMatcher
  onOpen: AgentMentionOpen
  sideHint?: string
}) {
  const tokens = useMemo(() => matcher.tokenize(text), [matcher, text])
  return createElement(Fragment, null, ...tokens.map((token, index): ReactNode => typeof token === 'string' ? token : createElement('button', {
    type: 'button',
    className: 'inline-link agent-mention',
    title: `Open ${token.name}${sideHint ? ` · ${sideHint}` : ''}`,
    onClick: (event: MouseEvent<HTMLButtonElement>) => onOpen(token.name, event),
    key: `${index}:${token.text}:${token.name}`,
  }, token.text)))
})
