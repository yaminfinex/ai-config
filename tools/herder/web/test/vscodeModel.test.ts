import assert from 'node:assert/strict'
import test from 'node:test'

import {
  createVSCodeHostsStore,
  encodeRemotePath,
  folderName,
  otherHosts,
  parseVSCodeHosts,
  readVSCodeHosts,
  validateHostAlias,
  vscodeHostKey,
  vscodeHostsStorageBackupKey,
  vscodeHostsStorageKey,
  vscodeRemoteURL,
  withHostAlias,
  withoutHost,
  writeVSCodeHosts,
} from '../src/features/vscode/vscodeModel.ts'

class FakeStorage {
  readonly values = new Map<string, string>()
  failWrites = false
  getItem(key: string) { return this.values.get(key) ?? null }
  setItem(key: string, value: string) { if (this.failWrites) throw new Error('quota'); this.values.set(key, value) }
}

test('remote path encoding matches bin/vsc-opener urllib.parse.quote byte for byte', () => {
  // Expected values were produced by python3 urllib.parse.quote(path).
  const cases: [string, string][] = [
    ['/home/ubuntu/Coding/ai-config', '/home/ubuntu/Coding/ai-config'],
    ['/mnt/a b/c#d?e+f', '/mnt/a%20b/c%23d%3Fe%2Bf'],
    ["/x/(it)'s!*~_.-", '/x/%28it%29%27s%21%2A~_.-'],
    ['/x/café/日本', '/x/caf%C3%A9/%E6%97%A5%E6%9C%AC'],
    ['/x/a%20b;c=d&e@f:g,h$i', '/x/a%2520b%3Bc%3Dd%26e%40f%3Ag%2Ch%24i'],
  ]
  for (const [path, expected] of cases) assert.equal(encodeRemotePath(path), expected, path)
})

test('the VS Code URL is the Remote-SSH scheme vsc-opener builds, for absolute paths only', () => {
  assert.equal(vscodeRemoteURL('devbox', '/mnt/bench-nvme/herdr-worktrees/ai-config/vscode-link'),
    'vscode://vscode-remote/ssh-remote+devbox/mnt/bench-nvme/herdr-worktrees/ai-config/vscode-link')
  assert.equal(vscodeRemoteURL('devbox', '/a b'), 'vscode://vscode-remote/ssh-remote+devbox/a%20b')
  assert.equal(vscodeRemoteURL('devbox', 'relative/path'), null)
  assert.equal(vscodeRemoteURL('dev box', '/a'), null)
})

test('host aliases are trimmed and refuse characters that would break the URI', () => {
  assert.deepEqual(validateHostAlias('  devbox  '), { ok: true, value: 'devbox' })
  assert.deepEqual(validateHostAlias('user@dev-box.example_1:22'), { ok: true, value: 'user@dev-box.example_1:22' })
  for (const bad of ['', '   ', 'dev box', 'dev\tbox', 'dev/box', 'dev+box', 'dev?box', 'dev#box', 'dev%2fwrong', 'dev%00', 'dev\u0000box', 'dev\u007fbox', 'dev\u0085box']) {
    const result = validateHostAlias(bad)
    assert.equal(result.ok, false, bad)
    assert.ok(!result.ok && result.reason.length > 0)
  }
  assert.match((validateHostAlias('a+b') as { reason: string }).reason, /“\+”/)
  assert.match((validateHostAlias('dev%2fwrong') as { reason: string }).reason, /“%”/)
  assert.match((validateHostAlias('dev\u0000box') as { reason: string }).reason, /control characters/)
  // VS Code decodes the authority before splitting, so an alias like
  // dev%2fwrong would open host dev at /wrong; no URL is built for it.
  assert.equal(vscodeRemoteURL('dev%2fwrong', '/a'), null)
})

test('the mapping key is the lower-cased hostname without the port', () => {
  assert.equal(vscodeHostKey({ hostname: 'Bench.Tailnet.ts.net' }), 'bench.tailnet.ts.net')
  assert.equal(vscodeHostKey(new URL('http://bench:4487/agents') as unknown as Location), vscodeHostKey(new URL('http://bench:4400/') as unknown as Location))
})

test('folder names come from the last path segment', () => {
  assert.equal(folderName('/mnt/x/vscode-link'), 'vscode-link')
  assert.equal(folderName('/mnt/x/vscode-link/'), 'vscode-link')
  assert.equal(folderName('/'), '/')
})

test('mapping edits are pure and validated', () => {
  const one = withHostAlias({}, 'bench', ' devbox ')
  assert.deepEqual(one, { ok: true, value: { bench: 'devbox' } })
  assert.equal(withHostAlias({}, 'bench', 'a/b').ok, false)
  const hosts = { bench: 'devbox', laptop: 'mac', alpha: 'a' }
  assert.deepEqual(withoutHost(hosts, 'laptop'), { bench: 'devbox', alpha: 'a' })
  assert.deepEqual(otherHosts(hosts, 'bench'), [['alpha', 'a'], ['laptop', 'mac']])
})

test('stored mappings are versioned and malformed data is refused', () => {
  assert.deepEqual(parseVSCodeHosts('{"version":1,"hosts":{"bench":"devbox"}}'), { version: 1, hosts: { bench: 'devbox' } })
  for (const raw of [null, '', 'nope', '{"version":2,"hosts":{}}', '{"version":1,"hosts":[]}', '{"version":1,"hosts":{"bench":"a b"}}', '{"version":1,"hosts":{"bench":3}}', '{"version":1,"hosts":{"bench":"dev%2fwrong"}}', '{"version":1,"hosts":{"bench":"dev\\u0000"}}']) {
    assert.equal(parseVSCodeHosts(raw), null, String(raw))
  }
})

test('writes keep a last-good copy and reads fall back to it', () => {
  const storage = new FakeStorage()
  assert.equal(writeVSCodeHosts(storage, { bench: 'one' }), true)
  assert.equal(writeVSCodeHosts(storage, { bench: 'two' }), true)
  assert.deepEqual(JSON.parse(storage.values.get(vscodeHostsStorageKey)!).hosts, { bench: 'two' })
  assert.deepEqual(JSON.parse(storage.values.get(vscodeHostsStorageBackupKey)!).hosts, { bench: 'one' })
  storage.values.set(vscodeHostsStorageKey, '{corrupt')
  assert.deepEqual(readVSCodeHosts(storage), { bench: 'one' })
  assert.deepEqual(readVSCodeHosts(null), {})
  storage.failWrites = true
  assert.equal(writeVSCodeHosts(storage, { bench: 'three' }), false)
})

test('the store sets, clears, notifies, persists, and reloads on another tab write', () => {
  const storage = new FakeStorage()
  const listeners = new Set<(event: Event) => void>()
  const events = {
    addEventListener: (_type: string, listener: (event: Event) => void) => listeners.add(listener),
    removeEventListener: (_type: string, listener: (event: Event) => void) => listeners.delete(listener),
  } as unknown as Pick<Window, 'addEventListener' | 'removeEventListener'>
  const store = createVSCodeHostsStore(() => storage, events)
  let notified = 0
  const dispose = store.subscribe(() => { notified += 1 })
  assert.equal(listeners.size, 1)
  assert.deepEqual(store.get(), {})
  assert.deepEqual(store.set('bench', ' dev '), { ok: true, value: 'dev', persisted: true })
  assert.equal(store.set('bench', 'bad alias').ok, false)
  assert.deepEqual(store.get(), { bench: 'dev' })
  assert.equal(notified, 1)
  assert.deepEqual(createVSCodeHostsStore(() => storage).get(), { bench: 'dev' })

  storage.values.set(vscodeHostsStorageKey, JSON.stringify({ version: 1, hosts: { bench: 'dev', other: 'x' } }))
  for (const listener of listeners) listener({ key: 'unrelated' } as StorageEvent)
  assert.equal(notified, 1)
  for (const listener of listeners) listener({ key: vscodeHostsStorageKey } as StorageEvent)
  assert.equal(notified, 2)
  assert.deepEqual(store.get(), { bench: 'dev', other: 'x' })

  assert.equal(store.remove('bench'), true)
  assert.equal(store.remove('missing'), true)
  assert.deepEqual(store.get(), { other: 'x' })
  assert.equal(notified, 3)
  dispose()
  assert.equal(listeners.size, 0)
})

test('a store without storage still works for the session and says so', () => {
  const store = createVSCodeHostsStore(() => { throw new Error('blocked') })
  assert.deepEqual(store.set('bench', 'dev'), { ok: true, value: 'dev', persisted: false })
  assert.deepEqual(store.get(), { bench: 'dev' })
  assert.equal(store.remove('bench'), false)
  assert.deepEqual(store.get(), {})
})

test('failed writes and removals keep the session value and report it was not stored', () => {
  const storage = new FakeStorage()
  const store = createVSCodeHostsStore(() => storage)
  assert.deepEqual(store.set('bench', 'one'), { ok: true, value: 'one', persisted: true })
  storage.failWrites = true
  assert.deepEqual(store.set('bench', 'two'), { ok: true, value: 'two', persisted: false })
  assert.deepEqual(store.get(), { bench: 'two' })
  assert.deepEqual(readVSCodeHosts(storage), { bench: 'one' })
  assert.equal(store.remove('bench'), false)
  assert.deepEqual(store.get(), {})
  assert.deepEqual(readVSCodeHosts(storage), { bench: 'one' })
  storage.failWrites = false
  assert.equal(store.remove('bench'), true)
  assert.deepEqual(store.set('bench', 'three'), { ok: true, value: 'three', persisted: true })
  assert.deepEqual(createVSCodeHostsStore(() => storage).get(), { bench: 'three' })
})
