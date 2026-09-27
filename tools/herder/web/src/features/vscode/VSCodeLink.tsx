import { useEffect, useRef, useState } from 'react'
import { ModalDialog } from '../../shared/ModalDialog.tsx'
import { HostAliasForm } from './HostAliasForm.tsx'
import { currentHostKey, useVSCodeHosts, vscodeHosts } from './vscodeHosts.ts'
import { folderName, vscodeRemoteURL } from './vscodeModel.ts'

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
  const content = <>VS Code<span aria-hidden="true">↗</span></>
  return <span className="context-vscode-wrap" ref={wrap}>
    {url
      ? <a className="context-fact context-vscode" href={url} aria-label={label} title={`${label} over SSH host ${alias} · ${cwd}`}>{content}</a>
      : <button type="button" className="context-fact context-vscode" aria-label={label} aria-haspopup="dialog" title={`${label} · set the SSH host for ${hostKey} first`} onClick={() => setPrompting(true)}>{content}</button>}
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
