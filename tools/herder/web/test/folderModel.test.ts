import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import {
  boardColumns,
  candidateDestination,
  cwdFolderTarget,
  exactRootChangesTarget,
  folderSelectionTarget,
  parentFolderPath,
  rootJoinedAbsolutePath,
  missionFacts,
  missionMarkdownBody,
  taskFileTarget,
} from '../src/features/folders/folderModel.ts'
import type { BacklogRead, FileCandidate } from '../src/types.ts'
import { autoOpenCandidate } from '../src/features/files/fileResolution.ts'

const candidate = (kind: 'file' | 'dir', path: string): FileCandidate => ({
  root: '/repo', path, kind, tier: 'exact', score: 100,
})

test('candidate kind dispatches only after resolution certainty is established', () => {
  assert.equal(candidateDestination(candidate('file', 'README.md')), 'file')
  assert.equal(candidateDestination(candidate('dir', 'backlog')), 'folder')
})

test('a lone certain directory auto-opens while a certain file-dir mix stays a chooser', () => {
  const directory = candidate('dir', 'backlog')
  assert.equal(autoOpenCandidate({ candidates: [directory], roots: [{ root: '/repo', status: 'complete' }] }), directory)
  assert.equal(autoOpenCandidate({
    candidates: [candidate('file', 'backlog.md'), directory],
    roots: [{ root: '/repo', status: 'complete' }],
  }), null)
})

test('cwd folder target uses boundary-safe served-root containment', () => {
  const roots = [
    { root: '/work', status: 'complete' as const },
    { root: '/work/repo', status: 'complete' as const },
  ]
  assert.deepEqual(cwdFolderTarget('/work/repo/packages/web', roots), { root: '/work/repo', path: 'packages/web' })
  assert.deepEqual(cwdFolderTarget('/work/repo', roots), { root: '/work/repo', path: '' })
  assert.equal(cwdFolderTarget('/workspace/repo', roots), null)
  assert.equal(exactRootChangesTarget({ root: '/work/repo', path: '' }), '/work/repo')
  assert.equal(exactRootChangesTarget({ root: '/work/repo', path: 'packages/web' }), undefined)
  assert.equal(exactRootChangesTarget(null), undefined)
})

test('filesystem paths stay absolute and parent navigation stops honestly at the served root', () => {
  assert.equal(rootJoinedAbsolutePath('/repo', 'src/App.tsx'), '/repo/src/App.tsx')
  assert.equal(rootJoinedAbsolutePath('/repo/', '/src/App.tsx'), '/repo/src/App.tsx')
  assert.equal(rootJoinedAbsolutePath('/', 'src/App.tsx'), '/src/App.tsx')
  assert.equal(rootJoinedAbsolutePath('/repo', ''), '/repo')
  assert.equal(parentFolderPath('src/components'), 'src')
  assert.equal(parentFolderPath('src'), '')
  assert.equal(parentFolderPath(''), null)
})

test('a folder selection hint accepts only a direct file child in the addressed root', () => {
  assert.deepEqual(folderSelectionTarget('/repo', 'src', { root: '/repo', path: 'src/App.tsx' }), {
    root: '/repo', path: 'src/App.tsx',
  })
  assert.equal(folderSelectionTarget('/repo', 'src', { root: '/other', path: 'src/App.tsx' }), null)
  assert.equal(folderSelectionTarget('/repo', 'src', { root: '/repo', path: 'src/nested/App.tsx' }), null)
  assert.equal(folderSelectionTarget('/repo', '', { root: '/repo', path: 'README.md' })?.path, 'README.md')
})

test('folder selection hints are consumed before an invalid hint can be ignored', () => {
  const panel = readFileSync(new URL('../src/features/folders/FolderPanel.tsx', import.meta.url), 'utf8')
  assert.match(panel, /if \(!selectionHint\) return\s+const hintedFile = folderSelectionTarget[\s\S]*onSelectionHintConsumed\?\.\(\)\s+if \(!hintedFile\) return/)
})

test('board columns preserve configured order and surface unconfigured statuses', () => {
  const backlog: BacklogRead = {
    root: '/repo', path: 'backlog', statuses: ['To Do', 'Done'],
    tasks: [
      { id: 'TASK-1', title: 'First', status: 'Done', ordinal: 2, file: 'tasks/1.md' },
      { id: 'TASK-2', title: 'Second', status: 'Blocked', ordinal: 1, file: 'tasks/2.md' },
      { id: 'TASK-3', title: 'Third', status: 'To Do', ordinal: 3, file: 'tasks/3.md' },
    ],
    unparsed: [], truncated: false, fetched_at: '2026-08-28T09:00:00Z',
  }
  assert.deepEqual(boardColumns(backlog).map((column) => [column.status, column.tasks.map((task) => task.id)]), [
    ['To Do', ['TASK-3']], ['Done', ['TASK-1']], ['Unconfigured status', ['TASK-2']],
  ])
})

test('task targets stay relative to the addressed board directory', () => {
  assert.deepEqual(taskFileTarget('/repo', 'backlog', 'tasks/task-1.md'), {
    root: '/repo', path: 'backlog/tasks/task-1.md',
  })
  assert.deepEqual(taskFileTarget('/repo', '', 'tasks/task-1.md'), {
    root: '/repo', path: 'tasks/task-1.md',
  })
})

test('mission facts accept only simple unambiguous top-of-file scalars', () => {
  assert.deepEqual(missionFacts('---\nmission: fleet-refit\nstatus: active\ncreated: 2026-08-28\nupdated: "2026-08-29"\n---\n# Body'), {
    title: 'fleet-refit', status: 'active', created: '2026-08-28', updated: '2026-08-29',
  })
  assert.deepEqual(missionFacts('---\ntitle: First\ntitle: Second\nstatus: active\n---\n'), { status: 'active' })
  assert.equal(missionFacts('preamble\n---\nmission: guessed\n---\n'), null)
  assert.deepEqual(missionFacts('---\nmission: [fleet-refit]\nstatus: { value: active }\ncreated: 2026-08-28 # comment\n---\n'), {})
})

test('mission rendered markdown omits only a leading closed frontmatter block', () => {
  assert.equal(missionMarkdownBody('---\nmission: fleet-refit\n---\n# Mission\nBody'), '# Mission\nBody')
  assert.equal(missionMarkdownBody('preamble\n---\nmission: fleet-refit\n---\n'), 'preamble\n---\nmission: fleet-refit\n---\n')
  assert.equal(missionMarkdownBody('---\nmission: fleet-refit\n'), '---\nmission: fleet-refit\n')
})
