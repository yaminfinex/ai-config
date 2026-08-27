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
