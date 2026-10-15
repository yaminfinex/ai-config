import { shortcutLabels } from '../layout/shellShortcuts'

// Crumb is the zoom's slim line above the dock: lens › space › agent on the
// left (lens › zooms out), the zoom's keys on the right in web's chords.
export function Crumb({ space, agent, alone, onLens }: { space: string, agent: string | undefined, alone: boolean, onLens: () => void }) {
  const labels = shortcutLabels(navigator.userAgent)
  const hints = alone ? ['esc lens'] : ['esc lens', `${labels.switchTabs} tabs`, `${labels.closePanel} close`, `${labels.toggleMaximize} maximize`]
  return <nav className="lens-crumb" aria-label="Zoom">
    <span className="lens-crumb-path">
      <button type="button" className="lens-crumb-home" onClick={onLens} title="Back to the lens (esc)">lens ›</button>
      <span className="lens-crumb-space">{space}</span>
      {agent && <><span aria-hidden="true">›</span><span className="lens-crumb-agent">{agent}</span></>}
    </span>
    <span className="lens-crumb-hints">{hints.join(' · ')}</span>
  </nav>
}
