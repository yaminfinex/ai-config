import assert from 'node:assert/strict'
import { createRequire } from 'node:module'
import test from 'node:test'
import { runInNewContext } from 'node:vm'
import { fileURLToPath } from 'node:url'
import { build } from 'esbuild'
import type { MouseEvent } from 'react'
import type { ResolveResponse } from '../src/types.ts'

// Execute the real hook callback without a renderer. Only React state/effects,
// the DOM boundary, and resolveFiles are replaced; token/range logic is real.
const bundle = await build({
  entryPoints: [fileURLToPath(new URL('../src/features/files/TranscriptFileResolver.tsx', import.meta.url))],
  bundle: true, write: false, format: 'cjs', platform: 'node', packages: 'external', jsx: 'automatic',
  plugins: [{ name: 'resolve-files-fixture', setup(builder) {
    builder.onResolve({ filter: /api\/client$/ }, () => ({ path: 'client', namespace: 'fixture' }))
    builder.onLoad({ filter: /.*/, namespace: 'fixture' }, () => ({ contents: 'export const resolveFiles = globalThis.resolveFiles' }))
  } }],
})

class FixtureNode {
  static TEXT_NODE = 3
  nodeType = 3
  nodeName = '#text'
  textContent = ''
  previousSibling: FixtureNode | null = null
  nextSibling: FixtureNode | null = null
  getRootNode() { return this }
}

class FixtureElement extends FixtureNode {
  nodeType = 1
  href: string | null = null
  ignored = false
  closest(selector: string) {
    if (selector === '.path-link' && this.href !== null) return this
    if (selector.startsWith('a,') && this.ignored) return this
    return null
  }
  getAttribute(name: string) { return name === 'title' ? this.href : null }
}

function fixture(result: ResolveResponse, enabled = true) {
  const target = new FixtureElement()
  const queries: string[] = []
  const opened: unknown[] = []
  const states: unknown[] = []
  const selected: unknown[] = []
  const ends: unknown[] = []
  const caret = { offsetNode: new FixtureNode(), offset: 0 }
  const range = {
    selectNodeContents: (node: unknown) => selected.push(node),
    setStart: (node: unknown, offset: number) => ends.push([node, offset]),
    setEnd: (node: unknown, offset: number) => ends.push([node, offset]),
    getBoundingClientRect: () => ({ left: 12, bottom: 24 }),
  }
  const require = createRequire(import.meta.url)
  const module = { exports: {} as typeof import('../src/features/files/TranscriptFileResolver.tsx') }
  runInNewContext(bundle.outputFiles[0].text, {
    module, exports: module.exports,
    require: (name: string) => name === 'react' ? {
      useCallback: (callback: unknown) => callback,
      useEffect: () => undefined,
      useRef: (value: unknown) => ({ current: value }),
      useState: (value: unknown) => [value, (next: unknown) => states.push(next)],
    } : require(name),
    window: { innerWidth: 1000, innerHeight: 800 },
    document: {
      createRange: () => range,
      caretPositionFromPoint: () => caret,
      getSelection: () => ({ removeAllRanges: () => undefined, addRange: (value: unknown) => selected.push(value) }),
    },
    Node: FixtureNode, Element: FixtureElement, ShadowRoot: class {}, AbortController, DOMException,
    fetch: () => assert.fail('resolveFiles must be mocked'),
    resolveFiles: async (query: string) => { queries.push(query); return result },
  })
  const hook = module.exports.useTranscriptFileResolver('fixture-agent', enabled, (value) => opened.push(value), (value) => opened.push(value))
  const event = { target, clientX: 1, clientY: 1, nativeEvent: { composedPath: () => [target] } } as unknown as MouseEvent<HTMLElement>
  return { target, queries, opened, states, selected, ends, caret, range, doubleClick: () => hook.onDoubleClick(event) }
}

const empty: ResolveResponse = { candidates: [], roots: [{ root: '/home/u', status: 'complete' }] }

test('double-click on a path link resolves the decoded href rather than its label', async () => {
  const f = fixture({ ...empty, candidates: [{ root: '/home/u', path: 'x y.md', tier: 'exact', score: 100 }] })
  f.target.href = 'file:///home/u/x%20y.md:12'
  f.target.textContent = 'trail'
  await f.doubleClick()
  assert.deepEqual(f.queries, ['/home/u/x y.md:12'])
  assert.equal(JSON.stringify(f.opened), JSON.stringify([{ root: '/home/u', path: 'x y.md', line: 12 }]))
  assert.deepEqual(f.selected, [f.target, f.range])
})

test('double-click selects both text nodes of a wrapped path before resolving', async () => {
  const f = fixture(empty)
  const first = f.caret.offsetNode
  first.textContent = 'trail (/home/u/proj/'
  const second = new FixtureNode()
  second.textContent = '\nsub/x.md)'
  first.nextSibling = second
  second.previousSibling = first
  f.caret.offsetNode = second
  f.caret.offset = 4
  await f.doubleClick()
  assert.deepEqual(f.queries, ['/home/u/proj/sub/x.md'])
  assert.equal(f.ends.length, 2)
  assert.deepEqual(f.ends[0], [first, 7])
  assert.deepEqual(f.ends[1], [second, 9])
  assert.deepEqual(f.selected, [f.range])
})

test('empty and weak fuzzy matches show the existing popover and preserve root outcomes', async () => {
  for (const result of [empty, { ...empty, candidates: [{ root: '/home/u', path: 'x.md', tier: 'fuzzy' as const, score: 1 }], roots: [{ root: '/home/u', status: 'degraded' as const }] }]) {
    const f = fixture(result)
    f.target.href = '/home/u/missing.md'
    await f.doubleClick()
    const state = f.states.at(-1) as { mention: string, resolution: ResolveResponse }
    assert.equal(state.mention, '/home/u/missing.md')
    assert.equal(state.resolution.candidates.length, 0)
    assert.equal(state.resolution.roots, result.roots)
    assert.equal(f.opened.length, 0)
  }
})

test('anchor gestures and ordinary prose do not resolve; disabled results do not open', async () => {
  const anchor = fixture(empty)
  anchor.target.ignored = true
  anchor.caret.offsetNode.textContent = '/home/u/x.md'
  await anchor.doubleClick()
  assert.equal(anchor.queries.length, 0)
  const prose = fixture(empty)
  prose.caret.offsetNode.textContent = 'ordinary'
  await prose.doubleClick()
  assert.equal(prose.queries.length, 0)
  const disabled = fixture(empty, false)
  disabled.target.href = '/home/u/x.md'
  await disabled.doubleClick()
  assert.equal(disabled.states.at(-1), null)
  assert.equal(disabled.opened.length, 0)
})
