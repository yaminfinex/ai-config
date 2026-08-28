const shortcuts = [
  ['Alt+W', 'Close active pane'],
  ['Ctrl/Cmd+K', 'Quick open file or folder'],
  ['Ctrl/Cmd+PageUp/PageDown', 'Switch tabs'],
  ['Alt+1', 'Focus fleet sidebar'],
  ['Alt+2', 'Focus active composer'],
  ['Ctrl/Cmd+Enter', 'Send request'],
  ['?', 'Open this reference'],
] as const

export function ShortcutReference({ open, onClose }: { open: boolean, onClose: () => void }) {
  if (!open) return null
  return <div className="shortcut-backdrop" role="presentation" onPointerDown={(event) => { if (event.target === event.currentTarget) onClose() }}>
    <section className="shortcut-reference" role="dialog" aria-modal="true" aria-labelledby="shortcut-title">
      <header><strong id="shortcut-title">Keyboard shortcuts</strong><button type="button" aria-label="Close shortcut reference" onClick={onClose}>×</button></header>
      <dl>{shortcuts.map(([keys, action]) => <div key={keys}><dt><kbd>{keys}</kbd></dt><dd>{action}</dd></div>)}</dl>
      <p><strong>Browser close:</strong> Ctrl/Cmd+W belongs to the browser and closes its tab. Herder does not intercept it.</p>
      <p><strong>Refresh:</strong> pinned layout restores on return; untouched previews do not.</p>
    </section>
  </div>
}
