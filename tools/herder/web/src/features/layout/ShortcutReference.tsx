import { shortcutLabels } from './shellShortcuts'
import { openInSideKeys } from './openPlacement'

// The lens's bare keys, and the zoom's. Neither fires from a text box.
const lensKeys = [
  ['← → / h l', 'Select the previous or next space'],
  ['↓ ↑ / j k', 'Select in the row below or above'],
  ['Enter / double-click', 'Zoom into the selected space'],
  ['n', 'Select the next space that needs you'],
  ['N', 'Open the next space, or agent in no space, that needs you'],
  ['1 / 2 / 3', 'Put the selected space in focus, watch or background'],
  ['v', 'Show the next agent on the selected card'],
  ['m', 'Mark the selected space read / unread'],
  ['u', 'Mark the selected space’s agents unread'],
  ['t', 'Card text: last answer, status or folder'],
  ['s', 'Card size: the next of four'],
] as const
const zoomKeys = [
  ['Esc', 'Back to the lens'],
  ['n / N', 'Next space (N: or agent in no space) that needs you'],
] as const

export function ShortcutReference({ open, lens, onClose }: { open: boolean, lens: boolean, onClose: () => void }) {
  if (!open) return null
  const labels = shortcutLabels(navigator.userAgent)
  const shortcuts = [
    ...(lens ? lensKeys : zoomKeys),
    [labels.closePanel, 'Close active pane'],
    [labels.quickOpen, 'Open spaces, agents, files, or folders'],
    [labels.switchTabs, 'Switch tabs'],
    [labels.switchSpaces, 'Previous or next space in lens order'],
    [labels.spaceSwitcher, 'Switch to recent spaces (hold ⌥/Alt to choose)'],
    [labels.focusFleet, 'Focus fleet sidebar'],
    [labels.toggleNotesRail, 'Toggle notes rail'],
    [labels.focusComposer, 'Focus active composer'],
    [labels.sendRequest, 'Send request'],
    [labels.fileBack, 'Send, then back to the lens with the agent read'],
    [labels.leaveComposer, 'Leave text box'],
    [labels.goToTop, 'Go to top'],
    [labels.goToBottom, 'Go to bottom and resume follow'],
    [labels.toggleMaximize, 'Maximize or restore active pane'],
    [labels.toggleRead, 'Toggle the active agent read or unread'],
    [labels.herdrView, 'Open or leave herdr itself (while open, every other key is herdr\'s)'],
    [openInSideKeys(navigator.userAgent), 'Open a link or path in closest side group'],
    [`${openInSideKeys(navigator.userAgent)} a transcript entry`, 'Mark unread from that entry'],
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
