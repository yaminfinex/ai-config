import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

import {
  applyTheme,
  cycleThemePreference,
  persistThemePreference,
  readThemePreference,
  resolveTheme,
  themeStorageKey,
} from '../src/theme.ts'

function memoryStorage(initial: Record<string, string> = {}) {
  const values = new Map(Object.entries(initial))
  return {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => { values.set(key, value) },
    removeItem: (key: string) => { values.delete(key) },
  }
}

function themeBlocks(css: string) {
  return [...css.matchAll(/:root\[data-theme='(?:light|dark)'\] \{([\s\S]*?)\n\}/g)].map((match) => match[1])
}

function luminance(hex: string) {
  const channels = [1, 3, 5].map((index) => Number.parseInt(hex.slice(index, index + 2), 16) / 255)
    .map((value) => value <= 0.04045 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4)
  return 0.2126 * channels[0] + 0.7152 * channels[1] + 0.0722 * channels[2]
}

function contrast(foreground: string, background: string) {
  const foregroundLuminance = luminance(foreground)
  const backgroundLuminance = luminance(background)
  return (Math.max(foregroundLuminance, backgroundLuminance) + 0.05) / (Math.min(foregroundLuminance, backgroundLuminance) + 0.05)
}

function rule(css: string, selector: string) {
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  const block = css.match(new RegExp(`${escaped} \\{([^}]*)\\}`))?.[1]
  assert.ok(block, `${selector} must have a concrete style rule`)
  return block
}

function token(block: string, property: string) {
  const name = block.match(new RegExp(`${property}: var\\(--([\\w-]+)\\)`))?.[1]
  assert.ok(name, `${property} must use a theme token`)
  return name
}

test('theme preference uses one global v1 key and defaults invalid or missing values to system', () => {
  assert.equal(themeStorageKey, 'herder.web.theme.v1')
  assert.equal(readThemePreference(memoryStorage()), 'system')
  assert.equal(readThemePreference(memoryStorage({ [themeStorageKey]: 'sepia' })), 'system')
  assert.equal(readThemePreference(memoryStorage({ [themeStorageKey]: 'light' })), 'light')
  assert.equal(readThemePreference(memoryStorage({ [themeStorageKey]: 'dark' })), 'dark')
})

test('manual themes persist while system removes the global override', () => {
  const storage = memoryStorage()
  persistThemePreference('light', storage)
  assert.equal(readThemePreference(storage), 'light')
  persistThemePreference('dark', storage)
  assert.equal(readThemePreference(storage), 'dark')
  persistThemePreference('system', storage)
  assert.equal(readThemePreference(storage), 'system')
})

test('blocked browser storage degrades to system preference without throwing', () => {
  const blocked = {
    getItem: () => { throw new Error('blocked') },
    setItem: () => { throw new Error('blocked') },
    removeItem: () => { throw new Error('blocked') },
  }
  assert.equal(readThemePreference(blocked), 'system')
  assert.doesNotThrow(() => persistThemePreference('dark', blocked))
})

test('system resolution follows the media preference and the control cycles all three states', () => {
  assert.equal(resolveTheme('system', false), 'light')
  assert.equal(resolveTheme('system', true), 'dark')
  assert.equal(resolveTheme('light', true), 'light')
  assert.equal(resolveTheme('dark', false), 'dark')
  assert.equal(cycleThemePreference('system'), 'light')
  assert.equal(cycleThemePreference('light'), 'dark')
  assert.equal(cycleThemePreference('dark'), 'system')
})

test('resolved theme is applied to the document root', () => {
  const attributes = new Map<string, string>()
  const root = { setAttribute: (name: string, value: string) => { attributes.set(name, value) } }
  applyTheme('dark', root)
  assert.equal(attributes.get('data-theme'), 'dark')
  applyTheme('light', root)
  assert.equal(attributes.get('data-theme'), 'light')
})

test('head bootstrap applies the stored or system theme before the application module', () => {
  const html = readFileSync(new URL('../index.html', import.meta.url), 'utf8')
  const bootstrap = html.indexOf(themeStorageKey)
  const application = html.indexOf('src="/src/main.tsx"')
  assert.ok(bootstrap > 0, 'theme bootstrap should name the storage contract')
  assert.ok(application > bootstrap, 'theme bootstrap should run before the application module')
  assert.match(html.slice(bootstrap, application), /matchMedia\('\(prefers-color-scheme: dark\)'\)/)
  assert.match(html.slice(bootstrap, application), /dataset\.theme/)
})

test('all concrete stylesheet colors live in the light/dark token declarations', () => {
  const css = readFileSync(new URL('../src/styles.css', import.meta.url), 'utf8')
  const rules = css.slice(css.indexOf('* {'))
  assert.equal(rules.match(/#[\da-f]{3,8}\b|rgba?\(|hsla?\(/gi), null)
})

test('compact pill text tokens meet WCAG AA contrast in both themes', () => {
  const css = readFileSync(new URL('../src/styles.css', import.meta.url), 'utf8')
  for (const block of themeBlocks(css)) {
    for (const [tone, tokenPrefix] of [
      ['tool', 'pill-tool'],
      ['thinking', 'pill-thinking'],
      ['message', 'pill-message'],
      ['other', 'pill-other'],
      ['assistant-status', 'info'],
    ]) {
      const border = block.match(new RegExp(`--${tokenPrefix}-border: (#[\\da-f]{6})`, 'i'))?.[1]
      const foreground = block.match(new RegExp(`--${tokenPrefix}-text: (#[\\da-f]{6})`, 'i'))?.[1]
      const background = block.match(new RegExp(`--${tokenPrefix}-bg: (#[\\da-f]{6})`, 'i'))?.[1]
      assert.ok(border && foreground && background, `${tone} pill tokens must be concrete theme declarations`)
      const ratio = contrast(foreground, background)
      assert.ok(ratio >= 4.5, `${tone} pill contrast ${ratio.toFixed(2)} must meet WCAG AA`)
    }
  }
})

test('dock tab text uses AA theme token pairs with no stock skin colors', () => {
  const css = readFileSync(new URL('../src/styles.css', import.meta.url), 'utf8')
  for (const block of themeBlocks(css)) {
    for (const [foregroundName, backgroundName] of [['text', 'bg'], ['dim', 'bg2']]) {
      const foreground = block.match(new RegExp(`--${foregroundName}: (#[\\da-f]{6})`, 'i'))?.[1]
      const background = block.match(new RegExp(`--${backgroundName}: (#[\\da-f]{6})`, 'i'))?.[1]
      assert.ok(foreground && background)
      const ratio = contrast(foreground, background)
      assert.ok(ratio >= 4.5, `${foregroundName} on ${backgroundName} contrast ${ratio.toFixed(2)} must meet WCAG AA`)
    }
  }
  const dockTheme = css.slice(css.indexOf('.dockview-theme-herder'), css.indexOf('.dockview-theme-herder .dv-drop-target-container'))
  assert.match(dockTheme, /--dv-activegroup-visiblepanel-tab-color: var\(--text\)/)
  assert.match(dockTheme, /--dv-activegroup-hiddenpanel-tab-color: var\(--dim\)/)
  assert.equal(dockTheme.match(/#[\da-f]{3,8}\b|rgba?\(|hsla?\(/gi), null)
})

test('inline link text token meets WCAG AA across both theme surfaces', () => {
  const css = readFileSync(new URL('../src/styles.css', import.meta.url), 'utf8')
  assert.match(css, /\.context-repository a \{[^}]*color: var\(--accent-text\);[^}]*text-decoration[^}]*solid/s)
  for (const block of themeBlocks(css)) {
    const foreground = block.match(/--accent-text: (#[\da-f]{6})/i)?.[1]
    assert.ok(foreground, 'inline links must use a concrete theme token')
    for (const backgroundName of ['bg', 'bg2', 'human-bg', 'operator-bg']) {
      const background = block.match(new RegExp(`--${backgroundName}: (#[\\da-f]{6})`, 'i'))?.[1]
      assert.ok(background, `${backgroundName} must be a concrete theme declaration`)
      const ratio = contrast(foreground, background)
      assert.ok(ratio >= 4.5, `inline link on ${backgroundName} contrast ${ratio.toFixed(2)} must meet WCAG AA`)
    }
  }
})

test('notes action labels, count badges, and orphan flags meet WCAG AA in both themes', () => {
  const css = readFileSync(new URL('../src/styles.css', import.meta.url), 'utf8')
  const pairs = [
    ['action-bar labels', token(rule(css, '.notes-action-bar label'), 'color'), token(rule(css, '.notes-action-bar'), 'background')],
    ['group-count badge', token(rule(css, '.notes-group-heading span'), 'color'), token(rule(css, '.notes-group-heading span'), 'background')],
    ['orphan flag', token(rule(css, '.notes-group-heading em'), 'color'), token(rule(css, '.notes-group-heading'), 'background')],
  ]
  for (const block of themeBlocks(css)) {
    for (const [label, foregroundName, backgroundName] of pairs) {
      const foreground = block.match(new RegExp(`--${foregroundName}: (#[\\da-f]{6})`, 'i'))?.[1]
      const background = block.match(new RegExp(`--${backgroundName}: (#[\\da-f]{6})`, 'i'))?.[1]
      assert.ok(foreground && background, `${label} tokens must be concrete theme declarations`)
      const ratio = contrast(foreground, background)
      assert.ok(ratio >= 4.5, `${label} contrast ${ratio.toFixed(2)} must meet WCAG AA`)
    }
  }
})
