import assert from 'node:assert/strict'
import test from 'node:test'

import type { Plugin } from 'vite'

import { fixtureAPI, startBrowser } from './browserFixture.ts'

// Synthetic transcripts with markdown, so a re-render of an open transcript
// shows up as TranscriptEntries and Markdown work.
function entries(agent: string) {
  return Array.from({ length: 12 }, (_, index) => {
    const base = { uuid: `${agent}-${index}`, line: index + 1, byteOffset: index * 100, timestamp: new Date(Date.UTC(2026, 0, 1, 10, 0, index)).toISOString() }
    return index % 2
      ? { ...base, kind: 'assistant_text', payload: { message: { content: [{ type: 'text', text: `## Finding ${index}\n\nfixture prose with \`code ${index}\` and **bold**\n\n- one\n- two` }] } } }
      : { ...base, kind: 'human_prompt', payload: { message: { content: `fixture prompt ${index}` } } }
  })
}

// A minimal React devtools hook, installed before React loads, that counts
// each component that rendered in a commit: mounted, or updated with work
// performed. Subtrees React bailed out of are skipped.
const renderCounter = `(() => {
  window.__renders = {}
  window.__commits = 0
  window.__track = false
  const label = (fiber) => {
    const type = fiber.type
    if (typeof type === 'function') return type.displayName || type.name || null
    if (type && typeof type === 'object') { const inner = type.render || type.type; return type.displayName || inner?.displayName || inner?.name || null }
    return null
  }
  const count = (name) => { window.__renders[name] = (window.__renders[name] ?? 0) + 1 }
  const walk = (root) => {
    const stack = root ? [root] : []
    while (stack.length) {
      const fiber = stack.pop()
      const name = [0, 1, 11, 14, 15].includes(fiber.tag) ? label(fiber) : null
      const mounted = fiber.alternate === null
      if (name && mounted) count('mount:' + name)
      else if (name && (fiber.flags & 1)) count(name)
      if (fiber.sibling) stack.push(fiber.sibling)
      if (fiber.child && (mounted || fiber.child !== fiber.alternate.child)) stack.push(fiber.child)
    }
  }
  window.__REACT_DEVTOOLS_GLOBAL_HOOK__ = {
    supportsFiber: true, renderers: new Map(), isDisabled: false,
    inject(renderer) { const id = this.renderers.size + 1; this.renderers.set(id, renderer); return id },
    onCommitFiberRoot(_id, root) { if (!window.__track) return; window.__commits++; walk(root.current.child) },
    onCommitFiberUnmount() {}, onPostCommitFiberRoot() {}, checkDCE() {}, on() {}, off() {}, emit() {}, sub() { return () => {} },
  }
})()`

// setDraft sets the composer's value through React's input path.
const setDraft = (selector: string, text: string) => `(() => {
  const field = ${selector}
  field.focus()
  Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value').set.call(field, ${JSON.stringify(text)})
  field.dispatchEvent(new Event('input', { bubbles: true }))
  return true
})()`

test('a draft flip and a fleet refresh leave open transcripts unrendered', { timeout: 180_000 }, async (context) => {
  const api = fixtureAPI(['alpha', 'bravo'], { entries })
  // The hook goes ahead of the React refresh preamble, which wraps it.
  const plugin: Plugin = { ...api.plugin, transformIndexHtml: () => [{ tag: 'script', children: renderCounter, injectTo: 'head-prepend' }] }
  const { url, browser, evaluate, waitFor } = await startBrowser(context, 'transcript-render', plugin)
  const composer = `document.querySelector('textarea[data-composer][aria-label="Message alpha"]')`
  const transcripts = `document.querySelectorAll('.transcript article').length`
  await browser(['open', `${url}agents/alpha`])
  await browser(['set', 'viewport', '1400', '900'])
  await evaluate('localStorage.clear(), true')
  await browser(['reload'])
  await waitFor(`Boolean(${composer}) && ${transcripts} > 0`)
  // Open bravo in a side group, so two transcripts are on screen.
  await evaluate(`(() => {
    const row = [...document.querySelectorAll('[role="treeitem"]')].find((node) => node.textContent.includes('bravo'))
    row.dispatchEvent(new MouseEvent('dblclick', { bubbles: true, altKey: true }))
    return true
  })()`)
  await waitFor(`document.querySelectorAll('.dv-groupview').length === 2 && Boolean(document.querySelector('textarea[data-composer][aria-label="Message bravo"]')) && Boolean(${composer})`)
  await waitFor(`document.querySelectorAll('.transcript').length === 2 && [...document.querySelectorAll('.transcript')].every((node) => node.querySelectorAll('article').length > 0)`)
  await browser(['wait', '1000'])

  const bravoTab = `[...document.querySelectorAll('.herder-dock-tab')].find((node) => node.textContent.includes('bravo'))`
  assert.equal(await evaluate(`Boolean(${bravoTab}?.querySelector('.status-dot.listening'))`), true)
  const draftMark = `Boolean([...document.querySelectorAll('.herder-dock-tab')].find((node) => node.textContent.includes('alpha'))?.querySelector('.draft-mark'))`
  assert.equal(await evaluate(draftMark), false)

  await evaluate('window.__renders = {}, window.__commits = 0, window.__track = true, true')
  await evaluate(setDraft(composer, 'a fixture draft'))
  await waitFor(draftMark)
  assert.equal(await evaluate(`localStorage.getItem('herder.web.messageDraft.v1:alpha')`), 'a fixture draft')
  await evaluate(setDraft(composer, ''))
  await waitFor(`!${draftMark}`)
  assert.equal(await evaluate(`localStorage.getItem('herder.web.messageDraft.v1:alpha')`), null)

  api.board.workspaces[0].tabs[0].panes[1].bus_status = 'active'
  api.send('fleet', api.board)
  await waitFor(`Boolean(${bravoTab}?.querySelector('.status-dot.active'))`)
  await browser(['wait', '1000'])
  const { commits, renders } = await evaluate('window.__track = false, { commits: window.__commits, renders: window.__renders }') as { commits: number, renders: Record<string, number> }

  // The controls: the draft and the board did render.
  assert.ok(commits > 0, 'the draft flip and the fleet refresh commit')
  assert.ok((renders.Composer ?? 0) > 0, `the composer renders on its draft: ${JSON.stringify(renders)}`)
  // Neither open transcript renders, mounts or re-parses its markdown.
  assert.equal(renders.TranscriptEntries ?? 0, 0, `TranscriptEntries rendered: ${JSON.stringify(renders)}`)
  assert.equal(renders['mount:TranscriptEntries'] ?? 0, 0, `TranscriptEntries remounted: ${JSON.stringify(renders)}`)
  assert.equal(renders.Markdown ?? 0, 0, `Markdown rendered: ${JSON.stringify(renders)}`)
  assert.equal(await evaluate(`${composer}.value`), '')
})
