import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

test('the focused active space starts rename with Enter or F2', () => {
  const source = readFileSync(new URL('../src/features/spaces/SpaceStrip.tsx', import.meta.url), 'utf8')
  assert.match(source, /event\.key === 'Enter' \|\| event\.key === 'F2'/)
  assert.match(source, /if \(space\.id === props\.activeID\) beginRename\(space\)/)
})
