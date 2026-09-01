import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const panel = readFileSync(new URL('../src/features/files/FilePanel.tsx', import.meta.url), 'utf8')

test('HTML files share the rendered/source control with a plain script warning', () => {
  assert.match(panel, /isHtmlPath/)
  assert.match(panel, /Scripts do not run/)
  assert.match(panel, /srcDoc=\{data\.content\}/)
})

test('HTML preview uses the strict empty iframe sandbox and truncated files stay source', () => {
  assert.match(panel, /<iframe[^>]+sandbox=""/s)
  assert.doesNotMatch(panel, /allow-scripts|allow-same-origin/)
  assert.match(panel, /truncated = Boolean\(data && !data\.binary && data\.truncated\)/)
  assert.match(panel, /effectiveViewMode = html && truncated \? 'source' : viewMode/)
  assert.match(panel, /disabled=\{html && truncated\}/)
  assert.match(panel, /Rendered view is unavailable because this file is truncated\./)
})
