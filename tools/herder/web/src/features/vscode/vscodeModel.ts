// VS Code Remote-SSH links are built in the browser: the browser runs on the
// owner's laptop, so a vscode:// anchor opens the laptop's VS Code against
// the SSH host alias the laptop knows this machine by. The alias is a
// browser-local mapping keyed by the herder web hostname.

export const vscodeHostsStorageKey = 'herder.web.vscode-hosts.v1'
export const vscodeHostsStorageBackupKey = 'herder.web.vscode-hosts.v1.last-good'

export type VSCodeHosts = Readonly<Record<string, string>>
export type StoredVSCodeHosts = { version: 1, hosts: Record<string, string> }
export type AliasResult = { ok: true, value: string } | { ok: false, reason: string }
// persisted is false when the browser refused the write: the change holds for
// this session only and the stored mapping returns on reload.
export type SaveResult = { ok: true, value: string, persisted: boolean } | { ok: false, reason: string }

// The mapping key is the hostname without the port: the alias names the
// machine, not the serve. localStorage is still per origin, so a serve on
// another port keeps its own copy of the mapping and asks once itself.
export function vscodeHostKey(location: Pick<Location, 'hostname'>) {
  return location.hostname.toLowerCase()
}

// encodeRemotePath matches Python's urllib.parse.quote(path), the encoding the
// retired VS Code opener scripts used: '/' and unreserved characters stay,
// everything else is percent-encoded as UTF-8. encodeURIComponent also leaves
// !'()* bare, so those are escaped here to stay byte-identical to quote().
export function encodeRemotePath(path: string) {
  return path.split('/').map((segment) => encodeURIComponent(segment)
    .replace(/[!'()*]/g, (char) => `%${char.charCodeAt(0).toString(16).toUpperCase()}`)).join('/')
}

export function vscodeRemoteURL(alias: string, cwd: string) {
  if (!cwd.startsWith('/') || !validateHostAlias(alias).ok) return null
  return `vscode://vscode-remote/ssh-remote+${alias}${encodeRemotePath(cwd)}`
}

export function folderName(cwd: string) {
  const trimmed = cwd.replace(/\/+$/, '')
  return trimmed.slice(trimmed.lastIndexOf('/') + 1) || '/'
}

// C0 and C1 control characters, DEL included.
function isControl(char: string) {
  const code = char.codePointAt(0)!
  return code < 0x20 || (code >= 0x7f && code <= 0x9f)
}

export function validateHostAlias(raw: string): AliasResult {
  const value = raw.trim()
  if (!value) return { ok: false, reason: 'Enter the SSH host alias.' }
  if ([...value].some(isControl)) return { ok: false, reason: 'A host alias cannot contain control characters.' }
  if (/\s/.test(value)) return { ok: false, reason: 'A host alias cannot contain spaces.' }
  // VS Code percent-decodes the authority before splitting it, so '%' could
  // smuggle a '/' into the host; ssh config aliases never need it.
  const bad = value.match(/[/+?#%]/)
  if (bad) return { ok: false, reason: `A host alias cannot contain “${bad[0]}”; it would break the VS Code link.` }
  return { ok: true, value }
}

function isHosts(value: unknown): value is Record<string, string> {
  return typeof value === 'object' && value !== null && !Array.isArray(value) &&
    Object.entries(value).every(([key, alias]) => key !== '' && typeof alias === 'string' && validateHostAlias(alias).ok && alias === alias.trim())
}

export function parseVSCodeHosts(raw: string | null): StoredVSCodeHosts | null {
  try {
    const value: unknown = JSON.parse(raw ?? '')
    if (typeof value !== 'object' || value === null) return null
    const stored = value as Partial<StoredVSCodeHosts>
    if (stored.version !== 1 || !isHosts(stored.hosts)) return null
    return { version: 1, hosts: { ...stored.hosts } }
  } catch {
    return null
  }
}

type HostsStorage = Pick<Storage, 'getItem' | 'setItem'>

export function readVSCodeHosts(storage: Pick<Storage, 'getItem'> | null): VSCodeHosts {
  try {
    const stored = parseVSCodeHosts(storage?.getItem(vscodeHostsStorageKey) ?? null) ??
      parseVSCodeHosts(storage?.getItem(vscodeHostsStorageBackupKey) ?? null)
    return stored?.hosts ?? {}
  } catch {
    return {}
  }
}

export function writeVSCodeHosts(storage: HostsStorage | null, hosts: VSCodeHosts) {
  if (!storage) return false
  try {
    const previous = storage.getItem(vscodeHostsStorageKey)
    if (parseVSCodeHosts(previous)) storage.setItem(vscodeHostsStorageBackupKey, previous!)
    const raw = JSON.stringify({ version: 1, hosts } satisfies StoredVSCodeHosts)
    storage.setItem(vscodeHostsStorageKey, raw)
  } catch {
    return false
  }
  try { if (!parseVSCodeHosts(storage.getItem(vscodeHostsStorageBackupKey))) storage.setItem(vscodeHostsStorageBackupKey, JSON.stringify({ version: 1, hosts } satisfies StoredVSCodeHosts)) } catch { /* the mapping itself is stored; only the backup is missing */ }
  return true
}

export function withHostAlias(hosts: VSCodeHosts, hostKey: string, raw: string): { ok: true, value: VSCodeHosts } | { ok: false, reason: string } {
  const alias = validateHostAlias(raw)
  if (!alias.ok) return alias
  return { ok: true, value: { ...hosts, [hostKey]: alias.value } }
}

export function withoutHost(hosts: VSCodeHosts, hostKey: string): VSCodeHosts {
  const { [hostKey]: _removed, ...rest } = hosts
  void _removed
  return rest
}

// otherHosts lists stored mappings except the current one, sorted by name.
export function otherHosts(hosts: VSCodeHosts, hostKey: string) {
  return Object.entries(hosts).filter(([key]) => key !== hostKey).sort(([left], [right]) => left.localeCompare(right))
}

type StoreEvents = Pick<Window, 'addEventListener' | 'removeEventListener'>

export type VSCodeHostsStore = {
  get: () => VSCodeHosts
  set: (hostKey: string, raw: string) => SaveResult
  // remove reports whether the removal reached storage.
  remove: (hostKey: string) => boolean
  subscribe: (listener: () => void) => () => void
}

export function createVSCodeHostsStore(storage: () => HostsStorage | null, events: StoreEvents | null = null): VSCodeHostsStore {
  let current: VSCodeHosts | undefined
  const listeners = new Set<() => void>()
  const safe = () => { try { return storage() } catch { return null } }
  const get = () => (current ??= readVSCodeHosts(safe()))
  const commit = (hosts: VSCodeHosts) => {
    current = hosts
    const persisted = writeVSCodeHosts(safe(), hosts)
    listeners.forEach((listener) => listener())
    return persisted
  }
  const onStorage = (event: Event) => {
    const key = (event as StorageEvent).key
    if (key !== null && key !== vscodeHostsStorageKey) return
    current = undefined
    listeners.forEach((listener) => listener())
  }
  return {
    get,
    set: (hostKey, raw) => {
      const next = withHostAlias(get(), hostKey, raw)
      if (!next.ok) return next
      const persisted = commit(next.value)
      return { ok: true, value: next.value[hostKey], persisted }
    },
    remove: (hostKey) => {
      if (!(hostKey in get())) return true
      return commit(withoutHost(get(), hostKey))
    },
    subscribe: (listener) => {
      if (listeners.size === 0) events?.addEventListener('storage', onStorage)
      listeners.add(listener)
      return () => {
        listeners.delete(listener)
        if (listeners.size === 0) events?.removeEventListener('storage', onStorage)
      }
    },
  }
}
