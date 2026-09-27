import { useEffect, useRef, useState } from 'react'
import { ModalDialog } from '../../shared/ModalDialog.tsx'
import { HostAliasForm } from './HostAliasForm.tsx'
import { currentHostKey, useVSCodeHosts, vscodeHosts } from './vscodeHosts.ts'
import { folderName, vscodeRemoteURL } from './vscodeModel.ts'

// The VS Code mark as one currentColor path (Simple Icons, CC0), so it takes
// the strip's accent colour in either theme like the text controls beside it.
const vscodeMark = <svg viewBox="0 0 24 24" width="11" height="11" aria-hidden="true" focusable="false">
  <path d="M23.15 2.587 18.21.21a1.494 1.494 0 0 0-1.705.29l-9.46 8.63-4.12-3.128a.999.999 0 0 0-1.276.057L.327 7.261A1 1 0 0 0 .326 8.74L3.899 12 .326 15.26a1 1 0 0 0 .001 1.479L1.65 17.94a.999.999 0 0 0 1.276.057l4.12-3.128 9.46 8.63a1.492 1.492 0 0 0 1.704.29l4.942-2.377A1.5 1.5 0 0 0 24 20.06V3.939a1.5 1.5 0 0 0-.85-1.352zm-5.146 14.861L10.826 12l7.178-5.448v10.896z" />
</svg>

// VSCodeLink is a plain vscode:// anchor once this hostname has an SSH alias.
// Before that it is a button that asks for the alias just in time; saving
// stores it and follows the new link in the same gesture.
export function VSCodeLink({ cwd }: { cwd: string }) {
  const hosts = useVSCodeHosts()
  const hostKey = currentHostKey()
  const alias = hosts[hostKey]
  const [prompting, setPrompting] = useState(false)
  const wrap = useRef<HTMLSpanElement>(null)
  const dialog = useRef<HTMLDialogElement>(null)
  const refocus = useRef(false)
  const url = alias ? vscodeRemoteURL(alias, cwd) : null
  // Closing the dialog returns focus to the button that opened it, but a save
  // swaps that button for the anchor, so focus follows to the new element.
  useEffect(() => {
    if (!refocus.current) return
    refocus.current = false
    wrap.current?.querySelector<HTMLElement>('a, button')?.focus()
  }, [prompting, url])
  if (!cwd.startsWith('/')) return null
  const label = `Open ${folderName(cwd)} in VS Code`
  return <span className="context-vscode-wrap" ref={wrap}>
    {url
      ? <a className="context-fact context-vscode" href={url} aria-label={label} title={label}>{vscodeMark}</a>
      : <button type="button" className="context-fact context-vscode" aria-label={label} aria-haspopup="dialog" title={label} onClick={() => setPrompting(true)}>{vscodeMark}</button>}
    {prompting && <ModalDialog className="launch-agent-dialog vscode-alias-dialog" labelledBy="vscode-alias-title" dialogRef={dialog}
      onClose={() => { refocus.current = true; setPrompting(false) }}>
      <header><strong id="vscode-alias-title">Open in VS Code</strong><button type="button" aria-label="Close" onClick={() => dialog.current?.close()}>×</button></header>
      <HostAliasForm hostKey={hostKey} initial="" submitLabel="Save and open" onCancel={() => dialog.current?.close()} onSave={(value) => {
        const saved = vscodeHosts.set(hostKey, value)
        dialog.current?.close()
        const next = saved.ok ? vscodeRemoteURL(saved.value, cwd) : null
        if (next) window.location.href = next
      }} />
    </ModalDialog>}
  </span>
}
