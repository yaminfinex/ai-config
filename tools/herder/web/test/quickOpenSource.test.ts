import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const source = readFileSync(new URL('../src/features/files/QuickOpen.tsx', import.meta.url), 'utf8')

test('QuickOpen clears the query and starts on the first openable row on open changes', () => {
  assert.match(source, /useEffect\(\(\) => \{\s*setQuery\(''\)\s*setActiveIndex\(open \? quickOpenInitialIndex\([\s\S]*?, ''\) : -1\)\s*if \(!open\) return[\s\S]*?\}, \[open\]\)/)
})

test('QuickOpen resets the selection only on a real query edit, never when the debounce settles', () => {
  assert.doesNotMatch(source, /setActiveIndex\(-1\), \[debounced/)
  assert.match(source, /onChange=\{\(event\) => \{\s*setQuery\(event\.target\.value\)\s*setActiveIndex\(quickOpenInitialIndex\(/)
  assert.match(source, /scrollIntoView\(\{ block: 'nearest' \}\)/)
})

test('QuickOpen resolves files only for a nonempty settled current query', () => {
  assert.match(source, /useDebounced\(open \? query\.trim\(\) : ''\)/)
  assert.match(source, /enabled: open && Boolean\(query\.trim\(\)\) && query\.trim\(\) === debounced/)
})
