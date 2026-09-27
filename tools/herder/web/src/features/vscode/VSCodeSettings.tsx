import { useState } from 'react'
import { HostAliasForm } from './HostAliasForm.tsx'
import { currentHostKey, useVSCodeHosts, vscodeHosts } from './vscodeHosts.ts'
import { otherHosts } from './vscodeModel.ts'

export function VSCodeSettings() {
  const hosts = useVSCodeHosts()
  const hostKey = currentHostKey()
  const alias = hosts[hostKey] ?? ''
  const [saved, setSaved] = useState('')
  const others = otherHosts(hosts, hostKey)
  return <section className="settings-section" aria-labelledby="settings-vscode-title">
    <h3 id="settings-vscode-title">VS Code Remote-SSH</h3>
    <HostAliasForm key={`${hostKey}:${alias}`} hostKey={hostKey} initial={alias} submitLabel="Save"
      onSave={(value) => { const result = vscodeHosts.set(hostKey, value); setSaved(result.ok ? `Saved ${result.value} for ${hostKey}.` : '') }} />
    <div className="settings-row-actions">
      <span role="status" aria-live="polite">{alias ? saved || `Links open over SSH host ${alias}.` : saved || 'Not set. The VS Code button asks on first use.'}</span>
      {alias && <button type="button" onClick={() => { vscodeHosts.remove(hostKey); setSaved(`Cleared the SSH host for ${hostKey}.`) }}>Clear</button>}
    </div>
    {others.length > 0 && <>
      <h4>Other hostnames in this browser</h4>
      <ul className="settings-host-list">{others.map(([key, value]) => <li key={key}>
        <span><strong>{key}</strong> → {value}</span>
        <button type="button" aria-label={`Remove SSH host for ${key}`} onClick={() => vscodeHosts.remove(key)}>Remove</button>
      </li>)}</ul>
    </>}
  </section>
}
