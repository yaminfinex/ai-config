import { shortcutLabels } from './shellShortcuts'
import { openInSideKeys } from './openPlacement'

export function ShortcutReference({ open, onClose }: { open: boolean, onClose: () => void }) {
  if (!open) return null
  const labels = shortcutLabels(navigator.userAgent)
  const shortcuts = [
    [labels.closePanel, 'Close active pane'],
    [labels.quickOpen, 'Quick open file or folder'],
    [labels.switchTabs, 'Switch tabs'],
    [labels.focusFleet, 'Focus fleet sidebar'],
    [labels.focusComposer, 'Focus active composer'],
    [labels.sendRequest, 'Send request'],
    [openInSideKeys(navigator.userAgent), 'Open in side split'],
    ['?', 'Open this reference'],
  ] as const
  return <div className="shortcut-backdrop" role="presentation" onPointerDown={(event) => { if (event.target === event.currentTarget) onClose() }}>
    <section className="shortcut-reference" role="dialog" aria-modal="true" aria-labelledby="shortcut-title">
      <header><strong id="shortcut-title">Keyboard shortcuts</strong><button type="button" aria-label="Close shortcut reference" onClick={onClose}>×</button></header>
      <dl>{shortcuts.map(([keys, action]) => <div key={keys}><dt><kbd>{keys}</kbd></dt><dd>{action}</dd></div>)}</dl>
      <p><strong>Browser close:</strong> {labels.browserClose} belongs to the browser and closes its tab. Herder does not intercept it.</p>
      <p><strong>Refresh:</strong> your visible tab strip restores on return; previews remain previews.</p>
    </section>
  </div>
}
