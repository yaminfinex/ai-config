import assert from 'node:assert/strict'
import test, { type TestContext } from 'node:test'

import { fixtureAPI, startBrowser } from './browserFixture.ts'

type Entry = { uuid: string, byteOffset: number }

// transcripts holds each agent's transcript; append adds one reply the
// page can be searched for.
function transcripts(agents: string[]) {
  const all = new Map<string, Entry[]>()
  const entry = (agent: string, index: number, text: string) => ({
    uuid: `${agent}-${index}`, line: index + 1, byteOffset: index * 100, timestamp: new Date(Date.UTC(2026, 0, 1, 10, 0, index)).toISOString(),
    kind: 'assistant_text', payload: { message: { content: [{ type: 'text', text }] } },
  })
  for (const agent of agents) all.set(agent, Array.from({ length: 20 }, (_, index) => entry(agent, index, `fixture reply ${index} for ${agent}`)))
  return {
    entries: (agent: string) => all.get(agent) ?? [],
    append: (agent: string, text: string) => {
      const list = all.get(agent) ?? []
      list.push(entry(agent, list.length, text))
    },
  }
}

async function until(condition: () => boolean, timeout: number) {
  const deadline = Date.now() + timeout
  while (!condition() && Date.now() < deadline) await new Promise((resolve) => setTimeout(resolve, 25))
  return condition()
}

// Two spaces, alpha open in the first and bravo in the second, with the first shown.
async function twoSpaces(context: TestContext, name: string, api: ReturnType<typeof fixtureAPI>) {
  const { url, browser, evaluate, waitFor } = await startBrowser(context, name, api.plugin)
  const composer = (agent: string) => `Boolean(document.querySelector('textarea[data-composer][aria-label="Message ${agent}"]'))`
  const ready = `[...document.querySelectorAll('.transcript')].some((node) => node.querySelectorAll('article').length > 0)`
  await browser(['open', `${url}agents/alpha`])
  await browser(['set', 'viewport', '1400', '900'])
  await evaluate('localStorage.clear(), sessionStorage.clear(), true')
  await browser(['reload'])
  await waitFor(`${composer('alpha')} && ${ready}`)
  await evaluate(`document.querySelector('.space-create').click(), true`)
  await waitFor(`document.querySelectorAll('.spaces-list .space-row').length === 2`)
  await evaluate(`(() => {
    const row = [...document.querySelectorAll('[role="treeitem"]')].find((node) => node.textContent.includes('bravo'))
    row.dispatchEvent(new MouseEvent('dblclick', { bubbles: true }))
    return true
  })()`)
  await waitFor(`${composer('bravo')} && ${ready}`)
  await evaluate(`document.querySelectorAll('.spaces-list .space-name')[0].click(), true`)
  await waitFor(`${composer('alpha')} && ${ready}`)
  // The switch settles: any subscription change or catch-up has landed.
  await browser(['wait', '800'])
  // switchTo clicks a space and returns how long the page took to show text, or null after timeout ms.
  const switchTo = async (index: number, text: string, timeout = 6_000) => {
    await evaluate(`(() => {
      window.__shownAfter = null
      const started = performance.now()
      const check = () => {
        if (window.__shownAfter !== null || !document.body.textContent.includes(${JSON.stringify(text)})) return false
        window.__shownAfter = performance.now() - started
        return true
      }
      const observer = new MutationObserver(() => { if (check()) observer.disconnect() })
      observer.observe(document.body, { childList: true, subtree: true, characterData: true })
      document.querySelectorAll('.spaces-list .space-name')[${index}].click()
      return true
    })()`)
    const deadline = Date.now() + timeout
    for (;;) {
      const shown = await evaluate('window.__shownAfter') as number | null
      if (shown !== null || Date.now() > deadline) return shown
      await browser(['wait', '50'])
    }
  }
  return { browser, evaluate, switchTo }
}

test('a space switch shows entries that arrived while it was hidden, with no new stream or fetch', { timeout: 180_000 }, async (context) => {
  const fixture = transcripts(['alpha', 'bravo'])
  const api = fixtureAPI(['alpha', 'bravo'], { entries: fixture.entries, live: {} })
  const { switchTo } = await twoSpaces(context, 'stream-switch-hidden', api)

  const opens = api.opens()
  const requests = api.entryRequests.length
  fixture.append('bravo', 'fresh reply while bravo was hidden')
  api.send('entry:bravo', {})
  const fetched = await until(() => api.entryRequests.slice(requests).some((request) => request.agent === 'bravo'), 3_000)

  // Entries requests now go unanswered: the switch must render from the cache.
  api.holdEntries(true)
  const afterEntry = api.entryRequests.length
  const shown = await switchTo(1, 'fresh reply while bravo was hidden', 3_000)
  const fetchesAfterSwitch = api.heldEntries() + api.entryRequests.length - afterEntry
  const opensAfterSwitch = api.opens() - opens
  api.holdEntries(false)

  assert.notEqual(shown, null, 'switching to bravo shows the entry that arrived while it was hidden, without a network answer')
  assert.equal(opensAfterSwitch, 0, 'the switch opens no new event stream')
  assert.equal(fetchesAfterSwitch, 0, 'the switch fetches no transcript')
  assert.ok(fetched, 'the hidden transcript fetched the new entry in the background')
  const delta = api.entryRequests.slice(requests).filter((request) => request.agent === 'bravo')
  assert.ok(delta.length > 0 && delta.every((request) => request.from !== null && request.from > 0), `the background fetch is a delta: ${JSON.stringify(delta)}`)
})

// serveFirstBoardMs is how long a serve on this branch's base took to send a
// new 40-agent stream its first board (0.5 s on a quiet scratch serve, 0.8-2.7 s
// on a busy live one): the wait main put between a space switch and a hidden
// transcript's refresh.
const serveFirstBoardMs = 1_000

test('ten space switches with new entries open no stream and fetch only deltas', { timeout: 300_000 }, async (context) => {
  const fixture = transcripts(['alpha', 'bravo'])
  const api = fixtureAPI(['alpha', 'bravo'], { entries: fixture.entries, live: { firstBoardDelayMs: serveFirstBoardMs } })
  const { browser, switchTo } = await twoSpaces(context, 'stream-switch-ten', api)

  const opens = api.opens()
  const requests = api.entryRequests.length
  const latencies: (number | null)[] = []
  let fetchesAfterSwitch = 0
  for (let round = 1; round <= 10; round++) {
    // Space 1 (alpha) is shown on odd rounds before the switch, so bravo is hidden.
    const hidden = round % 2 ? 'bravo' : 'alpha'
    const before = api.entryRequests.length
    fixture.append(hidden, `fresh reply ${round} for ${hidden}`)
    api.send(`entry:${hidden}`, {})
    await until(() => api.entryRequests.slice(before).some((request) => request.agent === hidden), 1_000)
    await browser(['wait', '200'])
    const atSwitch = api.entryRequests.length
    const shown = await switchTo(round % 2, `fresh reply ${round} for ${hidden}`)
    fetchesAfterSwitch += api.entryRequests.slice(atSwitch).filter((request) => request.agent === hidden).length
    latencies.push(shown === null ? null : Math.round(shown))
    await browser(['wait', '300'])
  }
  const fetched = api.entryRequests.slice(requests)
  const measured = {
    serveFirstBoardMs,
    switchToNewEntryMs: latencies,
    eventStreamOpens: api.opens() - opens,
    entriesRequests: fetched.length,
    entriesBytes: fetched.reduce((total, request) => total + request.bytes, 0),
    fullWindowRequests: fetched.filter((request) => request.from === null).length,
    fetchesAfterSwitch,
  }
  context.diagnostic(JSON.stringify(measured))
  assert.ok(latencies.every((latency) => latency !== null), `every switch shows its new entry: ${JSON.stringify(measured)}`)
  assert.equal(measured.eventStreamOpens, 0, `ten switches open no event stream: ${JSON.stringify(measured)}`)
  assert.equal(measured.fullWindowRequests, 0, `ten switches fetch no full transcript window: ${JSON.stringify(measured)}`)
  assert.equal(fetchesAfterSwitch, 0, `no switch waits on a transcript fetch: ${JSON.stringify(measured)}`)
})
