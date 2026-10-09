import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

import type { Plugin } from 'vite'

import { fixtureAPI, renderCounter, startBrowser } from './browserFixture.ts'

// Synthetic transcripts long enough to scroll, with markdown in every reply.
function entries(agent: string) {
  return Array.from({ length: 60 }, (_, index) => {
    const base = { uuid: `${agent}-${index}`, line: index + 1, byteOffset: index * 100, timestamp: new Date(Date.UTC(2026, 0, 1, 10, 0, index)).toISOString() }
    return index % 2
      ? { ...base, kind: 'assistant_text', payload: { message: { content: [{ type: 'text', text: `## Finding ${index}\n\nfixture prose for ${agent} with \`code ${index}\` and **bold**\n\n- one\n- two\n\n| case | count |\n|---|---|\n| ${index} | ${index * 2} |` }] } } }
      : { ...base, kind: 'human_prompt', payload: { message: { content: `fixture prompt ${index}` } } }
  })
}

// countParses counts react-markdown's parses on the page: the prebundled
// module bumps window.__markdownParses each time it builds a processor.
const countParses: Plugin = {
  name: 'count-markdown-parses',
  config: () => ({
    optimizeDeps: {
      esbuildOptions: {
        plugins: [{
          name: 'count-markdown-parses',
          setup(build) {
            build.onLoad({ filter: /react-markdown[\\/]lib[\\/]index\.js$/ }, async ({ path }) => {
              const source = await readFile(path, 'utf8')
              const anchor = 'const processor = createProcessor(options)'
              assert.ok(source.includes(anchor), 'react-markdown still builds one processor per parse')
              return { contents: source.replace(anchor, `globalThis.__markdownParses = (globalThis.__markdownParses ?? 0) + 1; ${anchor}`), loader: 'js' }
            })
          },
        }],
      },
    },
  }),
}

test('a space revisit reuses parsed markdown and keeps transcript scroll', { timeout: 180_000 }, async (context) => {
  const api = fixtureAPI(['alpha', 'bravo', 'charlie'], { entries })
  const plugin: Plugin = { ...api.plugin, ...countParses, name: 'space-switch-fixture', transformIndexHtml: () => [{ tag: 'script', children: renderCounter, injectTo: 'head-prepend' }] }
  const { url, browser, evaluate, waitFor } = await startBrowser(context, 'space-switch', plugin)
  const composer = (name: string) => `Boolean(document.querySelector('textarea[data-composer][aria-label="Message ${name}"]'))`
  const open = (name: string, side: boolean) => evaluate(`(() => {
    const row = [...document.querySelectorAll('[role="treeitem"]')].find((node) => node.textContent.includes('${name}'))
    row.dispatchEvent(new MouseEvent('dblclick', { bubbles: true, altKey: ${side} }))
    return true
  })()`)
  const transcriptsReady = (count: number) => `document.querySelectorAll('.transcript').length === ${count} && [...document.querySelectorAll('.transcript')].every((node) => node.querySelectorAll('article').length > 0)`
  const switchTo = (index: number) => evaluate(`document.querySelectorAll('.spaces-list .space-name')[${index}].click(), true`)
  const alphaTranscript = `document.querySelector('textarea[aria-label="Message alpha"]').closest('.dv-groupview').querySelector('.transcript')`
  await browser(['open', `${url}agents/alpha`])
  await browser(['set', 'viewport', '1400', '900'])
  await evaluate('localStorage.clear(), sessionStorage.clear(), true')
  await browser(['reload'])
  await waitFor(composer('alpha'))
  // Space one: alpha with bravo in a side group.
  await open('bravo', true)
  await waitFor(`${composer('alpha')} && ${composer('bravo')} && ${transcriptsReady(2)}`)
  await evaluate(`(() => { const view = ${alphaTranscript}; view.scrollTop = 400; view.dispatchEvent(new Event('scroll')); return true })()`)
  await browser(['wait', '500'])
  const scrolled = await evaluate(`${alphaTranscript}.scrollTop`) as number
  assert.ok(scrolled > 0 && scrolled < (await evaluate(`${alphaTranscript}.scrollHeight - ${alphaTranscript}.clientHeight - 100`) as number), `alpha scrolls away from the bottom: ${scrolled}`)

  // Space two: a first visit, so charlie's markdown parses.
  await evaluate('window.__markdownParses = 0, true')
  await evaluate(`document.querySelector('.space-create').click(), true`)
  await waitFor(`document.querySelectorAll('.spaces-list .space-row').length === 2`)
  await open('charlie', false)
  await waitFor(`${composer('charlie')} && ${transcriptsReady(1)}`)
  await browser(['wait', '1000'])
  assert.ok((await evaluate('window.__markdownParses') as number) > 0, 'a first visit parses its markdown, so the counter is live')

  // Back to space one: the revisit rebuilds its panels from parsed markdown.
  await evaluate('window.__markdownParses = 0, window.__renders = {}, window.__track = true, true')
  await switchTo(0)
  await waitFor(`${composer('alpha')} && ${composer('bravo')} && ${transcriptsReady(2)}`)
  await browser(['wait', '1000'])
  const { parses, renders } = await evaluate('window.__track = false, { parses: window.__markdownParses, renders: window.__renders }') as { parses: number, renders: Record<string, number> }
  assert.ok((renders['mount:Block'] ?? 0) > 0, `the revisit mounts its transcript entries: ${JSON.stringify(renders)}`)
  assert.ok(await evaluate(`${alphaTranscript}.textContent.includes('fixture prose for alpha')`), 'alpha shows its markdown')
  assert.equal(parses, 0, `the revisit parses no markdown: ${parses}`)
  assert.equal(await evaluate(`${alphaTranscript}.scrollTop`), scrolled, 'alpha keeps its scroll position')
})
