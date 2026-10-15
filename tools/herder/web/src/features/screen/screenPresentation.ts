import type { Pane } from '../../types.ts'

export const unattributedTerminalWarning = 'Herdr cannot attribute this terminal; it may belong to an unplaced agent.'

export function screenPanePresentation(pane: Pane) {
  if (pane.agent === '-') return {
    label: pane.current_command?.trim() || pane.label || 'Terminal',
    warning: unattributedTerminalWarning,
  }
  return {
    label: pane.label || pane.agent,
    warning: '',
  }
}
