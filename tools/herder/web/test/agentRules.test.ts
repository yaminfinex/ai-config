import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

test('Claude imports the terse frontend rules pointer', () => {
  const pointer = readFileSync(new URL('../CLAUDE.md', import.meta.url), 'utf8')
  const rules = readFileSync(new URL('../AGENTS.md', import.meta.url), 'utf8')
  assert.equal(pointer, '@AGENTS.md\n')
  assert.ok(rules.split('\n').length < 100)
  assert.match(rules, /bulletproof-react/)
  assert.match(rules, /Rules of React/)
  assert.match(rules, /single useFleetStream EventSource/)
  assert.match(rules, /panel-kind behavior lives in the workspace registry/)
  assert.match(rules, /agent mentions use quieter dotted underlines/)
})
