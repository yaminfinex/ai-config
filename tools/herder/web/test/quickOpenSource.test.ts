import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const source = readFileSync(new URL('../src/features/files/QuickOpen.tsx', import.meta.url), 'utf8')

test('QuickOpen clears the query and starts on the first openable row on open or mode changes', () => {
  assert.match(source, /useEffect\(\(\) => \{\s*setQuery\(''\)[\s\S]*?const initialRows = quickOpenRows\(mode, '', rowContext\)[\s\S]*?setSelection\(open \? mode\.kind === 'normal' \? quickOpenInitialSelection\(initialRows, ''\) : reassignSelection\(initialRows, ''\) : null\)[\s\S]*?\}, \[open, mode\]\)/)
})

test('QuickOpen resets the selection only on a real query edit, never when the debounce settles', () => {
  // Every effect whose dependency list names `debounced` must leave the selection alone.
  const effects = [...source.matchAll(/useEffect\(\(\) => ([\s\S]*?), \[([^\]]*)\]\)/g)].map(([, body, deps]) => ({ body, deps: deps.split(',').map((dep) => dep.trim()) }))
  assert.ok(effects.length >= 3, `expected the QuickOpen effects, found ${effects.length}`)
  for (const effect of effects.filter(({ deps }) => deps.includes('debounced'))) assert.doesNotMatch(effect.body, /setSelection\(/)
  assert.doesNotMatch(source, /setActiveIndex/)
  assert.match(source, /onChange=\{\(event\) => \{\s*setQuery\(event\.target\.value\)[\s\S]*?const nextRows = quickOpenRows\(mode, event\.target\.value, rowContext\)[\s\S]*?setSelection\(normalMode \? quickOpenInitialSelection\([\s\S]*?: reassignSelection\(/)
  assert.match(source, /setSelection\(quickOpenMoveSelection\(actions, fileKeys, selection, event\.key === 'ArrowDown' \? 'down' : 'up'\)\)/)
  assert.doesNotMatch(source, /useState\(-1\)/)
  assert.match(source, /scrollIntoView\(\{ block: 'nearest' \}\)/)
})

test('QuickOpen gets every mode-dependent action list from quickOpenRows', () => {
  assert.equal((source.match(/quickOpenRows\(/g) ?? []).length, 3)
  assert.doesNotMatch(source, /normalMode\s*\?\s*quickOpenActionRows/)
  assert.doesNotMatch(source, /reassignCandidates\(/)
})

test('QuickOpen resolves files only for a nonempty settled current query', () => {
  assert.match(source, /useDebounced\(open && normalMode \? query\.trim\(\) : ''\)/)
  assert.match(source, /enabled: open && normalMode && Boolean\(query\.trim\(\)\) && query\.trim\(\) === debounced/)
})

test('QuickOpen is a body-level layer above the diagram overlay: portaled to document.body when open, nothing when closed', () => {
  assert.match(source, /import \{ createPortal \} from 'react-dom'/)
  assert.match(source, /if \(!open\) return null/)
  // the whole palette (backdrop included) is the portal's child; its container is the body, not a node inside #root
  assert.match(source, /return createPortal\(<div className="quick-open-backdrop"[\s\S]*<\/div>, document\.body\)\n\}/)
  // a backdrop click closes without letting the browser drop focus on the body (the close effect restores it to the layer under)
  assert.match(source, /onMouseDown=\{\(event\) => \{ if \(event\.target !== event\.currentTarget\) return; event\.preventDefault\(\); onClose\(\) \}\}/)
  assert.equal((source.match(/createPortal\(/g) ?? []).length, 1)
  assert.doesNotMatch(source, /getElementById\('root'\)/)
  const css = readFileSync(new URL('../src/styles.css', import.meta.url), 'utf8')
  assert.match(css, /\.quick-open-backdrop \{ position: fixed; z-index: 100;/)
  assert.match(css, /\.diagram-overlay \{ position: fixed; z-index: 90;/)
})

test('QuickOpen contains the keyboard at its dialog boundary: Tab wraps inside the palette, Escape closes it from anywhere in it and stops before any layer under it', () => {
  assert.match(source, /import \{ dialogTabTargetIndex \} from '\.\.\/launch\/launchModel\.ts'/)
  assert.match(source, /const focusableSelector = 'button:not\(\[disabled\]\), input, \[tabindex\]:not\(\[tabindex="-1"\]\)'/)
  // both handlers live on the section[role=dialog], not on the input
  const section = source.match(/<section className="quick-open" role="dialog"[\s\S]*?onKeyDown=\{\(event\) => \{([\s\S]*?)\}\}>/)
  assert.ok(section, 'the dialog section has a key handler')
  assert.match(section![1], /if \(event\.key === 'Tab'\) \{\s*const items = \[\.\.\.event\.currentTarget\.querySelectorAll<HTMLElement>\(focusableSelector\)\]\s*const next = dialogTabTargetIndex\(items\.indexOf\(document\.activeElement as HTMLElement\), items\.length, event\.shiftKey\)\s*if \(next === null\) return\s*event\.preventDefault\(\)\s*items\[next\]\?\.focus\(\)/)
  assert.match(section![1], /else if \(event\.key === 'Escape'\) \{\s*event\.preventDefault\(\)\s*event\.stopPropagation\(\)\s*onClose\(\)\s*\}/)
  // the input keeps only its own keys
  assert.match(source, /<input ref=\{inputRef\}[\s\S]*?onKeyDown=\{\(event\) => \{\s*if \(event\.key === 'ArrowDown' \|\| event\.key === 'ArrowUp'\)/)
  assert.equal((source.match(/event\.key === 'Escape'/g) ?? []).length, 1)
  assert.equal((source.match(/event\.key === 'Tab'/g) ?? []).length, 1)
  // focus goes back to whoever had it at open (the overlay root when the palette was summoned over a diagram)
  assert.match(source, /restoreFocus\.current = document\.activeElement as HTMLElement \| null/)
  assert.match(source, /restoreFocus\.current\?\.focus\(\)/)
})
