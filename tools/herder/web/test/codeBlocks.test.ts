import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { createElement } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { readdirSync } from 'node:fs'
import { parseAst } from 'rollup/parseAst'
import { FencedBlock, diagramKey, diagramView } from '../src/shared/CodeBlock.ts'
import { createMermaidRenderer, intrinsicWidth, renderLeniently, type MermaidLike } from '../src/shared/mermaidRender.ts'
import { Markdown, fileMarkdownComponents, agentMarkdownOptions } from '../src/shared/Markdown.ts'
import { agentMentionMatcher } from '../src/shared/agentMentions.ts'
import { fenceInfo, lenientMermaid, lenientNotice, mermaidKeywords, mermaidLike, mermaidNotice, mermaidSourceLimit } from '../src/shared/mermaidLike.ts'

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

test('intrinsicWidth pins the root svg to its natural width and drops the inline max-width so the stylesheet fit applies', () => {
  const wide = '<svg id="x" width="100%" xmlns="http://www.w3.org/2000/svg" class="flowchart" style="max-width: 6517.78125px;" viewBox="0 0 6517.78125 70" role="graphics-document document"><g/></svg>'
  const out = intrinsicWidth(wide)
  assert.equal(out, '<svg id="x" width="6517.78125" xmlns="http://www.w3.org/2000/svg" class="flowchart" viewBox="0 0 6517.78125 70" role="graphics-document document"><g/></svg>')
  assert.doesNotMatch(out, /width="100%"|max-width/)
  const small = '<svg id="y" width="100%" style="max-width: 207.34375px;" viewBox="0 0 207.34375 70"><rect width="100%"/></svg>'
  assert.equal(intrinsicWidth(small), '<svg id="y" width="207.34375" viewBox="0 0 207.34375 70"><rect width="100%"/></svg>')
  // pie and gantt put the viewBox before the style; other declarations in the style survive
  const pie = '<svg id="p" width="100%" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 522 450" style="background: red; max-width: 522px; color: blue" role="graphics-document document"><g/></svg>'
  assert.equal(intrinsicWidth(pie), '<svg id="p" width="522" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 522 450" style="background: red; color: blue" role="graphics-document document"><g/></svg>')
  const fixed = '<svg id="z" width="300" height="100"><g/></svg>'
  assert.equal(intrinsicWidth(fixed), fixed)
  const css = read('../src/styles.css')
  assert.match(css, /\.mermaid-diagram svg \{ display: block; max-width: 100%; height: auto; \}/, 'inline fit: a wide diagram scales down to the box, a narrow one keeps its width')
  assert.doesNotMatch(css, /\.mermaid-diagram \{[^}]*overflow-x/, 'no horizontal scroll inside the block')
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
  // a lenient render carries both: drawn with its notice
  const lenient = { key, svg: '<svg/>', notice: lenientNotice(2) }
  assert.deepEqual(diagramView({ mode: 'diagram', key, result: lenient, tooLarge: false }), { drawn: true, notice: 'mermaid: drawn after escaping 2 characters that mermaid rejects', svg: '<svg/>' })
  assert.deepEqual(diagramView({ mode: 'source', key, result: lenient, tooLarge: false }), { drawn: false, notice: undefined, svg: undefined })
  const component = read('../src/shared/CodeBlock.ts')
  assert.match(component, /renderLeniently\(renderMermaid, source, theme\)/)
  assert.match(component, /setResult\(\{ key, svg, notice \}\)/)
  assert.match(component, /setResult\(\{ key, notice: mermaidNotice\(error\) \}\)/)
  assert.match(component, /const view = diagramView\(\{ mode, key, result, tooLarge \}\)/)
})

const fixture = `sequenceDiagram
    participant D as device
    participant S as AccountService
    participant DB
    participant T as Turnkey
    D->>S: login_challenge(eoa address, device P-256 pubkey)
    S->>DB: credential → account, state must be ready
    S->>DB: turnkey_org + device signer → SubOrgTarget (shape == 3, venue key == config)
    S->>S: author CREATE_API_KEYS_V2 body for Device user (key "device", TTL 7d+5m)
    S->>S: author SIWE message that embeds that body
    S->>DB: insert login_challenge (both messages, 5 min)
    S-->>D: challenge, siwe_message, signing_session_message
    D->>D: EIP-191 sign both
    D->>S: login(challenge, siwe_sig, signing_session_sig)
    S->>DB: load challenge; unconsumed, unexpired, kind eoa, account ready
    S->>S: verify both signatures recover the credential
    S->>DB: burn challenge (UPDATE consumed_at, must affect 1 row)
    S->>T: submit stored body under EIP-191 stamp (Owner's one vote, login-mints-device)
    T-->>S: api_key_id
    S->>DB: tx: session row (api_key_id), device_minted event, refresh hash, access JWT
    S-->>D: SessionCredentials
`

test('lenientMermaid escapes a mid-text semicolon in sequence messages and notes, and nothing else', () => {
  // mermaid 11.17.2 rejects the owner's fixture at line 15 ("load challenge; unconsumed": Parse error, got ','); `#59;` is the fix
  const fixed = lenientMermaid(fixture)
  assert.equal(fixed.changes, 1)
  assert.equal(fixed.source, fixture.replace('load challenge; unconsumed', 'load challenge#59; unconsumed'))
  assert.equal(lenientNotice(fixed.changes), 'mermaid: drawn after escaping 1 character that mermaid rejects')
  const cases: Array<[string, string, string, number]> = [
    ['a valid message line is unchanged', 'sequenceDiagram\n  A->>B: hi\n', 'sequenceDiagram\n  A->>B: hi\n', 0],
    ['a trailing semicolon is the legal terminator and stays', 'sequenceDiagram\n  A->>B: hi;\n  A-->>B: ok ; \n', 'sequenceDiagram\n  A->>B: hi;\n  A-->>B: ok ; \n', 0],
    ['two interior semicolons on one line, the trailing one kept', 'sequenceDiagram\n  A->>B: a; b; c;\n', 'sequenceDiagram\n  A->>B: a#59; b#59; c;\n', 2],
    ['every arrow kind and activation marker', 'sequenceDiagram\n  A->B: a; b\n  A-->B: a; b\n  A-xB: a; b\n  A--xB: a; b\n  A-)B: a; b\n  A--)B: a; b\n  A<<->>B: a; b\n  A->>+B: a; b\n  B-->>-A: a; b\n', 'sequenceDiagram\n  A->B: a#59; b\n  A-->B: a#59; b\n  A-xB: a#59; b\n  A--xB: a#59; b\n  A-)B: a#59; b\n  A--)B: a#59; b\n  A<<->>B: a#59; b\n  A->>+B: a#59; b\n  B-->>-A: a#59; b\n', 9],
    ['a second colon belongs to the text', 'sequenceDiagram\n  S->>DB: tx: a; b\n', 'sequenceDiagram\n  S->>DB: tx#59; a; b\n'.replace('tx#59; a; b', 'tx: a#59; b'), 1],
    ['note text is tokenised the same way', 'sequenceDiagram\n  A->>B: hi\n  Note over A,B: a; b\n  note right of A: c; d\n', 'sequenceDiagram\n  A->>B: hi\n  Note over A,B: a#59; b\n  note right of A: c#59; d\n', 2],
    ['an entity already escaped is left alone', 'sequenceDiagram\n  A->>B: a #59; b; c\n', 'sequenceDiagram\n  A->>B: a #59; b#59; c\n', 1],
    ['structural lines are never touched', 'sequenceDiagram\n  participant A as x; y\n  loop a; b\n  A->>B: hi\n  end\n', 'sequenceDiagram\n  participant A as x; y\n  loop a; b\n  A->>B: hi\n  end\n', 0],
    ['flowchart labels accept semicolons, so a flowchart is returned as is', 'flowchart LR\n  A[load; go] --> B{c; d}\n  B -->|x; y| C\n', 'flowchart LR\n  A[load; go] --> B{c; d}\n  B -->|x; y| C\n', 0],
    ['other diagram types are returned as is', 'stateDiagram-v2\n  [*] --> A: a; b\n', 'stateDiagram-v2\n  [*] --> A: a; b\n', 0],
    ['leading blank lines before the keyword', '\n\n  sequenceDiagram\n  A->>B: a; b\n', '\n\n  sequenceDiagram\n  A->>B: a#59; b\n', 1],
  ]
  for (const [name, source, expected, changes] of cases) assert.deepEqual(lenientMermaid(source), { source: expected, changes }, name)
  assert.equal(lenientNotice(3), 'mermaid: drawn after escaping 3 characters that mermaid rejects')
  assert.doesNotMatch(read('../src/shared/mermaidLike.ts'), /<[a-z]|innerHTML/u, 'the sanitiser never introduces HTML')
})

test('renderLeniently retries a rejected source once with the rewrite and keeps the original error otherwise', async () => {
  const log: string[] = []
  const mermaid = stubMermaid(log)
  const rejects = (source: string) => /[^#\d];(?!\s*$)/mu.test(source)
  mermaid.render = async (id, source) => {
    log.push(`render:${id}`)
    if (source === 'bad' || rejects(source)) throw new Error(`Parse error on line 2:\n${source}`)
    return { svg: `<svg id="${id}">${source}</svg>` }
  }
  const render = createMermaidRenderer(async () => mermaid)
  // valid: one render, no notice
  assert.deepEqual(await renderLeniently(render, 'sequenceDiagram\n  A->>B: hi', 'dark'), { svg: '<svg id="herder-mermaid-1">sequenceDiagram\n  A->>B: hi</svg>' })
  // rejected then fixed: two renders, the drawn source is the rewrite, the notice says so
  assert.deepEqual(await renderLeniently(render, 'sequenceDiagram\n  A->>B: a; b', 'dark'), { svg: '<svg id="herder-mermaid-3">sequenceDiagram\n  A->>B: a#59; b</svg>', notice: 'mermaid: drawn after escaping 1 character that mermaid rejects' })
  // rejected with nothing to rewrite: the original error, one render
  await assert.rejects(renderLeniently(render, 'bad', 'dark'), /Parse error on line 2:\nbad/)
  // rejected, rewritten, rejected again: the original error
  await assert.rejects(renderLeniently(render, 'sequenceDiagram\n  participant A as x; y\n  A->>B: a; b', 'dark'), /Parse error on line 2:\nsequenceDiagram\n {2}participant A as x; y\n {2}A->>B: a; b$/)
  assert.deepEqual(log.map((entry) => entry.split(':')[1]), ['init', 'herder-mermaid-1', 'herder-mermaid-2', 'herder-mermaid-3', 'herder-mermaid-4', 'herder-mermaid-5', 'herder-mermaid-6'].map((entry) => entry === 'init' ? 'dark' : entry))
})

test('DiagramBlock shows the maximise button beside the mode switch only when the diagram is drawn', () => {
  const node = { type: 'element', tagName: 'pre', children: [{ type: 'element', tagName: 'code', properties: { className: ['language-mermaid'] }, children: [{ type: 'text', value: 'graph TD\n' }] }] }
  const child = createElement('code', { className: 'language-mermaid' }, 'graph TD\n')
  const key = diagramKey('dark', 'graph TD\n')
  const drawn = renderToStaticMarkup(createElement(FencedBlock, { node: node as never, initialResult: { key, svg: '<svg id="d"></svg>' } }, child))
  assert.match(drawn, /<div class="code-block-controls"><div class="detail-toggle code-block-mode" role="group" aria-label="Block rendering mode">.*<\/div><button type="button" class="code-block-maximise" aria-label="Maximise diagram">maximise<\/button><\/div><div class="mermaid-diagram"><svg id="d"><\/svg><\/div>/)
  assert.doesNotMatch(drawn, /diagram-overlay/, 'closed until the button is pressed')
  const lenient = renderToStaticMarkup(createElement(FencedBlock, { node: node as never, initialResult: { key, svg: '<svg id="d"></svg>', notice: lenientNotice(1) } }, child))
  assert.match(lenient, /aria-label="Maximise diagram">maximise<\/button><\/div><div class="code-block-notice" role="status">mermaid: drawn after escaping 1 character that mermaid rejects<\/div><div class="mermaid-diagram">/)
  const pending = renderToStaticMarkup(createElement(FencedBlock, { node: node as never }, child))
  assert.doesNotMatch(pending, /code-block-maximise/)
  const failed = renderToStaticMarkup(createElement(FencedBlock, { node: node as never, initialResult: { key, notice: 'mermaid: Parse error on line 1:' } }, child))
  assert.doesNotMatch(failed, /code-block-maximise/)
  assert.match(failed, /code-block-notice/)
  const source = renderToStaticMarkup(createElement(FencedBlock, { node: node as never, initialMode: 'source', initialResult: { key, svg: '<svg id="d"></svg>' } }, child))
  assert.doesNotMatch(source, /code-block-maximise|mermaid-diagram/)
  const component = read('../src/shared/CodeBlock.ts')
  assert.match(component, /createPortal\(createElement\(DiagramOverlay, \{ svg: view\.svg as string, onClose: close \}\), document\.body\)/)
  assert.match(component, /const close = \(\) => \{ setOpen\(false\); maximise\.current\?\.focus\(\) \}/, 'focus returns to the maximise button on close')
  assert.match(component, /open && view\.drawn \? createPortal/, 'the overlay closes with the drawing')
  const css = read('../src/styles.css')
  assert.match(css, /\.code-block-controls \{ position: absolute; z-index: 1; top: 4px; right: 4px; display: flex;/)
  assert.match(css, /\.code-block-diagram > pre \{ padding-right: 200px; \}/)
})
