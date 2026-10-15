import assert from 'node:assert/strict'
import test from 'node:test'

import { fixtureAPI, startBrowser } from './browserFixture.ts'

// lead's transcript mentions mavu, so a mention click has something to open.
function entries(agent: string) {
  const text = agent === 'lead' ? 'hand this to @mavu when you are ready' : `${agent} here`
  return [{ uuid: `${agent}-0`, line: 1, byteOffset: 0, timestamp: '2026-01-01T10:00:00.000Z', kind: 'assistant_text', payload: { message: { content: [{ type: 'text', text }] } } }]
}

// Owner #463121: arriving at an agent focuses its composer the same way
// every time, and a zoom returns to the agent last focused in the space.
test('every way of arriving at an agent lands in its composer; a zoom returns to the agent last had', { timeout: 240_000 }, async (context) => {
  // lone is on the board in no space, and blocked: N reaches it alone.
  const api = fixtureAPI(['lead', 'mavu', 'alpha', 'bravo', 'lone'], { entries })
  const lone = api.board.workspaces[0]!.tabs[0]!.panes.find((pane) => pane.agent === 'lone')!
  lone.bus_status = 'blocked'
  const { url, browser, evaluate, waitFor } = await startBrowser(context, 'arrive', api.plugin)
  const focused = `document.activeElement?.matches?.('textarea[data-composer]') ? document.activeElement.getAttribute('aria-label') : document.activeElement?.className ?? null`
  const onLens = `Boolean(document.querySelector('.lens'))`
  const crumb = `document.querySelector('.lens-crumb-path')?.textContent ?? ''`
  const press = async (key: string) => { await browser(['press', key]); await browser(['wait', '80']) }
  // arrived waits for the composer of name to hold the focus, and names the path in the failure.
  const arrived = async (name: string, path: string) => {
    await waitFor(`document.activeElement?.getAttribute('aria-label') === 'Message ${name}'`).catch(() => undefined)
    assert.equal(await evaluate(focused), `Message ${name}`, `${path} lands in ${name}'s composer`)
  }
  // tag marks the first element matching a test for a real pointer click.
  const tag = (selector: string, text: string, name: string) => evaluate(`(() => {
    const node = [...document.querySelectorAll(${JSON.stringify(selector)})].find((candidate) => candidate.textContent.includes(${JSON.stringify(text)}))
    node?.setAttribute('data-test-click', ${JSON.stringify(name)})
    return Boolean(node)
  })()`)
  const click = async (selector: string, text: string) => {
    assert.ok(await tag(selector, text, text), `${selector} ${text} is drawn`)
    await browser(['click', `[data-test-click="${text}"]`])
    await evaluate(`document.querySelector('[data-test-click="${text}"]')?.removeAttribute('data-test-click'), true`)
  }
  const sidebarRow = '.fleet-tree [role="treeitem"]'

  // A page loaded on a zoom arrives there.
  await browser(['open', `${url}agents/lead`])
  await browser(['set', 'viewport', '1400', '900'])
  await evaluate('localStorage.clear(), sessionStorage.clear(), true')
  await browser(['reload'])
  await arrived('lead', 'a page load on a zoom')
  const herder = await evaluate(`document.querySelector('.space-row.active .space-name')?.textContent ?? ''`) as string
  assert.ok(herder)

  // The herder space: lead pinned first, then mavu.
  await evaluate(`document.querySelector('.dv-active-group .herder-dock-tab.preview')?.dispatchEvent(new MouseEvent('dblclick', { bubbles: true })), true`)
  await waitFor(`!document.querySelector('.herder-dock-tab.preview')`)
  // A mention click (⌥: beside) opens mavu in a second group and arrives there; a tab double-click pins it.
  await waitFor(`Boolean(document.querySelector('.agent-mention'))`)
  await evaluate(`document.querySelector('.agent-mention').dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true, altKey: true })), true`)
  await arrived('mavu', 'a mention click')
  await waitFor(`document.querySelectorAll('.dv-groupview').length === 2`)
  await evaluate(`document.querySelector('.dv-active-group .herder-dock-tab.preview')?.dispatchEvent(new MouseEvent('dblclick', { bubbles: true })), true`)

  // A tab click arrives at the tab's agent, in either group.
  await click('.herder-dock-tab', 'lead')
  await arrived('lead', 'a tab click')
  await click('.herder-dock-tab', 'mavu')
  await arrived('mavu', 'a tab click in the other group')

  // A second space holding alpha, then bravo previewed beside it in the same group.
  await evaluate(`document.querySelector('.space-create').click(), true`)
  await waitFor(`document.querySelectorAll('.spaces-list .space-row').length === 2`)
  await evaluate(`(() => {
    const row = [...document.querySelectorAll('${sidebarRow}')].find((node) => node.textContent.includes('alpha'))
    row.dispatchEvent(new MouseEvent('dblclick', { bubbles: true }))
    return true
  })()`)
  await arrived('alpha', 'a sidebar double-click (pin)')
  const second = await evaluate(`document.querySelector('.space-row.active .space-name')?.textContent ?? ''`) as string
  await click(sidebarRow, 'bravo')
  await arrived('bravo', 'a sidebar click (preview)')

  // ⌥←/→ through the group's tabs arrives at each.
  await press('Escape')
  await press('Alt+ArrowLeft')
  await arrived('alpha', '⌥← to the previous tab')

  // The Spaces list: back to the herder space, on mavu, the agent last had there (not lead, its first member).
  await click('.space-name', herder)
  await arrived('mavu', 'a Spaces list click')
  assert.match(await evaluate(crumb) as string, /›mavu$/)

  // Zoom out and back in: mavu again. On main this landed on lead, the first member.
  await press('Escape')
  assert.equal(await evaluate(onLens), false, 'esc in the composer hands the keys to the panel first')
  assert.equal(await evaluate(`document.activeElement?.classList.contains('agent-page') ?? false`), true)
  await press('Escape')
  await waitFor(onLens)
  assert.match(await evaluate(`[...document.querySelectorAll('.lens-card')].find((card) => card.textContent.includes(${JSON.stringify(herder)})).querySelector('.lens-who').textContent`) as string, /lead\+1$/, 'lead is the space\'s first member, mavu its second')
  await press('Enter')
  await arrived('mavu', 'enter on the lens')
  assert.match(await evaluate(crumb) as string, /›mavu$/, 'the zoom returns to the agent last had in the space')

  // ⇧⌥←/→: each space on its last agent.
  await press('Escape')
  await press('Shift+Alt+ArrowRight')
  await arrived('alpha', '⇧⌥→')
  await press('Escape')
  await press('Shift+Alt+ArrowLeft')
  await arrived('mavu', '⇧⌥←')
  await press('Escape')
  await press('Shift+Alt+ArrowRight')
  await arrived('alpha', '⇧⌥→')

  // ⌥Tab flips to the last space.
  await press('Escape')
  await press('Alt+Tab')
  // agent-browser sends no Alt keyup of its own: release the switcher as letting go of ⌥ does.
  await evaluate(`window.dispatchEvent(new KeyboardEvent('keyup', { key: 'Alt' })), true`)
  await arrived('mavu', '⌥Tab')

  // Double-click a card on the lens.
  await press('Escape')
  await press('Escape')
  await waitFor(onLens)
  await evaluate(`[...document.querySelectorAll('.lens-card')].find((card) => card.textContent.includes(${JSON.stringify(second)})).dispatchEvent(new MouseEvent('dblclick', { bubbles: true })), true`)
  await arrived('alpha', 'a card double-click')

  // Back to the lens and forward again: the zoom arrives once more.
  await press('Escape')
  await press('Escape')
  await waitFor(onLens)
  await browser(['back'])
  await waitFor(`!${onLens}`)
  await arrived('alpha', 'back into a zoom')

  // ⌘K on the lens: a space row zooms in and arrives.
  await press('Escape')
  await press('Escape')
  await waitFor(onLens)
  await press('Control+k')
  await waitFor(`document.activeElement?.closest('.quick-open') !== null`)
  await browser(['keyboard', 'type', herder])
  await waitFor(`document.querySelector('.quick-open [aria-selected="true"]')?.textContent === ${JSON.stringify(herder)}`)
  await press('Enter')
  await arrived('mavu', 'a ⌘K space row from the lens')

  // u marks the second space unread on the lens; n in a zoom then zooms there.
  await press('Escape')
  await press('Escape')
  await waitFor(onLens)
  await evaluate(`[...document.querySelectorAll('.lens-card')].find((card) => card.textContent.includes(${JSON.stringify(second)})).click(), true`)
  await press('u')
  await waitFor(`Boolean(document.querySelector('.lens-card.needs'))`)
  await evaluate(`[...document.querySelectorAll('.lens-card')].find((card) => card.textContent.includes(${JSON.stringify(herder)})).dispatchEvent(new MouseEvent('dblclick', { bubbles: true })), true`)
  await arrived('mavu', 'a card double-click')
  await press('Escape')
  await press('n')
  await waitFor(`(${crumb}).includes(${JSON.stringify(second)})`)
  await arrived('alpha', 'n in a zoom')

  // N reaches lone, in no space, alone.
  await press('Escape')
  await press('Escape')
  await waitFor(onLens)
  await evaluate('document.activeElement?.blur(), true')
  let alone = false
  for (let step = 0; step < 4 && !alone; step += 1) {
    await press('Shift+N')
    alone = await evaluate(`Boolean(document.querySelector('.alone-agent'))`) as boolean
    if (!alone) { await press('Escape'); await press('Escape') }
  }
  assert.ok(alone, 'N reaches the agent in no space')
  await arrived('lone', 'N to an agent alone')
})
