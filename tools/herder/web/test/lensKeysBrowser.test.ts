import assert from 'node:assert/strict'
import test from 'node:test'

import { fixtureAPI, startBrowser } from './browserFixture.ts'

// The lens keys of owner #475682: m toggles read, / replies to the peeked
// agent, the peek is half the lens, ↑↓ go to the closest card a line away.

function entries(agent: string) {
  return [
    { uuid: `${agent}-p0`, line: 1, byteOffset: 0, timestamp: '2026-01-01T10:00:00.000Z', kind: 'human_prompt', payload: { message: { content: 'fixture prompt' } } },
    { uuid: `${agent}-a0`, line: 2, byteOffset: 100, timestamp: '2026-01-01T10:00:01.000Z', kind: 'assistant_text', payload: { message: { content: [{ type: 'text', text: `${agent} answered.` }] } } },
  ]
}

// spaces makes one space per agent from a zoom, then opens the lens.
async function spaces(agents: string[], name: string, context: Parameters<typeof startBrowser>[0], before?: (evaluate: (expression: string) => Promise<unknown>) => Promise<unknown>) {
  const api = fixtureAPI(agents, { entries })
  for (const pane of api.board.workspaces[0]!.tabs[0]!.panes) pane.turn_end_id = 1
  const session = await startBrowser(context, name, api.plugin)
  const { url, browser, evaluate, waitFor } = session
  await browser(['open', `${url}agents/${agents[0]}`])
  await browser(['set', 'viewport', '1400', '900'])
  await evaluate('localStorage.clear(), sessionStorage.clear(), true')
  await before?.(evaluate)
  await browser(['reload'])
  await waitFor(`Boolean(document.querySelector('textarea[data-composer][aria-label="Message ${agents[0]}"]'))`)
  await evaluate(`document.querySelector('.dv-active-group .herder-dock-tab.preview')?.dispatchEvent(new MouseEvent('dblclick', { bubbles: true })), true`)
  for (const [index, agent] of agents.slice(1).entries()) {
    await evaluate(`document.querySelector('.space-create').click(), true`)
    await waitFor(`document.querySelectorAll('.spaces-list .space-row').length === ${index + 2}`)
    await evaluate(`[...document.querySelectorAll('[role="treeitem"]')].find((node) => node.textContent.includes('${agent}')).dispatchEvent(new MouseEvent('dblclick', { bubbles: true })), true`)
    await waitFor(`Boolean(document.querySelector('textarea[data-composer][aria-label="Message ${agent}"]'))`)
  }
  await browser(['open', url])
  await waitFor(`document.querySelectorAll('.lens-card').length === ${agents.length} && Boolean(document.querySelector('.lens-peek'))`)
  // Every agent seeded read at turn 1 before a key is pressed.
  await waitFor(`(() => { const rows = JSON.parse(localStorage.getItem('herder.web.read-markers.v3:rows') ?? '[]'); return ${JSON.stringify(agents)}.every((agent) => rows.find((row) => row.key === agent)?.value?.turn === 1) })()`)
  const press = async (key: string) => { await browser(['press', key]); await browser(['wait', '80']) }
  return { ...session, api, press }
}

const cardOf = (agent: string) => `[...document.querySelectorAll('.lens-card')].find((card) => card.querySelector('.lens-agent')?.textContent === '${agent}')`
const pill = (agent: string) => `(${cardOf(agent)}?.querySelector('.lens-pill')?.textContent ?? null)`
const selectedAgent = `(document.querySelector('.lens-card[aria-current="true"] .lens-agent')?.textContent ?? null)`

test('m marks the selected card unread, then read, then unread again', { timeout: 180_000 }, async (context) => {
  const { evaluate, waitFor, press } = await spaces(['alpha', 'bravo'], 'lens-keys-m', context)
  await evaluate(`${cardOf('alpha')}.click(), true`)
  await waitFor(`${selectedAgent} === 'alpha'`)
  assert.equal(await evaluate(pill('alpha')), null, 'alpha starts read')
  await press('m')
  await waitFor(`${pill('alpha')} === '1'`)
  await press('m')
  await waitFor(`${pill('alpha')} === null`)
  await press('m')
  await waitFor(`${pill('alpha')} === '1'`)
  assert.equal(await evaluate(pill('bravo')), null, 'only the selected card')
  assert.equal(await evaluate(selectedAgent), 'alpha')
})

test('/ replies to the peeked agent: typing stays in the reply, ⌘↵ sends and marks it read, esc keeps the draft, a failure says so', { timeout: 180_000 }, async (context) => {
  const { api, browser, evaluate, waitFor, press } = await spaces(['alpha', 'bravo'], 'lens-keys-reply', context)
  const panes = api.board.workspaces[0]!.tabs[0]!.panes
  const pane = (agent: string) => panes.find((item) => item.agent === agent)!
  const reply = `document.querySelector('.lens-peek-reply textarea[data-composer]')`
  const replyFocused = `document.activeElement === ${reply} && document.activeElement !== null`
  const lensFocused = `document.activeElement?.classList.contains('lens')`
  const peekAgent = `(document.querySelector('.lens-peek-agent')?.textContent ?? null)`
  const draft = `localStorage.getItem('herder.web.messageDraft.v1:alpha')`

  // alpha has a new turn: unread, its card counts it.
  pane('alpha').turn_end_id = 2
  api.send('fleet', api.board)
  await evaluate(`${cardOf('alpha')}.click(), true`)
  await waitFor(`${selectedAgent} === 'alpha' && ${peekAgent} === 'alpha' && ${pill('alpha')} === '1'`)
  assert.equal(await evaluate(`Boolean(${reply})`), false, 'no reply until /')

  // / opens the reply at the peek's foot with the keys; card keys type into it.
  await press('/')
  await waitFor(replyFocused)
  assert.equal(await evaluate(`${reply}.value`), '', 'the / itself is not typed')
  assert.equal(await evaluate(`document.querySelector('.lens-peek').lastElementChild.classList.contains('lens-peek-reply')`), true, 'at the foot of the peek')
  await browser(['keyboard', 'type', 'ok jkm 12 v'])
  await press('ArrowDown')
  await press('ArrowRight')
  assert.equal(await evaluate(`${reply}.value`), 'ok jkm 12 v')
  assert.equal(await evaluate(selectedAgent), 'alpha', 'typing and arrows move no selection')
  assert.equal(await evaluate(pill('alpha')), '1', 'm typed is no mark')
  assert.equal(await evaluate(`document.querySelector('.lens-row-focus .lens-card') === null`), true, '1 typed places nothing')

  // esc: the reply closes, the draft is kept, the keys are the cards'.
  await press('Escape')
  await waitFor(`!${reply} && ${lensFocused}`)
  assert.equal(await evaluate(draft), 'ok jkm 12 v', 'the draft is stored')
  assert.equal(await evaluate('Boolean(document.querySelector(".lens"))'), true, 'esc in the reply does not leave the lens')
  await press('/')
  await waitFor(replyFocused)
  assert.equal(await evaluate(`${reply}.value`), 'ok jkm 12 v', '/ brings the draft back')

  // A failed send keeps the text, says so, and marks nothing read.
  api.failSends('the bus is down')
  await press('Control+Enter')
  await waitFor(`Boolean(document.querySelector('.lens-peek-reply [role="alert"]')?.textContent.includes('the bus is down'))`)
  assert.equal(await evaluate(`${reply}.value`), 'ok jkm 12 v')
  assert.equal(await evaluate(pill('alpha')), '1', 'still unread')
  assert.equal(api.messages.length, 0)

  // ⌘↵ sends to alpha; once it lands alpha is read, the reply closes and the cards have the keys on alpha still.
  api.failSends(null)
  await evaluate(`${reply}.focus(), true`)
  await press('Control+Enter')
  await waitFor(`!${reply} && ${pill('alpha')} === null`)
  assert.deepEqual(api.messages, [{ agent: 'alpha', text: 'ok jkm 12 v' }])
  assert.equal(await evaluate(selectedAgent), 'alpha', 'the same card stays selected')
  assert.equal(await evaluate(lensFocused), true, 'the keys are the cards\'')
  assert.equal(await evaluate(`${draft} ?? ''`), '', 'the sent draft is cleared')
  await press('ArrowRight')
  await waitFor(`${selectedAgent} === 'bravo'`)

  // With no agent on the peek, / is what it was: nothing on the lens.
  api.board.workspaces[0]!.tabs[0]!.panes = panes.filter((item) => item.agent !== 'bravo')
  api.send('fleet', api.board)
  await waitFor(`(document.querySelector('.lens-peek')?.textContent ?? '').includes('No agent on the board')`)
  await evaluate(`document.querySelector('.lens').focus(), true`)
  await press('/')
  assert.equal(await evaluate(`Boolean(document.querySelector('.lens-peek textarea'))`), false, 'no reply without an agent')
  assert.equal(await evaluate(lensFocused), true, 'the keys stay with the cards')
  assert.equal(await evaluate('Boolean(document.querySelector(".lens"))'), true)
})

test('the peek is half the lens, even for a browser that stored the old width; resizing still persists', { timeout: 180_000 }, async (context) => {
  const { browser, evaluate, waitFor, press } = await spaces(['alpha', 'bravo'], 'lens-keys-peek', context)
  const share = `document.querySelector('.lens-peek').getBoundingClientRect().width / document.querySelector('.lens').getBoundingClientRect().width`
  // A record from before: the old peek field at 32%, its rows and text kept.
  await evaluate(`localStorage.setItem('herder.web.lens.v1', JSON.stringify({ version: 1, rows: {}, visible: {}, text: 'status', peek: 0.32 })), true`)
  await browser(['reload'])
  await waitFor(`document.querySelectorAll('.lens-card').length === 2 && Boolean(document.querySelector('.lens-peek'))`)
  assert.equal(await evaluate(`document.querySelector('.lens-peek').style.width`), '50%')
  assert.ok(Math.abs(await evaluate(share) as number - 0.5) < 0.01, 'half the lens')
  assert.match(await evaluate(`document.querySelector('.lens-card .lens-body').textContent`) as string, /^(your turn|idle)/, 'the rest of the record is kept')
  assert.equal(JSON.parse(await evaluate(`localStorage.getItem('herder.web.lens.v1')`) as string).peek, undefined, 'the old field is gone once written')
  // At half the cards beside it still fit the widest focus card (440).
  const room = `document.querySelector('.lens-grid').getBoundingClientRect().width`
  assert.ok(await evaluate(room) as number >= 440, `room for the cards at 50%: ${await evaluate(room)}`)
  // The edge's keys still resize it, and it persists.
  await evaluate(`document.querySelector('.lens-peek-resizer').focus(), true`)
  await press('ArrowLeft')
  assert.equal(await evaluate(`document.querySelector('.lens-peek').style.width`), '52%')
  await press('Escape')
  await browser(['reload'])
  await waitFor(`document.querySelector('.lens-peek')?.style.width === '52%'`)
  // At the 60% most they still fit the default focus card (284).
  await evaluate(`document.querySelector('.lens-peek-resizer').focus(), true`)
  for (let step = 0; step < 6; step += 1) await press('ArrowLeft')
  assert.equal(await evaluate(`document.querySelector('.lens-peek').style.width`), '60%')
  assert.ok(await evaluate(room) as number >= 284, `room for the cards at 60%: ${await evaluate(room)}`)
})

test('↓ ↑ go to the closest card on the next drawn line, the watch row\'s wrapped line included', { timeout: 240_000 }, async (context) => {
  const { browser, evaluate, waitFor, press } = await spaces(['alpha', 'bravo', 'charlie', 'delta'], 'lens-keys-arrows', context)
  // alpha in focus; bravo, charlie and delta in watch, the peek at 30% so watch wraps two to a line.
  await evaluate(`${cardOf('alpha')}.click(), true`)
  await press('1')
  await waitFor(`document.querySelector('.lens-row-focus .lens-agent')?.textContent === 'alpha'`)
  await evaluate(`localStorage.setItem('herder.web.lens.v1', JSON.stringify({ ...JSON.parse(localStorage.getItem('herder.web.lens.v1')), peekWidth: 0.3 })), true`)
  await browser(['reload'])
  await waitFor(`document.querySelector('.lens-peek')?.style.width === '30%' && document.querySelectorAll('.lens-card').length === 4`)
  const top = (agent: string) => `Math.round(${cardOf(agent)}.getBoundingClientRect().top)`
  const left = (agent: string) => `Math.round(${cardOf(agent)}.getBoundingClientRect().left)`
  assert.equal(await evaluate(top('bravo')), await evaluate(top('charlie')), 'bravo and charlie share watch\'s first line')
  assert.ok(await evaluate(top('delta')) as number > (await evaluate(top('charlie')) as number), 'delta wraps onto a second')
  assert.equal(await evaluate(left('delta')), await evaluate(left('bravo')), 'under bravo')

  const select = async (agent: string) => {
    await evaluate(`${cardOf(agent)}.click(), true`)
    await waitFor(`${selectedAgent} === '${agent}'`)
  }
  const step = async (key: string, from: string, to: string, why: string) => {
    await select(from)
    await press(key)
    assert.equal(await evaluate(selectedAgent), to, `${key} from ${from}: ${why}`)
  }
  await step('ArrowDown', 'charlie', 'delta', 'the wrapped line, not the empty background row')
  await step('ArrowUp', 'delta', 'bravo', 'the line above in the same row, not the focus row')
  await step('ArrowUp', 'charlie', 'alpha', 'the only card on the focus line')
  await step('ArrowDown', 'alpha', 'bravo', 'the closest by centre')
  await step('j', 'bravo', 'delta', 'j is ↓')
  await step('k', 'bravo', 'alpha', 'k is ↑')
  await step('ArrowDown', 'delta', 'delta', 'the bottom edge stays')
  await step('ArrowUp', 'alpha', 'alpha', 'the top edge stays')
  await step('ArrowRight', 'bravo', 'charlie', '→ is unchanged')
})
