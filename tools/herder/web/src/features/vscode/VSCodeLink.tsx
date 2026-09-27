import { useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
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
  const input = useRef<HTMLInputElement>(null)
  const wasPrompting = useRef(false)
  useEffect(() => {
    if (prompting) input.current?.focus()
    else if (wasPrompting.current) wrap.current?.querySelector<HTMLElement>('a, button')?.focus()
    wasPrompting.current = prompting
  }, [prompting])
  if (!cwd.startsWith('/')) return null
  const label = `Open ${folderName(cwd)} in VS Code`
  const url = alias ? vscodeRemoteURL(alias, cwd) : null
  const content = <>VS Code<span aria-hidden="true">↗</span></>
  return <span className="context-vscode-wrap" ref={wrap}>
    {url
      ? <a className="context-fact context-vscode" href={url} aria-label={label} title={`${label} over SSH host ${alias} · ${cwd}`}>{content}</a>
      : <button type="button" className="context-fact context-vscode" aria-label={label} aria-haspopup="dialog" title={`${label} · set the SSH host for ${hostKey} first`} onClick={() => setPrompting(true)}>{content}</button>}
    {prompting && createPortal(<div className="launch-agent-backdrop" onMouseDown={(event) => { if (event.target === event.currentTarget) setPrompting(false) }}>
      <section className="launch-agent-dialog vscode-alias-dialog" role="dialog" aria-modal="true" aria-labelledby="vscode-alias-title"
        onKeyDown={(event) => { if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); setPrompting(false) } }}>
        <header><strong id="vscode-alias-title">Open in VS Code</strong><button type="button" aria-label="Close" onClick={() => setPrompting(false)}>×</button></header>
        <HostAliasForm hostKey={hostKey} initial="" submitLabel="Save and open" inputRef={input} onCancel={() => setPrompting(false)} onSave={(value) => {
          const saved = vscodeHosts.set(hostKey, value)
          setPrompting(false)
          const next = saved.ok ? vscodeRemoteURL(saved.value, cwd) : null
          if (next) window.location.href = next
        }} />
      </section>
    </div>, document.body)}
  </span>
}
