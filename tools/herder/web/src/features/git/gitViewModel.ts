import type { SelectedLineRange } from '@pierre/diffs/react'

export type GitBase = 'uncommitted' | 'branch'
export type GitFileMode = 'current' | 'diff' | 'history'
export type GitFileState = { mode: GitFileMode, base: GitBase }

const extensionLanguages: Record<string, string> = {
  go: 'go', ts: 'typescript', mts: 'typescript', cts: 'typescript', js: 'javascript', mjs: 'javascript', cjs: 'javascript',
  tsx: 'tsx', jsx: 'jsx', py: 'python', sh: 'shellscript', bash: 'shellscript', json: 'json', jsonl: 'json',
  yaml: 'yaml', yml: 'yaml', md: 'markdown', markdown: 'markdown', html: 'html', htm: 'html', css: 'css',
  sql: 'sql', rs: 'rust', diff: 'diff', patch: 'diff',
}

const exactLanguages: Record<string, string> = {
  Dockerfile: 'docker', Makefile: 'makefile', GNUmakefile: 'makefile',
}

export function fileLanguage(path: string) {
  const name = path.split('/').at(-1) ?? path
  if (exactLanguages[name]) return exactLanguages[name]
  const extension = name.includes('.') ? name.split('.').at(-1)?.toLowerCase() ?? '' : ''
  return extensionLanguages[extension] ?? 'text'
}

export function initialGitFileState(): GitFileState {
  return { mode: 'current', base: 'uncommitted' }
}

export function selectGitFileMode(state: GitFileState, mode: GitFileMode, base = state.base): GitFileState {
  return state.mode === mode && state.base === base ? state : { mode, base }
}

export function selectedCurrentLines(line?: number): SelectedLineRange | null {
  return line ? { start: line, end: line } : null
}

function countLabel(count: number, singular: string, plural: string) {
  return `${count} ${count === 1 ? singular : plural}`
}

export function repoChangeSummary(commitsSinceBase: number | undefined, uncommittedFiles: number) {
  if (commitsSinceBase === 0 && uncommittedFiles === 0) return 'no unmerged commits; nothing uncommitted'
  if (commitsSinceBase === undefined && uncommittedFiles === 0) return 'nothing uncommitted'
  const parts = []
  if (commitsSinceBase !== undefined) parts.push(countLabel(commitsSinceBase, 'commit', 'commits'))
  if (uncommittedFiles > 0 || commitsSinceBase === undefined) parts.push(countLabel(uncommittedFiles, 'uncommitted file', 'uncommitted files'))
  return parts.join(' + ')
}
