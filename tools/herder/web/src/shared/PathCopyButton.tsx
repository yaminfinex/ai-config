import { useState } from 'react'
import { copyPath, type CopyPathState } from './pathCopyModel'

export function copyWithHiddenTextarea(value: string): boolean {
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
  }}>
    <svg viewBox="0 0 14 14" aria-hidden="true" focusable="false">
      {state === 'copied' ? <path d="M2.5 7.5l3 3 6-6" />
        : state === 'failed' ? <path d="M3.5 3.5l7 7m0-7l-7 7" />
          : <><rect x="4.5" y="4.5" width="7" height="7" rx="1.5" /><path d="M9.5 2.5h-6a1 1 0 0 0-1 1v6" /></>}
    </svg>
    <span className="path-copy-status">{label}</span>
  </button>
}
