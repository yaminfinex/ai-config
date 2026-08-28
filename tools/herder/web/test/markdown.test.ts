import assert from 'node:assert/strict'
import test from 'node:test'
import { createElement } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { Markdown, fileMarkdownComponents } from '../src/shared/Markdown.ts'

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
})
