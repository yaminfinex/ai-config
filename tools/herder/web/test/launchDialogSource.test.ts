import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

test('launch dialog restores focus to its launch button when it closes', () => {
  const component = readFileSync(new URL('../src/features/launch/LaunchAgent.tsx', import.meta.url), 'utf8')
  assert.match(component, /ref=\{launchButton\}/)
  assert.match(component, /return \(\) => launchButton\.current\?\.focus\(\)/)
  assert.match(component, /onOpenAgent\(confirmation\.action!\.agent\); close\(\)/)
})
