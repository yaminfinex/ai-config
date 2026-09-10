import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const source = readFileSync(new URL('../src/features/spaces/SpaceStrip.tsx', import.meta.url), 'utf8')

test('the focused active space starts rename with Enter or F2', () => {
  assert.match(source, /event\.key === 'Enter' \|\| event\.key === 'F2'/)
  assert.match(source, /if \(space\.id === props\.activeID\) beginRename\(space\)/)
})

test('the history button renders only when something can be reopened', () => {
  assert.match(source, /\{props\.recent\.length > 0 && <div ref=\{historyMenu\}/)
  assert.match(source, /className="space-history" aria-label="Recently closed spaces" title="Recently closed spaces"\s+aria-haspopup="menu" aria-expanded=\{historyOpen\}/)
})

test('the history menu is bounded to the eight most recent entries at the strip and only renders while open', () => {
  assert.match(source, /\{historyOpen && <div className="space-overflow-menu" role="menu" aria-label="Recently closed spaces">\s+\{props\.recent\.slice\(0, 8\)\.map/)
})

test('the inline reopen row is gone', () => {
  assert.doesNotMatch(source, /space-reopen|>reopen \{space\.name\}/)
})
