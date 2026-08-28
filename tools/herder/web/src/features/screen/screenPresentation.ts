import type { AgentDetail, Pane } from '../../types.ts'

export const unattributedTerminalWarning = 'Herdr cannot attribute this terminal; it may belong to an unplaced agent.'

export function screenPanePresentation(pane: Pane) {
  if (pane.agent === '-') return {
    label: 'Unattributed terminal',
    warning: unattributedTerminalWarning,
  }
  return {
    label: pane.label || pane.agent,
    warning: '',
  }
}

export function agentScreenChoice(agent: AgentDetail | undefined, selectedPaneID: string | undefined) {
  const paneID = agent?.pane?.pane_id
  if (!agent) return { enabled: false, active: false, paneID: undefined, reason: 'Resolving live pane…' }
  if (agent.bus_status === 'retired') return { enabled: false, active: false, paneID: undefined, reason: 'Retired agents have no live pane.' }
  if (!paneID) return { enabled: false, active: false, paneID: undefined, reason: 'No live pane.' }
  return { enabled: true, active: selectedPaneID === paneID, paneID, reason: '' }
}
