import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const panel = readFileSync(new URL('../src/features/files/FilePanel.tsx', import.meta.url), 'utf8')

test('raw HTML query is enabled only for current truncated rendered HTML', () => {
  assert.match(panel, /rawQueryEnabled = gitState\.mode === 'current' && !gitState\.revision && html && truncated && viewMode === 'rendered'/)
  assert.match(panel, /enabled: rawQueryEnabled/)
})

test('small rendered HTML stays content-sourced and never enables raw fetching', () => {
  assert.match(panel, /htmlPreviewModel\(html, truncated, rawState,/)
  assert.match(panel, /preview\.srcdocSource === 'raw' \? rawQuery\.data : data\.content/)
})

test('HTML preview remains maximally sandboxed', () => {
  assert.match(panel, /<iframe[^>]+sandbox=""/s)
  assert.doesNotMatch(panel, /allow-scripts|allow-same-origin/)
})
