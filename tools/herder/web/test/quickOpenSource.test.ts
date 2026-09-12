import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const source = readFileSync(new URL('../src/features/files/QuickOpen.tsx', import.meta.url), 'utf8')

test('QuickOpen clears the query and starts on the first openable row on open changes', () => {
  assert.match(source, /useEffect\(\(\) => \{\s*setQuery\(''\)\s*setSelection\(open \? quickOpenInitialSelection\([\s\S]*?, ''\) : null\)\s*if \(!open\) return[\s\S]*?\}, \[open\]\)/)
})

test('QuickOpen resets the selection only on a real query edit, never when the debounce settles', () => {
  // Every effect whose dependency list names `debounced` must leave the selection alone.
  const effects = [...source.matchAll(/useEffect\(\(\) => ([\s\S]*?), \[([^\]]*)\]\)/g)].map(([, body, deps]) => ({ body, deps: deps.split(',').map((dep) => dep.trim()) }))
  assert.ok(effects.length >= 3, `expected the QuickOpen effects, found ${effects.length}`)
  for (const effect of effects.filter(({ deps }) => deps.includes('debounced'))) assert.doesNotMatch(effect.body, /setSelection\(/)
  assert.doesNotMatch(source, /setActiveIndex/)
  assert.match(source, /onChange=\{\(event\) => \{\s*setQuery\(event\.target\.value\)\s*setSelection\(quickOpenInitialSelection\(/)
  assert.match(source, /setSelection\(quickOpenMoveSelection\(actions, fileKeys, selection, event\.key === 'ArrowDown' \? 'down' : 'up'\)\)/)
  assert.doesNotMatch(source, /useState\(-1\)/)
  assert.match(source, /scrollIntoView\(\{ block: 'nearest' \}\)/)
})

test('QuickOpen resolves files only for a nonempty settled current query', () => {
  assert.match(source, /useDebounced\(open \? query\.trim\(\) : ''\)/)
  assert.match(source, /enabled: open && Boolean\(query\.trim\(\)\) && query\.trim\(\) === debounced/)
})
