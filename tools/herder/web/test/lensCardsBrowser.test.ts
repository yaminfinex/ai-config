import assert from 'node:assert/strict'
import test from 'node:test'

import { fixtureAPI, startBrowser } from './browserFixture.ts'

// Each agent's answers, by turn: a turn end adds one.
const answers: Record<string, string[]> = {
  alpha: ['## Gate\n\nThe **gate** is `green`; see [the brief](https://example.test/brief).\n<status>idle again</status>'],
  bravo: ['<internal>check twice</internal>\nbravo has nothing pending.'],
}
function entries(agent: string) {
  return (answers[agent] ?? []).flatMap((text, index) => [
    { uuid: `${agent}-p${index}`, line: index * 2 + 1, byteOffset: index * 200, timestamp: '2026-01-01T10:00:00.000Z', kind: 'human_prompt', payload: { message: { content: `fixture prompt ${index}` } } },
    { uuid: `${agent}-a${index}`, line: index * 2 + 2, byteOffset: index * 200 + 100, timestamp: '2026-01-01T10:00:01.000Z', kind: 'assistant_text', payload: { message: { content: [{ type: 'text', text }] } } },
  ])
}

// Two spaces made in a zoom: alpha in the first, bravo in a second.
async function twoSpaces(api: ReturnType<typeof fixtureAPI>, name: string, context: Parameters<typeof startBrowser>[0]) {
  const session = await startBrowser(context, name, api.plugin)
  const { url, browser, evaluate, waitFor } = session
  await browser(['open', `${url}agents/alpha`])
  await browser(['set', 'viewport', '1400', '900'])
  await evaluate('localStorage.clear(), sessionStorage.clear(), true')
  await browser(['reload'])
  await waitFor(`Boolean(document.querySelector('textarea[data-composer][aria-label="Message alpha"]'))`)
  await evaluate(`document.querySelector('.dv-active-group .herder-dock-tab.preview')?.dispatchEvent(new MouseEvent('dblclick', { bubbles: true })), true`)
  await evaluate(`document.querySelector('.space-create').click(), true`)
  await waitFor(`document.querySelectorAll('.spaces-list .space-row').length === 2`)
  await evaluate(`(() => {
    const row = [...document.querySelectorAll('[role="treeitem"]')].find((node) => node.textContent.includes('bravo'))
    row.dispatchEvent(new MouseEvent('dblclick', { bubbles: true }))
    return true
  })()`)
  await waitFor(`Boolean(document.querySelector('textarea[data-composer][aria-label="Message bravo"]'))`)
  return session
}

const body = (agent: string) => `[...document.querySelectorAll('.lens-card')].find((card) => card.querySelector('.lens-agent')?.textContent === '${agent}')?.querySelector('.lens-body')?.textContent ?? null`
const cardOf = (agent: string) => `[...document.querySelectorAll('.lens-card')].find((card) => card.querySelector('.lens-agent')?.textContent === '${agent}')`
const onLens = `Boolean(document.querySelector('.lens'))`
// The dock host's running motion: its own animations and its children's.
const motion = `document.getAnimations().filter((animation) => animation.effect?.target?.closest?.('.dock-host')).map((animation) => Object.keys(animation.effect.getKeyframes()[0]).filter((key) => ['clipPath', 'transform', 'opacity'].includes(key)).sort().join('+')).filter(Boolean)`

test('a card shows its agent\'s last answer, read once per turn; t and s cycle and persist', { timeout: 180_000 }, async (context) => {
  const api = fixtureAPI(['alpha', 'bravo'], { entries })
  for (const pane of api.board.workspaces[0]!.tabs[0]!.panes) pane.turn_end_id = 1
  const { url, browser, evaluate, waitFor } = await twoSpaces(api, 'lens-cards', context)
  const press = async (key: string) => { await browser(['press', key]); await browser(['wait', '80']) }

  // A fresh page on the lens: no transcript is cached, so each text card gets one tail read of 12.
  api.entryRequests.length = 0
  await browser(['open', url])
  await waitFor(`${onLens} && document.querySelectorAll('.lens-card').length === 2`)
  await waitFor(`(${body('alpha')}) === 'Gate The gate is green; see the brief.'`)
  assert.equal(await evaluate(body('alpha')), 'Gate The gate is green; see the brief.', 'the answer, fences stripped and flattened')
  await waitFor(`(${body('bravo')}) === 'bravo has nothing pending.'`)
  // The active space's dock, laid out under the lens, loads bravo's whole transcript; a card may answer from it.
  const tails = (agent: string) => api.entryRequests.filter((request) => request.agent === agent && request.limit === 12).length
  assert.equal(tails('alpha'), 1, `one tail read of 12 for alpha: ${JSON.stringify(api.entryRequests)}`)
  assert.ok(tails('bravo') <= 1)

  // The same turn is never read again; a turn end reads the new answer.
  api.send('fleet', api.board)
  await browser(['wait', '400'])
  assert.equal(tails('alpha'), 1, 'a board without a new turn reads nothing')
  answers.alpha!.push('Second **turn** done.')
  api.board.workspaces[0]!.tabs[0]!.panes.find((pane) => pane.agent === 'alpha')!.turn_end_id = 2
  api.send('fleet', api.board)
  await waitFor(`(${body('alpha')}) === 'Second turn done.'`)
  assert.equal(tails('alpha'), 2)

  // t: answer → status → cwd (the workspace's name with no folder) → answer.
  await evaluate('document.activeElement?.blur(), true')
  await press('t')
  await waitFor(`(${body('alpha')}) !== 'Second turn done.'`)
  assert.match(await evaluate(body('alpha')) as string, /^(your turn|idle)$/, 'status text')
  await press('t')
  assert.equal(await evaluate(body('alpha')), 'fixture', 'cwd text: the workspace, with no folder')
  await press('t')
  await waitFor(`(${body('alpha')}) === 'Second turn done.'`)

  // s: the focus size cycles, and watch cards keep the focus width at two lines.
  const shape = (agent: string) => `(() => { const card = ${cardOf(agent)}; return [card.offsetWidth, Number(card.querySelector('.lens-body').style.webkitLineClamp)] })()`
  assert.deepEqual(await evaluate(shape('alpha')), [284, 2], 'a watch card at the first size')
  await evaluate(`${cardOf('alpha')}.click(), true`)
  await press('1')
  await waitFor(`Boolean(document.querySelector('.lens-row-focus .lens-card'))`)
  assert.deepEqual(await evaluate(shape('alpha')), [284, 5])
  await press('s')
  assert.deepEqual(await evaluate(shape('alpha')), [360, 8])
  assert.deepEqual(await evaluate(shape('bravo')), [360, 2], 'watch matches the focus width')
  await press('s')
  assert.deepEqual(await evaluate(shape('alpha')), [440, 12])
  await press('t')
  const stored = JSON.parse(await evaluate(`localStorage.getItem('herder.web.lens.v1')`) as string) as { text: string, size: number }
  assert.equal(stored.text, 'status')
  assert.equal(stored.size, 2)
  await browser(['reload'])
  await waitFor(`${onLens} && Boolean(document.querySelector('.lens-row-focus .lens-card'))`)
  assert.deepEqual(await evaluate(shape('alpha')), [440, 12], 'the size survives a reload')
  assert.match(await evaluate(body('alpha')) as string, /^(your turn|idle)$/, 'and so does the text mode')
})

test('the zoom morphs from its card and back, n and ⇧⌥←/→ swipe, none of it under reduced motion, and focus never waits', { timeout: 180_000 }, async (context) => {
  const api = fixtureAPI(['alpha', 'bravo'], { entries })
  const { url, browser, evaluate, waitFor } = await twoSpaces(api, 'lens-motion', context)
  const press = async (key: string) => { await browser(['press', key]) }
  const composerFocused = (name: string) => `document.activeElement?.getAttribute('aria-label') === 'Message ${name}'`
  // record watches every frame until recorded: the dock host's motion,
  // whether the dock showed over the lens, and whether the composer or the
  // lens already had the keys while it moved. Each motion it sees runs at a
  // quarter speed, so a slow machine still shows whether focus waited for it.
  const record = () => evaluate(`(() => {
    window.__motion = new Set()
    window.__recording = true
    const frame = () => {
      for (const animation of document.getAnimations()) if (animation.effect?.target?.closest?.('.dock-host') && animation.playbackRate === 1) animation.playbackRate = 0.25
      const kinds = ${motion}
      for (const kind of kinds) window.__motion.add(kind)
      if (kinds.length && document.activeElement?.matches('textarea[data-composer], .lens')) window.__motion.add('keys-while-moving')
      if (document.querySelector('.dock-host').dataset.morph) window.__motion.add('over-lens')
      if (window.__recording) requestAnimationFrame(frame)
    }
    requestAnimationFrame(frame)
    return true
  })()`)
  const recorded = async () => {
    await waitFor(`(${motion}).length === 0`)
    await browser(['wait', '50'])
    return evaluate('window.__recording = false, [...window.__motion].sort()')
  }
  await browser(['open', url])
  await waitFor(`${onLens} && document.querySelectorAll('.lens-card').length === 2`)
  await evaluate('document.activeElement?.blur(), true')

  // Enter: the dock grows out of the card (clip and fade); the composer has the keys straight away.
  await evaluate(`${cardOf('alpha')}.click(), true`)
  await record()
  await press('Enter')
  await waitFor(composerFocused('alpha'))
  assert.deepEqual(await recorded(), ['clipPath+opacity', 'keys-while-moving'], 'the zoom-in morph, the composer focused while it runs')
  // esc esc: the lens takes the keys at once while the dock shrinks back into the card over it.
  await press('Escape')
  await record()
  await press('Escape')
  await waitFor(`document.activeElement?.classList.contains('lens')`)
  assert.deepEqual(await recorded(), ['clipPath+opacity', 'keys-while-moving', 'over-lens'], 'the zoom-out morph over the lens, which has the keys')
  assert.equal(await evaluate(`document.querySelector('.dock-host').dataset.morph ?? null`), null, 'the dock goes back under the lens')

  // ⇧⌥→ from a zoom swipes the next space in.
  await press('Enter')
  await waitFor(composerFocused('alpha'))
  await press('Escape')
  await record()
  await press('Shift+Alt+ArrowRight')
  await waitFor(composerFocused('bravo'))
  assert.deepEqual(await recorded(), ['keys-while-moving', 'opacity+transform'], 'the swipe')
  await press('Escape')
  await press('Escape')
  await waitFor(`${onLens} && !document.querySelector('.dock-host').dataset.morph`)

  // Reduced motion: no animation at all, and the same landings.
  await browser(['set', 'media', 'dark', 'reduced-motion'])
  assert.equal(await evaluate(`matchMedia('(prefers-reduced-motion: reduce)').matches`), true)
  await evaluate(`${cardOf('alpha')}.click(), true`)
  await record()
  await press('Enter')
  await waitFor(composerFocused('alpha'))
  await press('Escape')
  await press('Shift+Alt+ArrowRight')
  await waitFor(composerFocused('bravo'))
  await press('Escape')
  await press('Escape')
  await waitFor(`${onLens} && document.activeElement?.classList.contains('lens')`)
  assert.deepEqual(await recorded(), [], 'no morph in or out, no swipe')
})

test('⌘⇧⏎ sends, reads the agent up to the turn the owner saw, and files back to the lens; a failure stays and says why; ⌘⏎ stays', { timeout: 180_000 }, async (context) => {
  const api = fixtureAPI(['alpha', 'bravo'], { entries })
  for (const pane of api.board.workspaces[0]!.tabs[0]!.panes) pane.turn_end_id = 1
  const { browser, evaluate, waitFor } = await twoSpaces(api, 'lens-file-back', context)
  const composer = `document.querySelector('textarea[data-composer][aria-label="Message alpha"]')`
  // alpha's read marker as this browser holds it.
  const marker = async () => await evaluate(`(JSON.parse(localStorage.getItem('herder.web.read-markers.v3:rows') ?? '[]').find((row) => row.key === 'alpha')?.value ?? null)`) as { turn?: number, unread?: boolean } | null
  const typeDraft = async (text: string) => {
    await evaluate(`${composer}.focus(), true`)
    await browser(['keyboard', 'inserttext', text])
  }
  // Back to alpha's space, then hide the page so no dwell reads anything: only ⌘⇧⏎ can.
  await evaluate(`[...document.querySelectorAll('.space-name')].find((node) => !node.closest('.space-row.active')).click(), true`)
  await waitFor(`document.activeElement === ${composer}`)
  for (let tries = 0; tries < 40 && (await marker())?.turn !== 1; tries += 1) await browser(['wait', '100'])
  assert.equal((await marker())?.turn, 1, 'alpha seeded read at turn 1')
  await evaluate(`Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => 'hidden' }), document.dispatchEvent(new Event('visibilitychange')), true`)
  api.board.workspaces[0]!.tabs[0]!.panes.find((pane) => pane.agent === 'alpha')!.turn_end_id = 2
  api.send('fleet', api.board)
  await browser(['wait', '1500'])
  assert.equal((await marker())?.turn, 1, 'unseen: the turn-2 answer stays unread')

  // A failed send stays in the zoom and says why; the draft is kept.
  api.failSends('the bus is down')
  await typeDraft('file this back')
  await browser(['press', 'Control+Shift+Enter'])
  await waitFor(`Boolean(document.querySelector('.send-box .inline-error'))`)
  assert.match(await evaluate(`document.querySelector('.send-box .inline-error').textContent`) as string, /the bus is down/)
  assert.equal(await evaluate(onLens), false, 'a failed send stays')
  assert.equal(await evaluate(`${composer}.value`), 'file this back')
  assert.equal((await marker())?.turn, 1, 'and reads nothing')

  // It lands: read up to turn 2, back on the lens with the space selected and the keys.
  api.failSends(null)
  await evaluate(`${composer}.focus(), true`)
  await browser(['press', 'Control+Shift+Enter'])
  await waitFor(`${onLens} && document.activeElement?.classList.contains('lens')`)
  assert.deepEqual(api.messages, [{ agent: 'alpha', text: 'file this back' }])
  assert.equal(await evaluate(`document.querySelector('.lens-card[aria-current="true"] .lens-agent')?.textContent`), 'alpha', 'the space stays selected')
  assert.equal((await marker())?.turn, 2, 'read up to the turn the owner saw')
  assert.equal((await marker())?.unread, false)
  assert.equal(await evaluate(`Boolean(${cardOf('alpha')}.querySelector('.lens-pill'))`), false, 'the card no longer needs you')

  // Plain ⌘⏎ is unchanged: it sends and stays.
  await browser(['press', 'Enter'])
  await waitFor(`!${onLens} && document.activeElement === ${composer}`)
  await browser(['keyboard', 'inserttext', 'stay here'])
  await browser(['press', 'Control+Enter'])
  await waitFor(`${composer}.value === ''`)
  assert.deepEqual(api.messages.at(-1), { agent: 'alpha', text: 'stay here' })
  assert.equal(await evaluate(onLens), false, '⌘⏎ stays in the zoom')
})

test('⌘⇧⏎ from a blocked agent views its block: the card goes quiet and it leaves "need you"; a failed send changes nothing; a later block needs you again', { timeout: 180_000 }, async (context) => {
  const api = fixtureAPI(['alpha', 'bravo'], { entries })
  for (const pane of api.board.workspaces[0]!.tabs[0]!.panes) pane.turn_end_id = 1
  const pane = (agent: string) => api.board.workspaces[0]!.tabs[0]!.panes.find((row) => row.agent === agent)!
  const { browser, evaluate, waitFor } = await twoSpaces(api, 'lens-file-back-block', context)
  const composer = `document.querySelector('textarea[data-composer][aria-label="Message alpha"]')`
  const viewedBlocks = async () => await evaluate(`JSON.parse(localStorage.getItem('herder.web.viewed-blocks.v1') ?? '{"agents":[]}').agents`) as string[]
  const header = `document.querySelector('.lens-header')?.textContent ?? ''`
  await evaluate(`[...document.querySelectorAll('.space-name')].find((node) => !node.closest('.space-row.active')).click(), true`)
  await waitFor(`document.activeElement === ${composer}`)
  // Hidden, the zoom views nothing, so only ⌘⇧⏎ can view the block.
  await evaluate(`Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => 'hidden' }), document.dispatchEvent(new Event('visibilitychange')), true`)
  pane('alpha').bus_status = 'blocked'
  api.send('fleet', api.board)
  await browser(['wait', '1000'])
  assert.deepEqual(await viewedBlocks(), [], 'blocked and not yet viewed')

  api.failSends('the bus is down')
  await evaluate(`${composer}.focus(), true`)
  await browser(['keyboard', 'inserttext', 'go ahead'])
  await browser(['press', 'Control+Shift+Enter'])
  await waitFor(`Boolean(document.querySelector('.send-box .inline-error'))`)
  assert.deepEqual(await viewedBlocks(), [], 'a failed send views nothing')

  api.failSends(null)
  await evaluate(`${composer}.focus(), true`)
  await browser(['press', 'Control+Shift+Enter'])
  await waitFor(`Boolean(document.querySelector('.lens'))`)
  assert.deepEqual(api.messages, [{ agent: 'alpha', text: 'go ahead' }])
  await waitFor(`!${cardOf('alpha')}.querySelector('.lens-pill')`)
  assert.deepEqual(await viewedBlocks(), ['alpha'], 'the block is viewed')
  assert.match(await evaluate(header) as string, / 0 need you/, 'alpha drops from "need you"')

  // Unblocked, then blocked again: needs you again, as today.
  pane('alpha').bus_status = 'listening'
  api.send('fleet', api.board)
  await waitFor(`JSON.parse(localStorage.getItem('herder.web.viewed-blocks.v1') ?? '{"agents":[]}').agents.length === 0`)
  pane('alpha').bus_status = 'blocked'
  api.send('fleet', api.board)
  await waitFor(`${cardOf('alpha')}.querySelector('.lens-pill')?.textContent === '1'`)
  assert.match(await evaluate(header) as string, / 1 need you/)
})
