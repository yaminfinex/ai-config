import { useEffect, useRef } from 'react'
import { VSCodeSettings } from '../vscode/index.ts'

// SettingsPanel holds browser-local preferences. Escape closes it and focus
// returns to whatever opened it.
export function SettingsPanel({ open, onClose }: { open: boolean, onClose: () => void }) {
  const panel = useRef<HTMLElement>(null)
  useEffect(() => {
    if (!open) return
    const previous = document.activeElement as HTMLElement | null
    (panel.current?.querySelector<HTMLElement>('input') ?? panel.current?.querySelector<HTMLElement>('button'))?.focus()
    return () => previous?.focus()
  }, [open])
  if (!open) return null
  return <div className="shortcut-backdrop" role="presentation" onPointerDown={(event) => { if (event.target === event.currentTarget) onClose() }}>
    <section ref={panel} className="shortcut-reference settings-panel" role="dialog" aria-modal="true" aria-labelledby="settings-title"
      onKeyDown={(event) => { if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); onClose() } }}>
      <header><strong id="settings-title">Settings</strong><button type="button" aria-label="Close settings" onClick={onClose}>×</button></header>
      <VSCodeSettings />
    </section>
  </div>
}
