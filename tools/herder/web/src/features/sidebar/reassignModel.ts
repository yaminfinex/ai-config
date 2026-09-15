import type { Board, Row } from '../../types.ts'
import { ranked } from '../files/quickOpenModel.ts'
import { reparentRefusal, type ReparentFacts } from './reparentModel.ts'

export type ReassignCandidate = {
  kind: 'reassign'
  subject: string
  target: string
  label: string
  title?: string
}

export function flattenedBoardRows(board: Board | undefined): Row[] {
  if (!board) return []
  const result: Row[] = []
  const add = (row: Row) => {
    if (row.agent === '-' || row.bus_status === '-') return
    result.push(row)
    row.subagents?.forEach(add)
  }
  board.workspaces.forEach((workspace) => workspace.tabs.forEach((tab) => tab.panes.forEach(add)))
  board.unplaced.forEach(add)
  return result
}

export function reassignDescendants(subject: string, rows: Row[]): Set<string> {
  const children = new Map<string, string[]>()
  for (const row of rows) {
    const parent = row.parent_agent || (row.manager_state === 'live' ? row.manager : undefined)
    if (!parent || parent === row.agent) continue
    children.set(parent, [...children.get(parent) ?? [], row.agent])
  }
  const found = new Set<string>()
  const visit = (name: string) => {
    for (const child of children.get(name) ?? []) {
      if (found.has(child)) continue
      found.add(child)
      visit(child)
    }
  }
  visit(subject)
  return found
}

export function reassignCandidates(
  subject: string,
  rows: Row[],
  descendantsOf: (subject: string) => Set<string>,
  query: string,
): ReassignCandidate[] {
  const source = rows.find((row) => row.agent === subject)
  const sourceFacts = source ? facts(source) : undefined
  const descendants = descendantsOf(subject)
  const candidates = rows
    .filter((row) => !row.parent_agent)
    .filter((row) => reparentRefusal(sourceFacts, facts(row), descendants.has(row.agent)) === null)
    .sort((left, right) => left.agent.localeCompare(right.agent))
  const matches = ranked(candidates, (row) => `${row.agent} ${row.title ?? ''}`.trim(), query.trim().toLocaleLowerCase())
    .map((row): ReassignCandidate => ({ kind: 'reassign', subject, target: row.agent, label: row.agent, ...(row.title ? { title: row.title } : {}) }))
  return [...matches, { kind: 'reassign', subject, target: 'human', label: 'human (adopt)' }]
}

function facts(row: Row): ReparentFacts {
  return { kind: row.parent_agent ? 'subagent' : 'agent', agent: row.agent, bus_status: row.bus_status, manager_state: row.manager_state }
}
