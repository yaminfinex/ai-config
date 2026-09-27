import { useRef, useState } from 'react'
import { HostAliasForm } from './HostAliasForm.tsx'
import { currentHostKey, useVSCodeHosts, vscodeHosts } from './vscodeHosts.ts'
import { otherHosts } from './vscodeModel.ts'

const sessionOnly = 'for this session only: this browser would not store the change, so it is undone on reload'

export function VSCodeSettings() {
  const hosts = useVSCodeHosts()
  const hostKey = currentHostKey()
  const alias = hosts[hostKey] ?? ''
  const [saved, setSaved] = useState('')
  const input = useRef<HTMLInputElement>(null)
  const others = otherHosts(hosts, hostKey)
  // Clear and Remove unmount the button they were pressed on, so focus moves
  // to the alias field and stays inside the dialog.
  const remove = (key: string, done: string) => {
    const persisted = vscodeHosts.remove(key)
    setSaved(persisted ? `${done}.` : `${done} ${sessionOnly}.`)
    input.current?.focus()
  }
  return <section className="settings-section" aria-labelledby="settings-vscode-title">
    <h3 id="settings-vscode-title">VS Code Remote-SSH</h3>
    <HostAliasForm hostKey={hostKey} initial={alias} submitLabel="Save" inputRef={input}
      onSave={(value) => {
        const result = vscodeHosts.set(hostKey, value)
        if (result.ok) setSaved(result.persisted ? `Saved ${result.value} for ${hostKey}.` : `Saved ${result.value} for ${hostKey} ${sessionOnly}.`)
      }} />
    <div className="settings-row-actions">
      <span role="status" aria-live="polite">{saved || (alias ? `Links open over SSH host ${alias}.` : 'Not set. The VS Code button asks on first use.')}</span>
      {alias && <button type="button" onClick={() => remove(hostKey, `Cleared the SSH host for ${hostKey}`)}>Clear</button>}
    </div>
    {others.length > 0 && <>
      <h4>Other hostnames in this browser</h4>
      <ul className="settings-host-list">{others.map(([key, value]) => <li key={key}>
        <span><strong>{key}</strong> → {value}</span>
        <button type="button" aria-label={`Remove SSH host for ${key}`} onClick={() => remove(key, `Removed the SSH host for ${key}`)}>Remove</button>
      </li>)}</ul>
    </>}
  </section>
}
