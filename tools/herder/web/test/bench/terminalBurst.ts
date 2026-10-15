// A rough speed note, not a gate: how long xterm.js and ghostty-web take to
// paint a 5 MB burst of mixed ANSI in headless Chrome.
//   AGENT_BROWSER_ARGS=--no-sandbox node --experimental-strip-types test/bench/terminalBurst.ts
import type { TestContext } from 'node:test'

import { fixtureAPI, startBrowser } from '../browserFixture.ts'

const cleanups: (() => unknown)[] = []
const context = { after: (cleanup: () => unknown) => { cleanups.push(cleanup) } } as unknown as TestContext
const runs = 5
try {
  const { url, browser, evaluate } = await startBrowser(context, 'terminal-burst', fixtureAPI([]).plugin)
  await browser(['open', url])
  await browser(['set', 'viewport', '1200', '800'])
  const result = await evaluate(`(async () => {
    const page = await import('/test/bench/burstPage.ts')
    const data = page.burst(5 * 1024 * 1024)
    const times = { xterm: [], ghostty: [] }
    // One warm-up each (WASM compile, font measuring), then alternate.
    await page.paintXterm(data.subarray(0, 65536)); await page.paintGhostty(data.subarray(0, 65536))
    const screens = { xterm: '', ghostty: '' }
    for (let run = 0; run < ${runs}; run++) {
      const xterm = await page.paintXterm(data), ghostty = await page.paintGhostty(data)
      times.xterm.push(xterm.elapsed); times.ghostty.push(ghostty.elapsed)
      screens.xterm = xterm.text; screens.ghostty = ghostty.text
    }
    // ghostty-web's translateToString trims written spaces and drops empty
    // cells mid-row, so the screens are compared with runs of spaces folded.
    const fold = (text) => text.split('\\n').map((row) => row.replace(/ +/g, ' ').trim()).join('\\n')
    return { bytes: data.length, times, same: fold(screens.xterm) === fold(screens.ghostty), nonEmpty: screens.ghostty.trim().length > 0 }
  })()`) as { bytes: number, times: Record<'xterm' | 'ghostty', number[]>, same: boolean, nonEmpty: boolean }
  const median = (values: number[]) => [...values].sort((a, b) => a - b)[Math.floor(values.length / 2)]
  console.log(`${(result.bytes / 1024 / 1024).toFixed(2)} MiB of mixed ANSI, ${runs} runs each, headless Chrome; final screens ${result.same ? 'identical' : 'DIFFER'}${result.nonEmpty ? '' : ' (EMPTY)'}`)
  for (const kind of ['xterm', 'ghostty'] as const) {
    const ms = median(result.times[kind])
    console.log(`${kind.padEnd(8)} median ${ms.toFixed(0)} ms (${(result.bytes / 1024 / 1024 / (ms / 1000)).toFixed(1)} MiB/s); runs ${result.times[kind].map((time) => time.toFixed(0)).join(', ')}`)
  }
} finally {
  for (const cleanup of cleanups.reverse()) await cleanup()
}
