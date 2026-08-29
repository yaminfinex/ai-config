import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import process from 'node:process'
import { URL } from 'node:url'

const themes = JSON.parse(readFileSync(new URL('../src/features/git/pierre-themes.json', import.meta.url), 'utf8'))

function luminance(hex) {
  const channels = hex.slice(1).match(/.{2}/g).map((pair) => Number.parseInt(pair, 16) / 255)
  const [red, green, blue] = channels.map((value) => value <= 0.03928 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4)
  return 0.2126 * red + 0.7152 * green + 0.0722 * blue
}

function contrast(a, b) {
  const [lighter, darker] = [luminance(a), luminance(b)].sort((left, right) => right - left)
  return (lighter + 0.05) / (darker + 0.05)
}

for (const theme of [themes.light, themes.dark]) {
  const background = theme.colors['editor.background']
  const foregrounds = [theme.colors['editor.foreground'], ...theme.tokenColors.flatMap((token) => token.settings.foreground ? [token.settings.foreground] : [])]
  for (const foreground of foregrounds) assert.ok(contrast(foreground, background) >= 4.5, `${theme.name}: ${foreground} on ${background} misses WCAG AA`)
}

process.stdout.write('Pierre syntax themes pass WCAG AA contrast checks\n')
