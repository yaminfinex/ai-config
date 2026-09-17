import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { createElement } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { readdirSync } from 'node:fs'
import { parseAst } from 'rollup/parseAst'
import { FencedBlock, diagramKey, diagramView } from '../src/shared/CodeBlock.ts'
import { createMermaidRenderer, intrinsicWidth, type MermaidLike } from '../src/shared/mermaidRender.ts'
import { Markdown, fileMarkdownComponents, agentMarkdownOptions } from '../src/shared/Markdown.ts'
import { agentMentionMatcher } from '../src/shared/agentMentions.ts'
import { fenceInfo, mermaidKeywords, mermaidLike, mermaidNotice, mermaidSourceLimit } from '../src/shared/mermaidLike.ts'

const read = (path: string) => readFileSync(new URL(path, import.meta.url), 'utf8')
const render = (markdown: string) => renderToStaticMarkup(createElement(Markdown, { components: fileMarkdownComponents }, markdown))

test('mermaidLike: tagged mermaid, untagged diagram keywords, and everything else', () => {
  const cases: Array<[string | undefined, string, boolean]> = [
    ['mermaid', 'anything at all', true],
    ['Mermaid', 'graph TD', true],
    ['ts', 'graph TD; A-->B', false],
    ['text', 'sequenceDiagram', false],
    ['text', 'graph TD\n  A --> B', false],
    ['txt', 'graph TD\n  A --> B', false],
    ['plain', 'graph TD\n  A --> B', false],
    ['md', 'flowchart LR', false],
    [undefined, '', false],
    [undefined, '\n\n  stateDiagram-v2\n  [*] --> a', true],
    [undefined, 'graph TD; A-->B', true],
    [undefined, 'graphql { a }', false],
    [undefined, 'pie title Pets', true],
    [undefined, 'piece of text', false],
    [undefined, 'flowchart LR', true],
    [undefined, 'C4Context', true],
    [undefined, 'xychart-beta', true],
    [undefined, 'const graph = 1', false],
    ['', 'gantt', true],
  ]
  for (const [lang, source, expected] of cases) assert.equal(mermaidLike(lang, source), expected, `${lang ?? '<none>'} / ${JSON.stringify(source)}`)
  for (const keyword of mermaidKeywords) assert.equal(mermaidLike(undefined, `${keyword}\n`), true, keyword)
  assert.equal(mermaidKeywords.length, 19)
})

test('fenceInfo reads the tag and text from the hast pre node; mermaidNotice keeps one line', () => {
  const node = { type: 'element', tagName: 'pre', children: [{ type: 'element', tagName: 'code', properties: { className: ['language-mermaid'] }, children: [{ type: 'text', value: 'graph TD\n' }] }] }
  assert.deepEqual(fenceInfo(node), { lang: 'mermaid', source: 'graph TD\n' })
  assert.deepEqual(fenceInfo({ type: 'element', tagName: 'pre', children: [{ type: 'element', tagName: 'code', properties: {}, children: [{ type: 'text', value: 'x' }] }] }), { lang: undefined, source: 'x' })
  assert.deepEqual(fenceInfo(undefined), { lang: undefined, source: '' })
  assert.equal(mermaidNotice(new Error('Parse error on line 2:\n...^\nExpecting X')), 'mermaid: Parse error on line 2:')
  assert.equal(mermaidNotice('boom'), 'mermaid: boom')
  assert.equal(mermaidSourceLimit, 20_000)
})

test('a ```mermaid block renders the mode switch, defaults to diagram, and shows the source until the SVG lands', () => {
  const html = render('Before\n\n```mermaid\nstateDiagram-v2\n  [*] --> a\n```\n\nAfter')
  assert.match(html, /<div class="code-block code-block-diagram" data-mode="diagram">/)
  assert.match(html, /<div class="detail-toggle code-block-mode" role="group" aria-label="Block rendering mode">/)
  assert.match(html, /<button type="button" class="active" aria-pressed="true">diagram<\/button><button type="button" aria-pressed="false">source<\/button>/)
  assert.match(html, /<pre><code class="language-mermaid">stateDiagram-v2\n {2}\[\*\] --&gt; a\n<\/code><\/pre>/)
  assert.doesNotMatch(html, /mermaid-diagram|code-block-notice/)
})

test('the switch swaps modes: source mode flips aria-pressed and keeps the plain pre', () => {
  const node = { type: 'element', tagName: 'pre', children: [{ type: 'element', tagName: 'code', properties: { className: ['language-mermaid'] }, children: [{ type: 'text', value: 'graph TD\n' }] }] }
  const child = createElement('code', { className: 'language-mermaid' }, 'graph TD\n')
  const diagram = renderToStaticMarkup(createElement(FencedBlock, { node: node as never, initialMode: 'diagram' }, child))
  const source = renderToStaticMarkup(createElement(FencedBlock, { node: node as never, initialMode: 'source' }, child))
  assert.match(diagram, /data-mode="diagram"/)
  assert.match(diagram, /aria-pressed="true">diagram<\/button><button type="button" aria-pressed="false">source/)
  assert.match(source, /data-mode="source"/)
  assert.match(source, /aria-pressed="false">diagram<\/button><button type="button" class="active" aria-pressed="true">source/)
  assert.match(source, /<pre><code class="language-mermaid">graph TD\n<\/code><\/pre>/)
  const component = read('../src/shared/CodeBlock.ts')
  assert.match(component, /onClick: \(\) => setMode\(value\)/)
  assert.match(component, /useState<BlockMode>\(initialMode\)/)
})

test('an untagged flowchart is detected; a tagged non-diagram block gets no switch and no notice', () => {
  const flow = render('```\nflowchart LR\n  a --> b\n```')
  assert.match(flow, /code-block-diagram/)
  const ts = render('```ts\nconst x = 1\n```')
  assert.match(ts, /<div class="code-block"><pre><code class="language-ts">const x = 1\n<\/code><\/pre><\/div>/)
  assert.doesNotMatch(ts, /aria-pressed|code-block-mode/)
})

test('an oversized diagram block renders as source with the too-large notice and keeps the switch', () => {
  const html = render('```mermaid\ngraph TD\n' + 'A-->B\n'.repeat(4_000) + '```')
  assert.match(html, /<div class="code-block-notice" role="status">mermaid: block too large<\/div>/)
  assert.match(html, /aria-pressed="true">diagram/)
  assert.match(html, /<pre><code class="language-mermaid">graph TD/)
})

test('GFM tables render inside a scroll wrapper with collapsed borders on every cell in both themes', () => {
  const html = render('| a | b |\n| - | - |\n| 1 | 2 |')
  assert.match(html, /<div class="table-scroll"><table><thead><tr><th>a<\/th><th>b<\/th><\/tr><\/thead><tbody><tr><td>1<\/td><td>2<\/td><\/tr><\/tbody><\/table><\/div>/)
  const mentioned = renderToStaticMarkup(createElement(Markdown, agentMarkdownOptions(agentMentionMatcher({ workspaces: [], unplaced: [] }), () => undefined), '| a |\n| - |\n| 1 |'))
  assert.match(mentioned, /<div class="table-scroll"><table>/)
  const css = read('../src/styles.css')
  assert.match(css, /\.markdown table \{ border-collapse: collapse;/)
  assert.match(css, /\.markdown th, \.markdown td \{ padding: var\(--space-1\) var\(--space-2\); border: 1px solid var\(--border\);/)
  assert.match(css, /\.markdown th \{ background: var\(--bg3\);/)
  assert.match(css, /\.markdown \.table-scroll \{ max-width: 100%; margin: 6px 0; overflow-x: auto; \}/)
  assert.doesNotMatch(css, /\.markdown table \{ display: block/)
  assert.doesNotMatch(css, /\.transcript table \{ display: block/)
  const dark = css.slice(css.indexOf('[data-theme="dark"]') > 0 ? css.indexOf('[data-theme="dark"]') : css.indexOf('dark'))
  assert.match(dark, /--border: #2e3037/)
})

test('mermaid loads lazily: the built main chunk has no static import of mermaid.core and none of its symbols', () => {
  const assets = new URL('../../internal/webui/dist/assets/', import.meta.url)
  const names = readdirSync(assets)
  const main = names.find((name) => /^index-.*\.js$/u.test(name))
  const core = names.filter((name) => name.startsWith('mermaid.core'))
  assert.ok(main, 'built main chunk present')
  assert.equal(core.length, 1, `one mermaid.core chunk, got ${core.join(', ')}`)
  const code = readFileSync(new URL(main as string, assets), 'utf8')
  const ast = parseAst(code) as unknown as { body: Array<{ type: string, source?: { value?: unknown } }> }
  const staticSources = ast.body.filter((node) => node.type === 'ImportDeclaration').map((node) => String(node.source?.value ?? ''))
  assert.deepEqual(staticSources.filter((source) => /mermaid/u.test(source)), [], 'no static import of any mermaid chunk')
  assert.ok(code.includes(`./${core[0]}`), 'the main chunk references the mermaid.core chunk (by dynamic import) so the lazy path is wired')
  assert.equal(code.includes('mermaidAPI'), false, 'the mermaid.core-only symbol mermaidAPI must not be in the main chunk')
  assert.equal(readFileSync(new URL(core[0], assets), 'utf8').includes('mermaidAPI'), true)
  const loader = read('../src/shared/mermaidRender.ts')
  assert.match(loader, /import\('mermaid'\)/)
  assert.doesNotMatch(loader, /^import .* from 'mermaid'/mu)
  assert.match(loader, /securityLevel: 'strict'/)
  assert.match(loader, /startOnLoad: false/)
  assert.match(loader, /theme: theme === 'light' \? 'default' : 'dark'/)
  assert.doesNotMatch(read('../src/shared/CodeBlock.ts') + read('../src/shared/Markdown.ts'), /from 'mermaid'/)
  assert.equal(JSON.parse(read('../package.json')).dependencies.mermaid, '11.17.2')
})

function stubMermaid(log: string[], delay = 0): MermaidLike {
  let theme = 'unset'
  return {
    initialize: (config) => { theme = config.theme; log.push(`init:${config.theme}`) },
    render: async (id, source) => {
      log.push(`render:${id}:${theme}`)
      if (delay) await new Promise((resolve) => setTimeout(resolve, delay))
      if (source === 'bad') throw new Error('Parse error on line 1:\nmore')
      return { svg: `<svg id="${id}" data-theme="${theme}">${source}</svg>` }
    },
  }
}

test('renders are serialized: concurrent dark and light requests each see their own theme and fresh ids', async () => {
  const log: string[] = []
  let loads = 0
  const render = createMermaidRenderer(async () => { loads += 1; return stubMermaid(log, 5) })
  const [dark, light, dark2] = await Promise.all([render('a', 'dark'), render('a', 'light'), render('a', 'dark')])
  assert.match(dark, /data-theme="dark"/)
  assert.match(light, /data-theme="default"/)
  assert.match(dark2, /data-theme="dark"/)
  assert.deepEqual(log, ['init:dark', 'render:herder-mermaid-1:dark', 'init:default', 'render:herder-mermaid-2:default', 'init:dark', 'render:herder-mermaid-3:dark'])
  assert.equal(loads, 1)
  // no cache: the same source and theme renders again with a new id
  assert.match(await render('a', 'dark'), /id="herder-mermaid-4"/)
  assert.equal(log.at(-1), 'render:herder-mermaid-4:dark')
})

test('a failed render rejects with its error and does not block the queue', async () => {
  const log: string[] = []
  const render = createMermaidRenderer(async () => stubMermaid(log))
  await assert.rejects(render('bad', 'dark'), /Parse error on line 1:/)
  assert.match(await render('ok', 'dark'), /id="herder-mermaid-2"/)
})

test('intrinsicWidth pins the root svg to its natural max-width so wide diagrams scroll and small ones keep their size', () => {
  const wide = '<svg id="x" width="100%" xmlns="http://www.w3.org/2000/svg" class="flowchart" style="max-width: 6517.78125px;" viewBox="0 0 6517.78125 70" role="graphics-document document"><g/></svg>'
  const out = intrinsicWidth(wide)
  assert.match(out, /^<svg id="x" width="6517\.78125" xmlns=/)
  assert.doesNotMatch(out, /width="100%"/)
  const small = '<svg id="y" width="100%" style="max-width: 207.34375px;" viewBox="0 0 207.34375 70"><rect width="100%"/></svg>'
  assert.match(intrinsicWidth(small), /^<svg id="y" width="207\.34375" style="max-width: 207\.34375px;"/)
  assert.match(intrinsicWidth(small), /<rect width="100%"\/>/)
  const fixed = '<svg id="z" width="300" height="100"><g/></svg>'
  assert.equal(intrinsicWidth(fixed), fixed)
})

test('diagramView draws only a result for the current source and theme; a pending change shows the source', () => {
  const key = diagramKey('dark', 'graph TD\nA-->B')
  const done = { key, svg: '<svg/>' }
  assert.deepEqual(diagramView({ mode: 'diagram', key, result: done, tooLarge: false }), { drawn: true, notice: undefined, svg: '<svg/>' })
  // source changed while the new render is pending: the old SVG must not be shown
  const changed = diagramKey('dark', 'sequenceDiagram\nA->>B: hi')
  assert.deepEqual(diagramView({ mode: 'diagram', key: changed, result: done, tooLarge: false }), { drawn: false, notice: undefined, svg: undefined })
  // theme changed: same rule
  assert.equal(diagramView({ mode: 'diagram', key: diagramKey('light', 'graph TD\nA-->B'), result: done, tooLarge: false }).drawn, false)
  // an error notice is also tied to its key
  const failed = { key, notice: 'mermaid: Parse error on line 1:' }
  assert.equal(diagramView({ mode: 'diagram', key, result: failed, tooLarge: false }).notice, 'mermaid: Parse error on line 1:')
  assert.equal(diagramView({ mode: 'diagram', key: changed, result: failed, tooLarge: false }).notice, undefined)
  assert.equal(diagramView({ mode: 'source', key, result: failed, tooLarge: false }).notice, undefined)
  assert.deepEqual(diagramView({ mode: 'diagram', key, result: done, tooLarge: true }), { drawn: false, notice: 'mermaid: block too large', svg: undefined })
  assert.equal(diagramView({ mode: 'diagram', key, result: undefined, tooLarge: false }).drawn, false)
  const component = read('../src/shared/CodeBlock.ts')
  assert.match(component, /setResult\(\{ key, svg \}\)/)
  assert.match(component, /setResult\(\{ key, notice: mermaidNotice\(error\) \}\)/)
  assert.match(component, /const view = diagramView\(\{ mode, key, result, tooLarge \}\)/)
})
