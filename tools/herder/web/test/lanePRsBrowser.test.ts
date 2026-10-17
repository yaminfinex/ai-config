import assert from 'node:assert/strict'
import test from 'node:test'

import { fixtureAPI, lanesAPI, startBrowser } from './browserFixture.ts'

const row = (label: string) => `[...document.querySelectorAll('[role="treeitem"]')].find((node) => node.querySelector('.tree-label')?.textContent.trim().startsWith(${JSON.stringify(label)}))`
const card = (id: string) => `document.querySelector('.lens-card[data-space-id="${id}"]')`
const onLens = `Boolean(document.querySelector('.lens'))`
const focused = `document.activeElement?.getAttribute('aria-label') ?? ''`

function pr(number: number, extra: Record<string, unknown> = {}) {
  return { repo: 'acme/app', number, title: `PR ${number}`, url: `https://github.com/acme/app/pull/${number}`, state: 'OPEN', draft: false, unresolved: 0, head: `b-${number}`, ...extra }
}

test('lane PRs: rows under the lane header, a line on the lane card, and #n in ⌘K zooms into the lane at its lead', { timeout: 180_000 }, async (context) => {
  const api = fixtureAPI(['alpha', 'bravo', 'charlie'])
  const lanes = lanesAPI(api, [
    { id: 'pay1', name: 'payload', lead: 'bravo', members: ['alpha', 'bravo'], prs: [pr(309, { draft: true, checks: 'FAILURE', linked: true }), pr(7, { state: 'MERGED', linked: true })] },
    { id: 'doc1', name: 'docs', lead: 'charlie', members: ['charlie'], prs: [], pr_note: 'no GitHub token: set GITHUB_TOKEN or run gh auth login, then restart the serve' },
  ])
  const { url, browser, evaluate, waitFor } = await startBrowser(context, 'lane-prs', [lanes.plugin, api.plugin])
  const press = async (key: string) => { await browser(['press', key]); await browser(['wait', '80']) }

  await browser(['open', `${url}agents/alpha`])
  await browser(['set', 'viewport', '1280', '800'])
  await evaluate('localStorage.clear(), sessionStorage.clear(), true')
  await browser(['reload'])
  await waitFor(`document.activeElement?.getAttribute('aria-label') === 'Message alpha'`)
  await evaluate(`document.querySelector('.fleet-view-option[title^="Lanes"]').click(), true`)
  await waitFor(`Boolean(${row('payload')})`)
  await evaluate(`${row('payload')}.getAttribute('aria-expanded') === 'true' || ${row('payload')}.click(), true`)
  await waitFor(`Boolean(${row('#309')})`)
  assert.match(await evaluate(`${row('#309')}.textContent`) as string, /#309 PR 309 · checks failing, draft/)
  assert.match(await evaluate(`${row('#7')}.textContent`) as string, /merged/)

  // A poll moves a row: the serve's next frame carries the change.
  lanes.record[0]!.prs = [pr(309, { checks: 'SUCCESS', linked: true })]
  lanes.publish()
  await waitFor(`!${row('#7')} && /checks pass/.test(${row('#309')}.textContent)`)

  // The lens: the lane card carries its PR line, the other its note.
  await press('Escape')
  await press('Escape')
  await waitFor(`${onLens} && Boolean(${card('lane:pay1')})`)
  assert.equal(await evaluate(`${card('lane:pay1')}.querySelector('.lens-prs')?.textContent`), '#309 checks pass')
  assert.match(await evaluate(`${card('lane:doc1')}.querySelector('.lens-prs')?.textContent`) as string, /no GitHub token/)

  // ⌘K #309 from the lens: zooms into payload with its lead bravo focused.
  await press('Control+k')
  await waitFor(`document.activeElement?.closest('.quick-open') !== null`)
  await browser(['keyboard', 'type', '#309'])
  await waitFor(`/#309 PR 309 · acme\\/app · lane payload/.test(document.querySelector('.quick-open [aria-selected="true"]')?.textContent ?? '')`)
  await press('Enter')
  await waitFor(`!${onLens} && document.activeElement?.getAttribute('aria-label') === 'Message bravo'`).catch(() => undefined)
  assert.equal(await evaluate(focused), 'Message bravo', 'the lead holds the focus')
  assert.match(await evaluate(`document.querySelector('.lens-crumb-path')?.textContent ?? ''`) as string, /payload/)
})
