import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const filePanel = readFileSync(new URL('../src/features/files/FilePanel.tsx', import.meta.url), 'utf8')
const agentPanel = readFileSync(new URL('../src/features/transcript/AgentPanel.tsx', import.meta.url), 'utf8')
const resolver = readFileSync(new URL('../src/features/files/TranscriptFileResolver.tsx', import.meta.url), 'utf8')

test('file and transcript panels share one double-click resolver', () => {
  assert.match(agentPanel, /useTranscriptFileResolver\(name, active && !screenMode, onOpenFile, onOpenFolder\)/u)
  assert.match(filePanel, /useTranscriptFileResolver\(resolverContext, active && gitState\.mode === 'current', onOpenFile, onOpenFolder\)/u)
  assert.match(filePanel, /className="file-content"[^>]+onDoubleClick=\{fileResolver\.onDoubleClick\}/u)
  assert.match(filePanel, /<Markdown components=\{fileMarkdownComponents\}>/u)
  assert.match(filePanel, /<PierreFile path=/u)
  assert.equal((resolver.match(/function useTranscriptFileResolver|export function useTranscriptFileResolver/gu) ?? []).length, 1)
})

test('shared token lookup pierces Pierre open shadow roots without changing Pierre', () => {
  assert.match(resolver, /nativeEvent\.composedPath\(\)/u)
  assert.match(resolver, /caretPositionFromPoint\?\.\(x, y, shadowRoots\.length > 0 \? \{ shadowRoots \} : undefined\)/u)
})

test('note capture aborts any in-flight resolver request before closing it', () => {
  const cancelStart = resolver.indexOf('const cancel = useCallback')
  const cancelEnd = resolver.indexOf('\n  }', cancelStart)
  const cancel = resolver.slice(cancelStart, cancelEnd)
  assert.ok(cancelStart >= 0)
  assert.match(cancel, /request\.current\?\.abort\(\)/)
  assert.match(cancel, /request\.current = null/)
  assert.match(cancel, /close\(\)/)
  assert.match(resolver, /useDOMEvent\(window, noteCaptureGestureEvent, cancel/)
})
