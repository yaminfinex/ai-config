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
