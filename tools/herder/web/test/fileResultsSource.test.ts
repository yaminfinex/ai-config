import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const read = (path: string) => readFileSync(new URL(`../src/features/files/${path}`, import.meta.url), 'utf8')
const results = read('FileResults.tsx')
const quickOpen = read('QuickOpen.tsx')
const transcript = read('TranscriptFileResolver.tsx')

test('the result list ends with the quiet "N of TOTAL" line from the server total', () => {
  assert.match(results, /resultsLimitLine\(visible\.length, Math\.max\(resolution\.total \?\? 0, resolution\.candidates\.length\)\)/)
  assert.match(results, /\{limitLine && <p className="file-results-limit">\{limitLine\}<\/p>\}/)
  assert.doesNotMatch(results, /Showing /)
})

test('the palette takes the server default cap; a transcript link asks for enough to auto-open', () => {
  assert.match(quickOpen, /resolveFiles\(debounced, agent, fetch, signal\)/)
  assert.doesNotMatch(quickOpen, /QUICK_OPEN_RESULT_LIMIT|\.slice\(0, /)
  assert.match(transcript, /const TRANSCRIPT_RESOLVE_LIMIT = 100/)
  assert.match(transcript, /resolveFiles\(mention, context, fetch, controller\.signal, TRANSCRIPT_RESOLVE_LIMIT\)/)
})

test('each result row shows the quiet "+N worktrees" after its root label, with the folded roots on hover', () => {
  assert.match(results, /<span className="root-tag" title=\{rootTitle\(candidate\)\}>\{rootLabel\(candidate\.root\)\}\{alsoLabel\(candidate\) && <span className="root-also"> \{alsoLabel\(candidate\)\}<\/span>\}<\/span>/)
})
