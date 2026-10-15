import assert from 'node:assert/strict'
import test from 'node:test'

import { fixtureAPI, startBrowser } from './browserFixture.ts'

test('home is the lens: zoom in and out, [ ] across spaces, N to an agent alone, keys inert while typing', { timeout: 180_000 }, async (context) => {
  // lone is on the board in no space, and blocked: it needs the owner.
  const api = fixtureAPI(['alpha', 'bravo', 'lone'])
  const lone = api.board.workspaces[0]!.tabs[0]!.panes.find((pane) => pane.agent === 'lone')!
  lone.bus_status = 'blocked'
  const { url, browser, evaluate, waitFor } = await startBrowser(context, 'lens-home', api.plugin)
  const composer = (name: string) => `document.querySelector('.dv-active-group textarea[data-composer][aria-label="Message ${name}"]')`
  const onLens = `Boolean(document.querySelector('.lens'))`
  const crumb = `document.querySelector('.lens-crumb')?.textContent ?? ''`
  const leftRail = `getComputedStyle(document.querySelector('.utility-rail-left')).display`
  const cards = `[...document.querySelectorAll('.lens-card')].map((card) => card.dataset.spaceId)`
  const selected = `document.querySelector('.lens-card[aria-current="true"]')?.dataset.spaceId ?? null`
  const press = async (key: string) => { await browser(['press', key]); await browser(['wait', '80']) }

  // Two spaces from a zoom: alpha in the first, bravo in a new one.
  await browser(['open', `${url}agents/alpha`])
  await browser(['set', 'viewport', '1280', '800'])
  await evaluate('localStorage.clear(), sessionStorage.clear(), true')
  await browser(['reload'])
  await waitFor(`Boolean(${composer('alpha')})`)
  assert.equal(await evaluate(onLens), false, 'a panel URL is a zoom')
  await evaluate(`document.querySelector('.space-create').click(), true`)
  await waitFor(`document.querySelectorAll('.spaces-list .space-row').length === 2`)
  await evaluate(`(() => {
    const row = [...document.querySelectorAll('[role="treeitem"]')].find((node) => node.textContent.includes('bravo'))
    row.dispatchEvent(new MouseEvent('dblclick', { bubbles: true }))
    return true
  })()`)
  await waitFor(`Boolean(${composer('bravo')})`)

  // Home: the lens, with no fleet rail.
  await browser(['open', url])
  await waitFor(`${onLens} && document.querySelectorAll('.lens-card').length === 2`)
  assert.equal(await evaluate(leftRail), 'none', 'the fleet rail and Spaces are hidden on the lens')
  assert.match(await evaluate(`document.querySelector('.lens-header').textContent`) as string, /^herder · (live|offline) · 2 spaces · 1 need you · \? keys$/, 'lone, in no space, counts in the header total')
  const order = JSON.parse(await evaluate(`JSON.stringify(${cards})`) as string) as string[]
  assert.equal(order.length, 2)
  assert.match(await evaluate(`document.querySelector('.lens-row-watch .lens-head').textContent`) as string, /^2 WATCH two lines · 2$/, 'unplaced spaces sit in watch')
  assert.equal(await evaluate(selected), order[1], 'the lens opens on the space last zoomed into')
  assert.match(await evaluate(`document.querySelector('.lens-card[data-space-id="${order[1]}"] .lens-who').textContent`) as string, /bravo/)

  // h l move the selection; enter zooms into the second space on bravo; the rail returns.
  await evaluate('document.activeElement?.blur(), true')
  await press('h')
  assert.equal(await evaluate(selected), order[0])
  await press('h')
  assert.equal(await evaluate(selected), order[0], '← stops at the first card')
  await press('l')
  assert.equal(await evaluate(selected), order[1])
  await press('Enter')
  await waitFor(`!${onLens} && Boolean(${composer('bravo')})`)
  assert.equal(await evaluate(leftRail), 'flex', 'the fleet rail returns inside a zoom')
  assert.match(await evaluate(crumb) as string, /^lens ›.*›bravo/)
  assert.match(await evaluate('location.search') as string, new RegExp(`space=${order[1]}`))
  await waitFor(`document.activeElement === ${composer('bravo')}`)
  assert.equal(await evaluate(`document.activeElement === ${composer('bravo')}`), true, 'a zoom arrives in the agent composer (owner #463121)')

  // esc hands the keys to the panel; the next zooms out with the space still selected.
  await press('Escape')
  await press('Escape')
  await waitFor(onLens)
  assert.equal(await evaluate(selected), order[1])
  assert.equal(await evaluate('location.pathname + location.search'), '/')

  // Double-click zooms; [ ] cross spaces in lens order, wrapping. Each lands in a composer, so esc hands the keys back first.
  await evaluate(`document.querySelector('.lens-card[data-space-id="${order[0]}"]').dispatchEvent(new MouseEvent('dblclick', { bubbles: true })), true`)
  await waitFor(`!${onLens} && document.activeElement === ${composer('alpha')}`)
  await press('Escape')
  await press(']')
  await waitFor(`document.activeElement === ${composer('bravo')} && (${crumb}).includes('bravo')`)
  await press('Escape')
  await press(']')
  await waitFor(`document.activeElement === ${composer('alpha')} && (${crumb}).includes('alpha')`)
  await press('Escape')
  await press('[')
  await waitFor(`document.activeElement === ${composer('bravo')}`)
  await press('Escape')
  await press('[')
  await waitFor(`document.activeElement === ${composer('alpha')}`)

  // Typing in the composer: no lens or zoom key fires; esc returns to the panel, the next esc to the lens.
  await evaluate(`${composer('alpha')}.focus(), true`)
  await browser(['keyboard', 'type', 'n]l[N'])
  assert.equal(await evaluate(`${composer('alpha')}.value`), 'n]l[N')
  assert.match(await evaluate(crumb) as string, /alpha/)
  await press('Escape')
  assert.equal(await evaluate(onLens), false, 'esc in the composer returns to the panel first')
  assert.equal(await evaluate(`document.activeElement?.classList.contains('agent-page') ?? false`), true)
  await press('Escape')
  await waitFor(onLens)
  assert.equal(await evaluate(selected), order[0])

  // N reaches lone, in no space, and opens it alone; back returns to the lens.
  await evaluate('document.activeElement?.blur(), true')
  await press('Shift+N')
  await waitFor(`Boolean(document.querySelector('.alone-agent textarea[aria-label="Message lone"]'))`)
  assert.match(await evaluate(crumb) as string, /no space.*lone/)
  assert.match(await evaluate('location.search') as string, /alone=1/)
  assert.equal(await evaluate(onLens), false)
  await browser(['back'])
  await waitFor(onLens)

  // A reload keeps the lens.
  await browser(['reload'])
  await waitFor(`${onLens} && document.querySelectorAll('.lens-card').length === 2`)
})
