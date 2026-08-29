import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const themes = JSON.parse(readFileSync(new URL('../src/features/git/pierre-themes.json', import.meta.url), 'utf8')) as {
  light: { name: string, type: string, colors: Record<string, string>, tokenColors: Array<{ settings: { foreground?: string } }> }
  dark: { name: string, type: string, colors: Record<string, string>, tokenColors: Array<{ settings: { foreground?: string } }> }
}
const stylesheet = readFileSync(new URL('../src/styles.css', import.meta.url), 'utf8')
const backgroundTokens = ['bg2', 'diff-add-bg', 'diff-add-emphasis', 'diff-delete-bg', 'diff-delete-emphasis', 'diff-selection']

function themeBackgrounds(type: string) {
  const block = stylesheet.match(new RegExp(`:root\\[data-theme='${type}'\\] \\{([\\s\\S]*?)\\n\\}`, 'u'))?.[1]
  assert.ok(block)
  const variables = new Map([...block.matchAll(/--([\w-]+):\s*(#[\da-f]{6})\b/giu)].map((match) => [match[1], match[2]]))
  return backgroundTokens.map((token) => variables.get(token) ?? '')
}

function luminance(hex: string) {
  assert.match(hex, /^#[\da-f]{6}$/iu)
  const values = hex.slice(1).match(/.{2}/g)?.map((pair) => Number.parseInt(pair, 16) / 255) ?? []
  const [red, green, blue] = values.map((value) => value <= 0.03928 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4)
  return 0.2126 * red + 0.7152 * green + 0.0722 * blue
}

function contrast(a: string, b: string) {
  const [lighter, darker] = [luminance(a), luminance(b)].sort((left, right) => right - left)
  return (lighter + 0.05) / (darker + 0.05)
}

test('both generated Pierre syntax palettes meet AA on every rendered diff background', () => {
  for (const theme of [themes.light, themes.dark]) {
    assert.match(theme.name, /^herder-(?:light|dark)$/u)
    const backgrounds = themeBackgrounds(theme.type)
    assert.equal(theme.colors['editor.background'], backgrounds[0])
    const foregrounds = [theme.colors['editor.foreground'], ...theme.tokenColors.flatMap((token) => token.settings.foreground ? [token.settings.foreground] : [])]
    for (const foreground of foregrounds) for (const background of backgrounds) {
      assert.ok(contrast(foreground, background) >= 4.5, `${theme.name}: ${foreground} on ${background}`)
    }
  }
})
