import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import {
  fileLanguage,
  gitStateForFileOpen,
  initialGitFileState,
  repoChangeSummary,
  selectGitFileMode,
  selectHistoricalDiff,
  selectHistoricalFile,
  selectedCurrentLines,
} from '../src/features/git/gitViewModel.ts'

test('language detection requests only curated grammars and unknown files stay plain text', () => {
  assert.equal(fileLanguage('src/App.tsx'), 'tsx')
  assert.equal(fileLanguage('cmd/main.go'), 'go')
  assert.equal(fileLanguage('Dockerfile'), 'docker')
  assert.equal(fileLanguage('GNUmakefile'), 'makefile')
  assert.equal(fileLanguage('notes.unknown'), 'text')
  assert.equal(fileLanguage('script'), 'text')
})

test('git file mode state is ephemeral, base-aware, and line selection stays Current-owned', () => {
  const initial = initialGitFileState()
  assert.deepEqual(initial, { mode: 'current', base: 'uncommitted' })
  assert.deepEqual(selectGitFileMode(initial, 'diff', 'branch'), { mode: 'diff', base: 'branch' })
  assert.deepEqual(selectHistoricalFile(initial, { sha: 'a'.repeat(40), path: 'old/name.ts' }), {
    mode: 'current', base: 'uncommitted', revision: { sha: 'a'.repeat(40), path: 'old/name.ts' },
  })
  assert.deepEqual(selectHistoricalDiff(initial, { sha: 'b'.repeat(40), path: 'old/name.ts' }), {
    mode: 'diff', base: 'uncommitted', commit: { sha: 'b'.repeat(40), path: 'old/name.ts' },
  })
  assert.deepEqual(selectGitFileMode(selectHistoricalFile(initial, { sha: 'a'.repeat(40), path: 'old/name.ts' }), 'current'), initial)
  assert.deepEqual(gitStateForFileOpen({ mode: 'history', base: 'branch' }, 73), initial)
  assert.deepEqual(gitStateForFileOpen({ mode: 'diff', base: 'branch' }), { mode: 'diff', base: 'branch' })
  assert.deepEqual(selectedCurrentLines(73), { start: 73, end: 73 })
  assert.equal(selectedCurrentLines(undefined), null)
})

test('change summaries use proved merge-base counts and never upstream ahead', () => {
  assert.equal(repoChangeSummary(3, 4), '3 commits + 4 uncommitted files')
  assert.equal(repoChangeSummary(0, 0), 'no unmerged commits; nothing uncommitted')
  assert.equal(repoChangeSummary(undefined, 4), '4 uncommitted files')
})

test('History stops honestly at the first follow page until deep pagination is fixed', () => {
  const panel = readFileSync(new URL('../src/features/files/FilePanel.tsx', import.meta.url), 'utf8')
  assert.match(panel, /Showing the 50 most recent commits; older history is not yet available\./)
  assert.doesNotMatch(panel, /fetchNextPage|Load older commits/)
})
