import type { BacklogRead, BacklogTask, FileCandidate, FolderTarget, RootOutcome } from '../../types'

export function candidateDestination(candidate: FileCandidate): 'file' | 'folder' {
  return candidate.kind === 'dir' ? 'folder' : 'file'
}

export function folderTabID(root: string, path: string) {
  return `folder:${encodeURIComponent(root)}:${encodeURIComponent(path)}`
}

function insideRoot(path: string, root: string) {
  return path === root || path.startsWith(root.endsWith('/') ? root : `${root}/`)
}

export function cwdFolderTarget(cwd: string, roots: RootOutcome[]): FolderTarget | null {
  const root = roots.map((outcome) => outcome.root).filter((candidate) => insideRoot(cwd, candidate))
    .sort((left, right) => right.length - left.length)[0]
  if (!root) return null
  return { root, path: cwd === root ? '' : cwd.slice(root.endsWith('/') ? root.length : root.length + 1) }
}

export function exactRootChangesTarget(target: FolderTarget | null) {
  return target && !target.path ? target.root : undefined
}

export type BoardColumn = { status: string, tasks: BacklogTask[], overflow?: true }

export function boardColumns(backlog: BacklogRead): BoardColumn[] {
  const configured = new Set(backlog.statuses)
  const columns = backlog.statuses.map((status) => ({ status, tasks: backlog.tasks.filter((task) => task.status === status) }))
  const overflow = backlog.tasks.filter((task) => !task.status || !configured.has(task.status))
  return overflow.length > 0 ? [...columns, { status: 'Unconfigured status', tasks: overflow, overflow: true }] : columns
}

export function taskFileTarget(root: string, boardPath: string, taskFile: string) {
  const path = [boardPath.replace(/^\/+|\/+$/gu, ''), taskFile.replace(/^\/+|\/+$/gu, '')].filter(Boolean).join('/')
  return { root, path }
}

export type MissionFacts = { title?: string, status?: string, created?: string, updated?: string }

function frontmatter(content: string) {
  const lines = content.replace(/\r\n/gu, '\n').split('\n')
  if (lines[0] !== '---') return null
  const end = lines.indexOf('---', 1)
  return end < 0 ? null : { lines, end }
}

export function missionMarkdownBody(content: string) {
  const block = frontmatter(content)
  return block ? block.lines.slice(block.end + 1).join('\n').replace(/^\n/u, '') : content
}

function scalar(value: string): string | null {
  const trimmed = value.trim()
  if (!trimmed || trimmed.includes('#')) return null
  const quote = trimmed[0]
  if (quote === '"' || quote === "'") {
    if (trimmed.at(-1) !== quote || trimmed.slice(1, -1).includes(quote) || trimmed.includes('\\')) return null
    return trimmed.slice(1, -1)
  }
  if (/^(?:\[|\]|\{|\}|[&*!|>@`])/u.test(trimmed) || /:\s/u.test(trimmed)) return null
  return trimmed
}

export function missionFacts(content: string): MissionFacts | null {
  const block = frontmatter(content)
  if (!block) return null
  const values = new Map<string, string>()
  const ambiguous = new Set<string>()
  for (const line of block.lines.slice(1, block.end)) {
    if (!line.trim() || /^\s*#/u.test(line)) continue
    const match = line.match(/^([A-Za-z_][A-Za-z0-9_-]*):\s*(.*)$/u)
    if (!match) continue
    const key = match[1]
    const value = scalar(match[2])
    if (value === null) { ambiguous.add(key); values.delete(key); continue }
    if (values.has(key)) { ambiguous.add(key); values.delete(key); continue }
    if (!ambiguous.has(key)) values.set(key, value)
  }
  const unique = (...keys: string[]) => {
    const present = keys.flatMap((key) => values.has(key) && !ambiguous.has(key) ? [values.get(key) as string] : [])
    return present.length === 1 ? present[0] : undefined
  }
  const title = unique('title', 'mission')
  const status = unique('status')
  const created = unique('created', 'created_date')
  const updated = unique('updated', 'updated_date')
  return {
    ...(title ? { title } : {}), ...(status ? { status } : {}),
    ...(created ? { created } : {}), ...(updated ? { updated } : {}),
  }
}
