import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

import { screenPanePresentation, unattributedTerminalWarning } from '../src/features/screen/screenPresentation.ts'
import type { Pane } from '../src/types.ts'

const pane: Pane = {
  pane_id: 'w4R:p5', label: 'pjsafter-rava', agent: '-', tool: '-',
  herdr_status: 'unknown', bus_status: '-', gap: 'no bus row',
}

test('agent-less Herdr panes are described as unattributed, never agent-free', () => {
  assert.deepEqual(screenPanePresentation(pane), {
    label: 'pjsafter-rava',
    warning: unattributedTerminalWarning,
  })
  assert.equal(screenPanePresentation({ ...pane, current_command: ' htop ' }).label, 'htop')
  const sidebar = readFileSync(new URL('../src/features/sidebar/FleetSidebar.tsx', import.meta.url), 'utf8')
  assert.match(sidebar, /unattributedTerminalWarning/)
  assert.doesNotMatch(sidebar, />shell</)
})

test('the agent panel has no Screen view; a terminal no agent owns opens in the herdr view', () => {
  const agentPanel = readFileSync(new URL('../src/features/transcript/AgentPanel.tsx', import.meta.url), 'utf8')
  assert.doesNotMatch(agentPanel, />Screen</)
  assert.doesNotMatch(agentPanel, /agent-view-toggle|ScreenViewport|screenMode/)
  const sidebar = readFileSync(new URL('../src/features/sidebar/FleetSidebar.tsx', import.meta.url), 'utf8')
  assert.match(sidebar, /node\.kind === 'pane' && node\.pane\?\.agent === '-'\) onOpenTerminal\(node\.pane as Pane\)/)
  const app = readFileSync(new URL('../src/App.tsx', import.meta.url), 'utf8')
  assert.match(app, /const openTerminal = useCallback\(\(pane: Pane\) => showTerminal\(pane\.pane_id\), \[showTerminal\]\)/)
  assert.match(app, /onOpenTerminal=\{openTerminal\}/)
  assert.match(app, /terminal: herdr\.terminal \}/)
  const herdr = readFileSync(new URL('../src/features/herdr/useHerdrView.ts', import.meta.url), 'utf8')
  // Leaving the view lets go of the terminal: the next open follows the agent again.
  assert.match(herdr, /const leave = useCallback\(\(\) => \{\n\s*setOpen\(false\)\n\s*setTerminal\(undefined\)/)
})
