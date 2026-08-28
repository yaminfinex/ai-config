import assert from 'node:assert/strict'
import test from 'node:test'
import {
  FUZZY_POPOVER_SCORE_PER_RUNE,
  autoOpenCandidate,
  hasPathSignal,
  isConfidentResolution,
  fileFailureKind,
  keyboardCandidate,
  mentionLine,
  pathTokenSpanAt,
  quickOpenAgentPreference,
} from '../src/features/files/fileResolution.ts'
import type { ResolveResponse } from '../src/types.ts'

const response = (tier: 'exact' | 'prefix' | 'suffix' | 'fuzzy', score: number): ResolveResponse => ({
  candidates: [{ root: '/repo', path: 'tools/herder/web/src/App.tsx', tier, score }],
  roots: [{ root: '/repo', status: 'complete' }],
})

test('token spans select the occurrence under the pointer', () => {
  const text = 'src/App.tsx and src/App.tsx'
  const span = pathTokenSpanAt(text, text.lastIndexOf('App'))
  assert.deepEqual(span, { start: 16, end: 27, text: 'src/App.tsx' })
})

test('path token expansion fences slash, dot, line, and fused punctuation', () => {
  const text = 'Open tools/herder/web/src/App.tsx:44, then continue.'
  assert.equal(pathTokenSpanAt(text, text.indexOf('herder')).text, 'tools/herder/web/src/App.tsx:44,')
  assert.deepEqual(mentionLine('tools/herder/web/src/App.tsx:44,'), { line: 44 })
})

test('quoted and backticked paths retain literal spaces as one token', () => {
  const quoted = 'See "artifacts/backlog/my file — notes.md:12" now'
  const backticked = 'See `docs/a file.md` now'
  assert.equal(pathTokenSpanAt(quoted, quoted.indexOf('file')).text, '"artifacts/backlog/my file — notes.md:12"')
  assert.equal(pathTokenSpanAt(backticked, backticked.indexOf('file')).text, '`docs/a file.md`')
  assert.equal(mentionLine('"artifacts/backlog/my file — notes.md:12"').line, 12)
})

test('rendered code keeps literal spaces while prose between quotes stays fenced', () => {
  const rendered = 'artifacts/backlog/my file — notes.md:12'
  assert.equal(pathTokenSpanAt(rendered, rendered.indexOf('file'), true).text, rendered)
  const prose = 'Compare "old.ts" against "new.ts"'
  assert.equal(pathTokenSpanAt(prose, prose.indexOf('against')).text, 'against')
})

test('path signal rejects ordinary prose but admits path-shaped and code mentions', () => {
  assert.equal(hasPathSignal('ordinary', false), false)
  assert.equal(hasPathSignal('src/App.tsx', false), true)
  assert.equal(hasPathSignal('README', true), true)
  assert.equal(hasPathSignal('file:31', false), true)
})

test('auto-open is exactly one exact-or-suffix candidate and never fuzzy or prefix', () => {
  assert.equal(autoOpenCandidate(response('exact', 100))?.tier, 'exact')
  assert.equal(autoOpenCandidate(response('suffix', 100))?.tier, 'suffix')
  assert.equal(autoOpenCandidate(response('prefix', 100)), null)
  assert.equal(autoOpenCandidate(response('fuzzy', 1000)), null)
  assert.equal(autoOpenCandidate({
    ...response('exact', 100),
    candidates: [...response('exact', 100).candidates, { root: '/other', path: 'App.tsx', tier: 'suffix', score: 80 }],
  }), null)
  assert.equal(autoOpenCandidate({
    ...response('exact', 100),
    roots: [{ root: '/repo', status: 'complete' }, { root: '/offline', status: 'degraded' }],
  }), null)
})

test('explicit keyboard selection wins over automatic resolution', () => {
  const resolution = {
    ...response('exact', 100),
    candidates: [...response('exact', 100).candidates, { root: '/other', path: 'AppShell.tsx', tier: 'fuzzy' as const, score: 50 }],
  }
  assert.equal(keyboardCandidate(resolution, resolution.candidates, 1)?.path, 'AppShell.tsx')
  assert.equal(keyboardCandidate(resolution, resolution.candidates, -1)?.path, 'tools/herder/web/src/App.tsx')
})

test('file failures and agent preferences preserve honest recovery semantics', () => {
  assert.equal(fileFailureKind(404, 'not found'), 'vanished')
  assert.equal(fileFailureKind(404, 'unknown root'), 'unknown-root')
  assert.equal(fileFailureKind(502, 'substrate unreachable'), 'other')
  assert.equal(quickOpenAgentPreference('dima', 'active'), 'dima')
  assert.equal(quickOpenAgentPreference('dima', 'retired'), undefined)
  assert.equal(quickOpenAgentPreference('dima', '-'), undefined)
})

test('hard bands are confident while fuzzy uses the named per-rune floor', () => {
  assert.equal(isConfidentResolution(response('exact', 1), 'App.tsx'), true)
  assert.equal(isConfidentResolution(response('prefix', 1), 'App.tsx'), true)
  assert.equal(isConfidentResolution(response('suffix', 1), 'App.tsx'), true)
  const query = 'App.tsx'
  assert.equal(isConfidentResolution(response('fuzzy', FUZZY_POPOVER_SCORE_PER_RUNE * [...query].length), query), true)
  assert.equal(isConfidentResolution(response('fuzzy', FUZZY_POPOVER_SCORE_PER_RUNE * [...query].length - 1), query), false)
  assert.equal(isConfidentResolution(response('fuzzy', FUZZY_POPOVER_SCORE_PER_RUNE * [...query].length), '`App.tsx:44`'), true)
})
