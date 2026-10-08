import assert from 'node:assert/strict'
import test from 'node:test'

import { fixtureAPI, startBrowser } from './browserFixture.ts'

// A browser shaped like the owner's: 360 tombstones and 40 live notes, more
// than the server's 64 KiB write cap in one go. Every value here is synthetic.
const seed = `(() => {
  localStorage.clear()
  const prefix = 'herder.web.notes.v1:record:'
  for (let index = 0; index < 360; index++) {
    const id = '00000000-0000-4000-8000-' + String(index).padStart(12, '0')
    localStorage.setItem(prefix + id, JSON.stringify({ version: 1, writeID: 'write-' + 'w'.repeat(200) + '-' + index, record: { id, deleted: true, updated: Date.now() - 60_000 - index } }))
  }
  for (let index = 0; index < 40; index++) {
    const id = '11111111-0000-4000-8000-' + String(index).padStart(12, '0')
    const group = index % 2 ? 'alpha' : 'bravo'
    localStorage.setItem(prefix + id, JSON.stringify({ version: 1, writeID: 'write-live-' + index, record: { id, group, text: 'fixture note ' + index + ' ' + 'x'.repeat(1200), created: 1000 + index, updated: 1000 + index } }))
  }
  return true
})()`

// Counts storage and serialisation work from here on, by key pattern for writes.
const instrument = `(() => {
  const counts = window.__cost = { setItem: 0, setBytes: 0, getItem: 0, key: 0, stringify: 0, stringifyBytes: 0, keys: {} }
  if (!window.__instrumented) {
    window.__instrumented = true
    const proto = Storage.prototype
    const setItem = proto.setItem, getItem = proto.getItem, key = proto.key, stringify = JSON.stringify
    proto.setItem = function (name, value) { const c = window.__cost; c.setItem++; c.setBytes += String(value).length; c.keys[name.replace(/[0-9a-f-]{36}/, '<id>')] = (c.keys[name.replace(/[0-9a-f-]{36}/, '<id>')] ?? 0) + 1; return setItem.call(this, name, value) }
    proto.getItem = function (name) { window.__cost.getItem++; return getItem.call(this, name) }
    proto.key = function (index) { window.__cost.key++; return key.call(this, index) }
    JSON.stringify = function (...args) { const out = stringify.apply(this, args); const c = window.__cost; c.stringify++; c.stringifyBytes += out?.length ?? 0; return out }
  }
  return true
})()`

// typeInto sets the field's value one character at a time through React's
// input path and times each synchronous dispatch (render, commit, effects).
const typeInto = (selector: string, text: string) => `(() => {
  const field = ${selector}
  field.focus()
  const setter = Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value').set
  const times = []
  for (const char of ${JSON.stringify(text)}) {
    const start = performance.now()
    setter.call(field, field.value + char)
    field.dispatchEvent(new Event('input', { bubbles: true }))
    times.push(performance.now() - start)
  }
  return times
})()`

type Cost = { setItem: number, setBytes: number, getItem: number, key: number, stringify: number, stringifyBytes: number, keys: Record<string, number> }

test('a keystroke in the composer or a note edit does no storage scan, notes serialisation or queue write, and a commit sends one row', { timeout: 180_000 }, async (context) => {
  const api = fixtureAPI(['alpha', 'bravo'], { maxWriteBytes: 64 * 1024 })
  const { url, browser, evaluate, waitFor } = await startBrowser(context, 'typing-cost', api.plugin)
  const composer = `document.querySelector('textarea[data-composer][aria-label="Message alpha"]')`
  await browser(['open', `${url}agents/alpha`])
  await browser(['set', 'viewport', '1280', '800'])
  await waitFor(`Boolean(${composer})`).catch(async () => { await browser(['reload']); await waitFor(`Boolean(${composer})`) })
  await evaluate(seed)
  await browser(['reload'])
  await waitFor(`Boolean(${composer}) && document.querySelectorAll('.note-card').length > 0`)
  // Let start-up sync settle (the old code sent it as one refused POST).
  await browser(['wait', '1500'])

  const keys = 'the quick brown fox jumps'
  await evaluate(instrument)
  const composerTimes = await evaluate(typeInto(composer, keys)) as number[]
  await browser(['wait', '500'])
  const composerCost = await evaluate('window.__cost') as Cost
  assert.equal(await evaluate(`${composer}.value`), keys)

  await evaluate(`${composer}.blur(), true`)
  await evaluate(`document.querySelector('.note-card').dispatchEvent(new MouseEvent('dblclick', { bubbles: true })), true`)
  const editor = `document.querySelector('textarea[aria-label="Edit note comment"]')`
  await waitFor(`Boolean(${editor})`)
  await evaluate(instrument)
  const editTimes = await evaluate(typeInto(editor, keys)) as number[]
  await browser(['wait', '500'])
  const editCost = await evaluate('window.__cost') as Cost

  // Committing the edit is one mutation: what does it write and send?
  await evaluate(instrument)
  const postsBefore = api.posts.length
  await evaluate(`${editor}.blur(), true`)
  await browser(['wait', '1500'])
  const commitCost = await evaluate('window.__cost') as Cost
  const commitPosts = api.posts.slice(postsBefore)

  const summary = (times: number[]) => {
    const sorted = [...times].sort((a, b) => a - b)
    return { keystrokes: times.length, medianMs: Number(sorted[Math.floor(sorted.length / 2)].toFixed(2)), maxMs: Number(sorted.at(-1)!.toFixed(2)) }
  }
  const sent = (posts: typeof api.posts) => posts.map((post) => ({ ns: post.namespace, bytes: post.bytes, rows: post.keys.length, status: post.status }))
  context.diagnostic(JSON.stringify({
    composer: { ...summary(composerTimes), ...composerCost },
    noteEdit: { ...summary(editTimes), ...editCost },
    commit: { ...commitCost, posts: sent(commitPosts) },
    start: sent(api.posts.slice(0, postsBefore)),
  }))

  // A composer keystroke writes its own draft and nothing else, and never
  // walks storage or serialises notes (one fixture note is over 1 KiB).
  assert.deepEqual(composerCost.keys, { 'herder.web.messageDraft.v1:alpha': keys.length })
  assert.equal(composerCost.key, 0, 'a composer keystroke must not scan localStorage keys')
  assert.ok(composerCost.stringifyBytes < 50 * keys.length, `composer keystrokes serialised ${composerCost.stringifyBytes} bytes`)
  // A note-edit keystroke is React state only until the edit commits.
  assert.equal(editCost.setItem, 0, 'a note-edit keystroke must not write storage')
  assert.equal(editCost.key, 0, 'a note-edit keystroke must not scan localStorage keys')
  assert.ok(editCost.stringifyBytes < 50 * keys.length, `note-edit keystrokes serialised ${editCost.stringifyBytes} bytes`)
  // Start-up catches up in chunks the server accepts.
  const startNotes = api.posts.slice(0, postsBefore).filter((post) => post.namespace === 'notes')
  assert.ok(startNotes.length > 1, 'the catch-up is over the write cap, so it goes in chunks')
  for (const post of startNotes) assert.ok(post.status === 200 && post.bytes <= 48 * 1_024, `a start-up chunk: ${JSON.stringify(sent([post]))}`)
  // Committing the edit sends that one note and writes no large queue.
  assert.deepEqual(sent(commitPosts.filter((post) => post.namespace === 'notes')).map(({ rows, status }) => ({ rows, status })), [{ rows: 1, status: 200 }])
  assert.ok(commitCost.setBytes < 8 * 1_024, `the commit wrote ${commitCost.setBytes} bytes to storage`)
})
