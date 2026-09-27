import { ModalDialog } from '../../shared/ModalDialog.tsx'
import { VSCodeSettings } from '../vscode/index.ts'

// SettingsPanel holds browser-local preferences in a native modal dialog, so
// Escape closes it and focus returns to whatever opened it.
export function SettingsPanel({ open, onClose }: { open: boolean, onClose: () => void }) {
  if (!open) return null
  return <ModalDialog className="shortcut-reference settings-panel" labelledBy="settings-title" onClose={onClose}>
    <header><strong id="settings-title">Settings</strong><form method="dialog"><button type="submit" aria-label="Close settings">×</button></form></header>
    <VSCodeSettings />
  </ModalDialog>
}
