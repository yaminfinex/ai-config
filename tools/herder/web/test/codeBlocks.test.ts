import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { createElement } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { FencedBlock } from '../src/shared/CodeBlock.ts'
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

test('mermaid loads lazily with strict security and the app theme; the main bundle does not import it statically', () => {
  const loader = read('../src/shared/mermaidRender.ts')
  assert.match(loader, /import\('mermaid'\)/)
  assert.match(loader, /securityLevel: 'strict'/)
  assert.match(loader, /startOnLoad: false/)
  assert.match(loader, /theme: theme === 'light' \? 'default' : 'dark'/)
  assert.match(loader, /mermaid\.render\(`herder-mermaid-\$\{renderSerial\}`, source\)/)
  assert.doesNotMatch(read('../src/shared/CodeBlock.ts') + read('../src/shared/Markdown.ts'), /from 'mermaid'/)
  assert.equal(JSON.parse(read('../package.json')).dependencies.mermaid, '11.17.2')
})
