import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

test('screen viewport upgrades in place to direct-focus xterm with fixed Herdr geometry', () => {
  const source = readFileSync(new URL('../src/features/screen/ScreenPanel.tsx', import.meta.url), 'utf8')
  assert.match(source, /@xterm\/xterm/)
  assert.match(source, /@xterm\/addon-fit/)
  assert.match(source, /@xterm\/addon-webgl/)
  assert.match(source, /scrollback:\s*live\s*\?\s*0\s*:\s*2000/)
  assert.match(source, /proposeDimensions\(\)/)
  assert.doesNotMatch(source, /\.fit\(\)/)
  assert.match(source, /terminal\.resize\(frame\.cols, frame\.rows\)/)
  assert.match(source, /onContextLoss/)
  assert.doesNotMatch(source, /<textarea|<input|Send<\/button>/)
})

test('live terminal accepts direct keystrokes without fabricating local echo', () => {
  const source = readFileSync(new URL('../src/features/screen/ScreenPanel.tsx', import.meta.url), 'utf8')
  assert.match(source, /onData/)
  assert.match(source, /live — keystrokes go to the real pane/)
  assert.doesNotMatch(source, /onData[\s\S]{0,300}(?:terminal|term)\.write/)
})
