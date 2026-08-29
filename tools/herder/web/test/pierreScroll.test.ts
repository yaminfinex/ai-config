import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { isLineCentered, LINE_CENTER_TOLERANCE_PX, MAX_LINE_SCROLL_ATTEMPTS } from '../src/features/git/pierreScroll.ts'

test('a deep selected line is not settled until its post-highlight rect is centered', () => {
  const scrollableFile = Array.from({ length: 300 }, (_, index) => `line ${index + 1}`)
  assert.equal(scrollableFile[199], 'line 200')

  const container = { top: 97.5, bottom: 876 }
  const beforeHighlightSettles = { top: 4093.5, bottom: 4113.5 }
  const centeredAfterHighlight = { top: 476.75, bottom: 496.75 }

  assert.equal(isLineCentered(beforeHighlightSettles, container), false)
  assert.equal(isLineCentered(centeredAfterHighlight, container), true)
  assert.equal(LINE_CENTER_TOLERANCE_PX, 24)
  assert.equal(MAX_LINE_SCROLL_ATTEMPTS, 8)
})

test('Pierre revalidates center after every render and bounds retries', () => {
  const source = readFileSync(new URL('../src/features/git/PierreView.tsx', import.meta.url), 'utf8')
  assert.match(source, /const renderRoot = node\.shadowRoot \?\? node/)
  assert.match(source, /if \(container && isLineCentered\(line\.getBoundingClientRect\(\), container\.getBoundingClientRect\(\)\)\)/)
  assert.match(source, /attempts >= MAX_LINE_SCROLL_ATTEMPTS/)
  assert.doesNotMatch(source, /if \(previous\?\.path === path && previous\.content === content && previous\.line === selectedLines\.start\) return/)
})
