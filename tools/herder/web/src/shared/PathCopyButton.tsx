import { useState } from 'react'
import { copyPath, type CopyPathState } from './pathCopyModel'

function copyWithHiddenTextarea(value: string): boolean {
  const textarea = document.createElement('textarea')
  const previousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null
  textarea.value = value
  textarea.readOnly = true
  textarea.tabIndex = -1
  textarea.setAttribute('aria-hidden', 'true')
  textarea.style.cssText = 'position:fixed;left:-9999px;top:0;opacity:0;pointer-events:none'
  document.body.append(textarea)
  textarea.select()
  try {
    return document.execCommand('copy')
  } finally {
    textarea.remove()
    previousFocus?.focus({ preventScroll: true })
  }
}

export function PathCopyButton({ path }: { path: string }) {
  const [state, setState] = useState<CopyPathState | 'idle' | 'copying'>('idle')
  const label = state === 'copying' ? 'Copying…' : state === 'copied' ? 'Copied' : state === 'failed' ? 'Copy failed' : 'Copy path'
  return <button type="button" className={`path-copy ${state}`} title={`Copy ${path}`} aria-live="polite" disabled={state === 'copying'} onClick={() => {
    setState('copying')
    void copyPath(navigator.clipboard, path, copyWithHiddenTextarea).then(setState)
  }}>{label}</button>
}
