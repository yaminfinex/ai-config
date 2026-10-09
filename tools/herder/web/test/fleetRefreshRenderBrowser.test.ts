import assert from 'node:assert/strict'
import test from 'node:test'

import type { Plugin } from 'vite'

import { fixtureAPI, renderCounter, startBrowser } from './browserFixture.ts'

type Counted = { commits: number, renders: Record<string, number> }

// rendered counts a component's renders; the dev build may suffix a memoised
// component's inner function name with a digit.
function rendered({ renders }: Counted, name: string) {
  return Object.entries(renders).reduce((sum, [label, count]) => label.replace(/\d+$/u, '') === name ? sum + count : sum, 0)
}

// The dock panels and tabs read the fleet board through narrow selections,
// and the sidebar is memoised, so neither renders for a change it does not show.
test('dock panels, tabs and the sidebar render only for changes they show', { timeout: 180_000 }, async (context) => {
  const api = fixtureAPI(['alpha', 'bravo', 'charlie'])
  // The hook goes ahead of the React refresh preamble, which wraps it.
  const plugin: Plugin = { ...api.plugin, transformIndexHtml: () => [{ tag: 'script', children: renderCounter, injectTo: 'head-prepend' }] }
  const { url, browser, evaluate, waitFor } = await startBrowser(context, 'fleet-refresh-render', plugin)
  await browser(['open', `${url}agents/alpha`])
  await browser(['set', 'viewport', '1400', '900'])
  await evaluate('localStorage.clear(), true')
  await browser(['reload'])
  await waitFor(`Boolean(document.querySelector('textarea[data-composer][aria-label="Message alpha"]'))`)
  // Open bravo in a side group: two dock panels and two tabs; charlie has none.
  await evaluate(`(() => {
    const row = [...document.querySelectorAll('[role="treeitem"]')].find((node) => node.textContent.includes('bravo'))
    row.dispatchEvent(new MouseEvent('dblclick', { bubbles: true, altKey: true }))
    return true
  })()`)
  await waitFor(`document.querySelectorAll('.dv-groupview').length === 2 && Boolean(document.querySelector('textarea[data-composer][aria-label="Message bravo"]'))`)
  await browser(['wait', '1000'])

  const counted = async (action: () => Promise<void>) => {
    await evaluate('window.__renders = {}, window.__commits = 0, window.__track = true, true')
    await action()
    await browser(['wait', '1000'])
    return await evaluate('window.__track = false, { commits: window.__commits, renders: window.__renders }') as Counted
  }
  const tab = (name: string) => `[...document.querySelectorAll('.herder-dock-tab')].find((node) => node.textContent.includes('${name}'))`
  const sidebarDot = (name: string, status: string) => `Boolean([...document.querySelectorAll('[role="treeitem"]')].find((node) => node.textContent.includes('${name}'))?.querySelector('.status-dot.${status}'))`

  // A status change on an agent with no open panel.
  const unopened = await counted(async () => {
    api.board.workspaces[0].tabs[0].panes[2].bus_status = 'active'
    api.send('fleet', api.board)
    await waitFor(sidebarDot('charlie', 'active'))
  })
  assert.ok(rendered(unopened, 'FleetSidebar') > 0, `the sidebar shows the change: ${JSON.stringify(unopened.renders)}`)
  for (const name of ['AgentDockPanel', 'AgentPanel', 'DockTab']) {
    assert.equal(rendered(unopened, name), 0, `${name} rendered for charlie: ${JSON.stringify(unopened.renders)}`)
  }

  // A status change on bravo renders bravo's panel and tab, not alpha's.
  const bravo = await counted(async () => {
    api.board.workspaces[0].tabs[0].panes[1].bus_status = 'active'
    api.send('fleet', api.board)
    await waitFor(`Boolean(${tab('bravo')}?.querySelector('.status-dot.active'))`)
  })
  for (const name of ['AgentDockPanel', 'AgentPanel', 'DockTab']) {
    assert.equal(rendered(bravo, name), 1, `${name} renders once, for bravo: ${JSON.stringify(bravo.renders)}`)
  }

  // A Shell render that changes no panel, tab or tree data: opening the shortcut reference.
  const shell = await counted(async () => {
    await evaluate(`document.querySelector('[aria-label="Open keyboard shortcuts"]').click(), true`)
    await waitFor(`Boolean(document.querySelector('.shortcut-backdrop'))`)
  })
  assert.ok(rendered(shell, 'Shell') > 0, `the shell renders: ${JSON.stringify(shell.renders)}`)
  for (const name of ['FleetSidebar', 'AgentDockPanel', 'DockTab']) {
    assert.equal(rendered(shell, name), 0, `${name} rendered for the shortcut reference: ${JSON.stringify(shell.renders)}`)
  }
})
