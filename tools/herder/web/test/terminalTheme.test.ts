import assert from 'node:assert/strict'
import test from 'node:test'

import { contrastRatio, terminalTheme } from '../src/features/screen/terminalTheme.ts'

test('the terminal has one complete dark ANSI palette in both app themes', () => {
  const ansi = ['black', 'red', 'green', 'yellow', 'blue', 'magenta', 'cyan', 'white', 'brightBlack', 'brightRed', 'brightGreen', 'brightYellow', 'brightBlue', 'brightMagenta', 'brightCyan', 'brightWhite'] as const
  ansi.forEach((name) => assert.match(terminalTheme[name], /^#[0-9a-f]{6}$/i, name))
  assert.equal(terminalTheme.background, '#0d0f12')
  assert.ok(contrastRatio(terminalTheme.foreground, terminalTheme.background) >= 4.5)
  assert.ok(contrastRatio(terminalTheme.selectionForeground, terminalTheme.selectionBackground) >= 4.5)
})
