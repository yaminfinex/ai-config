import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const source = readFileSync(new URL('../src/features/files/QuickOpen.tsx', import.meta.url), 'utf8')

test('QuickOpen clears the query and active index on open changes', () => {
  assert.match(source, /useEffect\(\(\) => \{\s*setQuery\(''\)\s*setActiveIndex\(-1\)\s*if \(!open\) return[\s\S]*?\}, \[open\]\)/)
})

test('QuickOpen resolves files only for a nonempty settled current query', () => {
  assert.match(source, /useDebounced\(open \? query\.trim\(\) : ''\)/)
  assert.match(source, /enabled: open && Boolean\(query\.trim\(\)\) && query\.trim\(\) === debounced/)
})
