import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const styles = readFileSync(new URL('../src/styles.css', import.meta.url), 'utf8')

function ruleFor(selector: string): string {
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  const match = styles.match(new RegExp(`${escaped}\\s*\\{([^}]+)\\}`))
  assert.ok(match, `missing CSS rule for ${selector}`)
  return match[1]
}

test('transcript wide content scrolls without wrapping or widening the pane', () => {
  assert.match(ruleFor('.markdown'), /overflow-wrap:\s*anywhere\s*;/)

  const pre = ruleFor('.transcript pre')
  assert.match(pre, /white-space:\s*pre\s*;/)
  assert.match(pre, /overflow-x:\s*auto\s*;/)

  const table = ruleFor('.transcript table')
  assert.match(table, /overflow-x:\s*auto\s*;/)

  const cells = ruleFor('.transcript :is(pre, pre code, table, th, td)')
  assert.match(cells, /overflow-wrap:\s*normal\s*;/)

  assert.match(ruleFor('.turn p'), /white-space:\s*pre-wrap\s*;/)
})

test('transcript content is not capped while the human entry keeps its deliberate width', () => {
  const cappedTranscriptRules = [...styles.matchAll(/(?:^|\n)([^{}\n]+)\s*\{([^{}]*)\}/g)]
    .filter(([, selectors, declarations]) =>
      selectors.split(',').some(selector => {
        const trimmed = selector.trim()
        return trimmed.startsWith('.transcript') || trimmed.startsWith('.assistant-')
      }) && /900px/.test(declarations))
    .map(([, selectors]) => selectors.trim())

  assert.deepEqual(cappedTranscriptRules, [])
  assert.match(ruleFor('.human-entry'), /max-width:\s*790px\s*;/)
})
