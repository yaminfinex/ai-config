import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { createElement } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { Markdown, agentMarkdownOptions, fileMarkdownComponents } from '../src/shared/Markdown.ts'
import { agentMentionMatcher } from '../src/shared/agentMentions.ts'
import type { Board } from '../src/types.ts'

test('truncated markdown ending inside a code fence renders without repair or a crash', () => {
  const truncated = '# Current\n\n```ts\nconst unfinished = true'
  const html = renderToStaticMarkup(createElement(Markdown, { components: fileMarkdownComponents }, truncated))
  assert.match(html, /<h1>Current<\/h1>/)
  assert.match(html, /const unfinished = true/)
})

test('file markdown keeps relative targets visible without navigating', () => {
  const markdown = '[details](docs/details.md) ![diagram](images/flow.png) [web](https://example.com)'
  const html = renderToStaticMarkup(createElement(Markdown, { components: fileMarkdownComponents }, markdown))
  assert.doesNotMatch(html, /href="docs\/details\.md"/)
  assert.match(html, /docs\/details\.md/)
  assert.doesNotMatch(html, /src="images\/flow\.png"/)
  assert.match(html, /diagram/)
  assert.match(html, /images\/flow\.png/)
  assert.match(html, /href="https:\/\/example\.com" target="_blank" rel="noopener noreferrer"/)
  assert.match(html, /<a class="inline-link" href="https:\/\/example\.com"/)
})

test('agent mentions, Markdown links, and bare URLs share the inline link idiom', () => {
  const board: Board = {
    workspaces: [{
      workspace_id: 'w1', number: 1, label: 'fixture', focused: true, pane_count: 1, tab_count: 1,
      active_tab_id: 't1', agent_status: 'active', tabs: [{
        tab_id: 't1', number: 1, label: 'agents', focused: true, pane_count: 1, agent_status: 'active',
        panes: [{ pane_id: 'p1', agent: 'grill-kila', tool: 'codex', herdr_status: 'active', bus_status: 'active', gap: '-' }],
      }],
    }],
    unplaced: [],
  }
  const html = renderToStaticMarkup(createElement(
    Markdown,
    agentMarkdownOptions(agentMentionMatcher(board), () => undefined),
    'Ask grill-kila, read [the docs](https://docs.example.com), or visit https://example.com.',
  ))
  assert.match(html, /<button[^>]*class="inline-link agent-mention"[^>]*>grill-kila<\/button>/)
  assert.match(html, /<a class="inline-link" href="https:\/\/docs\.example\.com">the docs<\/a>/)
  assert.match(html, /<a class="inline-link" href="https:\/\/example\.com">https:\/\/example\.com<\/a>/)
  const css = readFileSync(new URL('../src/styles.css', import.meta.url), 'utf8')
  assert.match(css, /\.inline-link \{[^}]*color: var\(--accent-text\);[^}]*text-decoration[^}]*underline/s)
})

test('agent markdown transforms parsed text nodes but skips code, existing links, and raw html', () => {
  const board: Board = {
    workspaces: [{
      workspace_id: 'w1', number: 1, label: 'fixture', focused: true, pane_count: 1, tab_count: 1,
      active_tab_id: 't1', agent_status: 'active', tabs: [{
        tab_id: 't1', number: 1, label: 'agents', focused: true, pane_count: 1, agent_status: 'active',
        panes: [{ pane_id: 'p1', agent: 'grill-kila', tool: 'codex', herdr_status: 'active', bus_status: 'active', gap: '-' }],
      }],
    }],
    unplaced: [],
  }
  const matcher = agentMentionMatcher(board)
  const opened: string[] = []
  const markdown = 'Ask **grill-kila** or @kila. `grill-kila` [grill-kila](https://example.com) <span>grill-kila</span>\n\n```txt\ngrill-kila\n```'
  const html = renderToStaticMarkup(createElement(Markdown, agentMarkdownOptions(matcher, (name) => opened.push(name)), markdown))
  assert.match(html, /<strong><button[^>]*>grill-kila<\/button><\/strong>/)
  assert.match(html, /<button[^>]*>@kila<\/button>/)
  assert.match(html, /<code>grill-kila<\/code>/)
  assert.match(html, /<a class="inline-link" href="https:\/\/example.com">grill-kila<\/a>/)
  assert.match(html, /&lt;span&gt;grill-kila&lt;\/span&gt;/)
  assert.match(html, /<pre><code class="language-txt">grill-kila\n<\/code><\/pre>/)
  assert.equal(opened.length, 0)
})

test('agent markdown treats malformed and non-roster internal links as inert text', () => {
  const board: Board = {
    workspaces: [{
      workspace_id: 'w1', number: 1, label: 'fixture', focused: true, pane_count: 1, tab_count: 1,
      active_tab_id: 't1', agent_status: 'active', tabs: [{
        tab_id: 't1', number: 1, label: 'agents', focused: true, pane_count: 1, agent_status: 'active',
        panes: [{ pane_id: 'p1', agent: 'grill-kila', tool: 'codex', herdr_status: 'active', bus_status: 'active', gap: '-' }],
      }],
    }],
    unplaced: [],
  }
  const options = agentMarkdownOptions(agentMentionMatcher(board), () => assert.fail('inert links must not open'))
  const html = renderToStaticMarkup(createElement(Markdown, options, '[bad](herder-agent:%ZZ) [retired](herder-agent:nelo) [live](herder-agent:grill-kila)'))
  assert.match(html, /<span>bad<\/span>/)
  assert.match(html, /<span>retired<\/span>/)
  assert.match(html, /<button[^>]*>live<\/button>/)
})
