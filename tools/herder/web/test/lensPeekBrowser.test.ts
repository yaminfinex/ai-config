import assert from 'node:assert/strict'
import test from 'node:test'

import { fixtureAPI, startBrowser } from './browserFixture.ts'

const answers: Record<string, string[]> = {
  alpha: ['The alpha gate is green.'],
  bravo: ['Bravo has nothing pending.'],
}
function entries(agent: string) {
  return (answers[agent] ?? []).flatMap((text, index) => [
    { uuid: `${agent}-p${index}`, line: index * 2 + 1, byteOffset: index * 200, timestamp: '2026-01-01T10:00:00.000Z', kind: 'human_prompt', payload: { message: { content: `fixture prompt ${index}` } } },
    { uuid: `${agent}-a${index}`, line: index * 2 + 2, byteOffset: index * 200 + 100, timestamp: '2026-01-01T10:00:01.000Z', kind: 'assistant_text', payload: { message: { content: [{ type: 'text', text }] } } },
  ])
}

const cardOf = (agent: string) => `[...document.querySelectorAll('.lens-card')].find((card) => card.querySelector('.lens-agent')?.textContent === '${agent}')`
const pill = (agent: string) => `(${cardOf(agent)}?.querySelector('.lens-pill')?.textContent ?? null)`
const peekAgent = `(document.querySelector('.lens-peek-agent')?.textContent ?? null)`
const peekText = `(document.querySelector('.lens-peek')?.textContent ?? '')`
const lensFocused = `document.activeElement?.classList.contains('lens')`

test('the peek follows the selection and the stream, reads nothing, views no block, leaves the keys with the lens, and a zoom still reads', { timeout: 240_000 }, async (context) => {
  const api = fixtureAPI(['alpha', 'bravo'], { entries })
  const panes = api.board.workspaces[0]!.tabs[0]!.panes
  const pane = (agent: string) => panes.find((item) => item.agent === agent)!
  for (const item of panes) item.turn_end_id = 1
  const { url, browser, evaluate, waitFor } = await startBrowser(context, 'lens-peek', api.plugin)
  const press = async (key: string) => { await browser(['press', key]); await browser(['wait', '80']) }
  const marker = async (agent: string) => await evaluate(`(JSON.parse(localStorage.getItem('herder.web.read-markers.v3:rows') ?? '[]').find((row) => row.key === '${agent}')?.value ?? null)`) as { turn?: number } | null
  const viewedBlocks = async () => await evaluate(`JSON.parse(localStorage.getItem('herder.web.viewed-blocks.v1') ?? '{"agents":[]}').agents`) as string[]

  // Two spaces made in a zoom: alpha in the first, bravo in a second.
  await browser(['open', `${url}agents/alpha`])
  await browser(['set', 'viewport', '1400', '900'])
  await evaluate('localStorage.clear(), sessionStorage.clear(), true')
  await browser(['reload'])
  await waitFor(`Boolean(document.querySelector('textarea[data-composer][aria-label="Message alpha"]'))`)
  await evaluate(`document.querySelector('.dv-active-group .herder-dock-tab.preview')?.dispatchEvent(new MouseEvent('dblclick', { bubbles: true })), true`)
  await evaluate(`document.querySelector('.space-create').click(), true`)
  await waitFor(`document.querySelectorAll('.spaces-list .space-row').length === 2`)
  await evaluate(`[...document.querySelectorAll('[role="treeitem"]')].find((node) => node.textContent.includes('bravo')).dispatchEvent(new MouseEvent('dblclick', { bubbles: true })), true`)
  await waitFor(`Boolean(document.querySelector('textarea[data-composer][aria-label="Message bravo"]'))`)

  // A fresh page on the lens; nothing is viewed there, so mark the page hidden-proof by waiting on seeding.
  await browser(['open', url])
  await waitFor(`document.querySelectorAll('.lens-card').length === 2 && Boolean(document.querySelector('.lens-peek'))`)
  for (let tries = 0; tries < 40 && (await marker('alpha'))?.turn !== 1; tries += 1) await browser(['wait', '100'])
  assert.equal((await marker('alpha'))?.turn, 1, 'alpha seeded read at turn 1')
  const lensWidth = await evaluate(`document.querySelector('.lens').getBoundingClientRect().width`) as number
  const peekWidth = await evaluate(`document.querySelector('.lens-peek').getBoundingClientRect().width`) as number
  assert.ok(Math.abs(peekWidth / lensWidth - 0.5) < 0.01, `the peek takes half the lens: ${peekWidth}/${lensWidth}`)
  assert.equal(await evaluate(`document.querySelector('.lens-peek').getBoundingClientRect().left >= document.querySelector('.lens-rows').getBoundingClientRect().right`), true, 'on the right of the cards')
  assert.equal(await evaluate(`document.querySelector('.lens-peek').getBoundingClientRect().right <= document.querySelector('.utility-rail-right').getBoundingClientRect().left`), true, 'the notes rail stays outermost')
  assert.equal(await evaluate(`Boolean(document.querySelector('.lens-peek textarea, .lens-peek .dv-tab, .lens-peek [data-composer]'))`), false, 'no composer, no dock chrome')

  // The selection moves the peek: a click, the arrows, n.
  await evaluate(`${cardOf('alpha')}.click(), true`)
  await waitFor(`${peekAgent} === 'alpha' && ${peekText}.includes('The alpha gate is green.')`)
  assert.match(await evaluate(peekText) as string, /alpha.*main/, 'the header names the agent and its space')
  assert.equal(await evaluate(lensFocused), true, 'selecting never moves the keys to the peek')
  await press('ArrowRight')
  await waitFor(`${peekAgent} === 'bravo' && ${peekText}.includes('Bravo has nothing pending.')`)
  assert.equal(await evaluate(lensFocused), true)
  await press('ArrowLeft')
  await waitFor(`${peekAgent} === 'alpha'`)

  // It follows the agent live, through the shared cache.
  answers.alpha!.push('A live line from alpha.')
  api.send('entry:alpha', {})
  await waitFor(`${peekText}.includes('A live line from alpha.')`)

  // A new turn makes alpha unread; peeking at it well past the dwell reads nothing.
  pane('alpha').turn_end_id = 2
  api.send('fleet', api.board)
  await waitFor(`${pill('alpha')} === '1'`)
  await press('ArrowRight')
  await press('n')
  await waitFor(`${peekAgent} === 'alpha'`)
  // A click inside the peek (to scroll or select) leaves the lens keys working.
  await evaluate(`document.querySelector('.lens-peek-body').dispatchEvent(new MouseEvent('click', { bubbles: true })), true`)
  assert.equal(await evaluate(lensFocused), true)
  await press('ArrowRight')
  await waitFor(`${peekAgent} === 'bravo'`)
  await press('ArrowLeft')
  await waitFor(`${peekAgent} === 'alpha'`)
  await browser(['wait', '2500'])
  assert.equal((await marker('alpha'))?.turn, 1, 'a peeked unread agent stays unread')
  assert.equal(await evaluate(pill('alpha')), '1', 'and keeps its card count')
  assert.match(await evaluate(`document.querySelector('.lens-header').textContent`) as string, /1 need you/)

  // A peeked blocked agent stays needs-you, its block not viewed.
  pane('bravo').bus_status = 'blocked'
  api.send('fleet', api.board)
  await waitFor(`${pill('bravo')} === '1'`)
  await press('ArrowRight')
  await waitFor(`${peekAgent} === 'bravo'`)
  await browser(['wait', '2500'])
  assert.equal(await evaluate(pill('bravo')), '1', 'a peeked blocked agent stays needs-you')
  assert.deepEqual(await viewedBlocks(), [], 'its block is not viewed')

  // The resize edge: its arrows resize, esc gives the keys back, and the width persists.
  await evaluate(`document.querySelector('.lens-peek-resizer').focus(), true`)
  await press('ArrowLeft')
  assert.equal(await evaluate(`document.querySelector('.lens-peek').style.width`), '52%')
  assert.equal(await evaluate(peekAgent), 'bravo', 'the edge keeps its arrows from the cards')
  await press('Escape')
  assert.equal(await evaluate(lensFocused), true, 'esc returns the keys to the cards')
  await press('ArrowLeft')
  await waitFor(`${peekAgent} === 'alpha'`)
  const stored = JSON.parse(await evaluate(`localStorage.getItem('herder.web.lens.v1')`) as string) as { peekWidth: number }
  assert.equal(stored.peekWidth, 0.52)
  await browser(['reload'])
  await waitFor(`document.querySelector('.lens-peek')?.style.width === '52%' && Boolean(${peekAgent})`)
  await evaluate(`${cardOf('alpha')}.click(), true`)
  await waitFor(`${peekAgent} === 'alpha'`)
  assert.equal((await marker('alpha'))?.turn, 1, 'still unread after the reload')

  // Zooming in and dwelling still reads, as today.
  await evaluate('document.activeElement?.blur(), true')
  await press('Enter')
  await waitFor(`Boolean(document.querySelector('textarea[data-composer][aria-label="Message alpha"]'))`)
  for (let tries = 0; tries < 40 && (await marker('alpha'))?.turn !== 2; tries += 1) await browser(['wait', '100'])
  assert.equal((await marker('alpha'))?.turn, 2, 'a zoom dwell reads')

  // A card with no agent on the board. (The first esc leaves the composer.)
  await press('Escape')
  await press('Escape')
  await waitFor(`Boolean(document.querySelector('.lens-peek'))`)
  api.board.workspaces[0]!.tabs[0]!.panes = panes.filter((item) => item.agent !== 'bravo')
  api.send('fleet', api.board)
  await evaluate(`document.querySelectorAll('.lens-card')[1].click(), true`)
  await waitFor(`${peekText}.includes('No agent on the board')`)
})
