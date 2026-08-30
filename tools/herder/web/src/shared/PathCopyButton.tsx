import { useState } from 'react'
import { copyPath, type CopyPathState } from './pathCopyModel'

export function PathCopyButton({ path }: { path: string }) {
  const [state, setState] = useState<CopyPathState | 'idle' | 'copying'>('idle')
  const label = state === 'copying' ? 'Copying…' : state === 'copied' ? 'Copied' : state === 'failed' ? 'Copy failed' : 'Copy path'
  return <button type="button" className={`path-copy ${state}`} title={`Copy ${path}`} aria-live="polite" disabled={state === 'copying'} onClick={() => {
    setState('copying')
    void copyPath(navigator.clipboard, path).then(setState)
  }}>{label}</button>
}
