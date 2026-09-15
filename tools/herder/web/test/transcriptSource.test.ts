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

test('transcript prose keeps its measure while containers and wide content stay uncapped', () => {
  const prose = ruleFor(
    '.transcript .markdown > :is(p, ul, ol, blockquote, h1, h2, h3, h4, h5, h6)',
  )
  assert.match(prose, /max-width:\s*900px\s*;/)

  assert.doesNotMatch(ruleFor('.transcript > *'), /max-width:/)
  assert.doesNotMatch(ruleFor('.assistant-fenced-content'), /max-width:/)
  assert.match(ruleFor('.human-entry'), /max-width:\s*790px\s*;/)
})
