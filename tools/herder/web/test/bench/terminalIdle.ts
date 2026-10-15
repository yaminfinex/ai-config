// A rough cost note, not a gate: the main thread's busy time over 10 s with
// the herdr view on xterm.js and on ghostty-web, against the PTY fixture, in
// four states: (a) open and idle, (b) opened then hidden, (c) typing ten keys
// a second into it, (d) a 5 MiB output burst. Busy time is Chrome's own
// TaskDuration (and ScriptDuration) from Performance.getMetrics, read over
// CDP before and after each window; renders are the ghostty surface's count.
//   AGENT_BROWSER_ARGS=--no-sandbox node --experimental-strip-types test/bench/terminalIdle.ts
import type { TestContext } from 'node:test'

import { fixtureAPI, herdrSurfaceProbe, startBrowser } from '../browserFixture.ts'
import { herdrTermFixture } from '../herdrTermFixture.ts'

const windowMs = 10_000
const kinds = (process.argv[2] ?? 'xterm,ghostty').split(',') as ('xterm' | 'ghostty')[]

// cdp is one flattened session on the page target of the browser at url.
async function cdp(url: string, pageURL: string) {
  const socket = new WebSocket(url)
  await new Promise((resolve, reject) => { socket.onopen = resolve; socket.onerror = reject })
  let next = 0
  const pending = new Map<number, { resolve: (value: unknown) => void, reject: (error: Error) => void }>()
  socket.onmessage = (event) => {
    const message = JSON.parse(String(event.data)) as { id?: number, result?: unknown, error?: { message: string } }
    const waiter = message.id === undefined ? undefined : pending.get(message.id)
    if (!waiter) return
    pending.delete(message.id!)
    if (message.error) waiter.reject(new Error(message.error.message))
    else waiter.resolve(message.result)
  }
  const send = <T>(method: string, params: object = {}, sessionId?: string) => new Promise<T>((resolve, reject) => {
    const id = ++next
    pending.set(id, { resolve: resolve as (value: unknown) => void, reject })
    socket.send(JSON.stringify({ id, method, params, sessionId }))
  })
  const { targetInfos } = await send<{ targetInfos: { targetId: string, type: string, url: string }[] }>('Target.getTargets')
  const page = targetInfos.find((target) => target.type === 'page' && target.url.startsWith(pageURL))
  if (!page) throw new Error(`no page at ${pageURL}: ${JSON.stringify(targetInfos)}`)
  const { sessionId } = await send<{ sessionId: string }>('Target.attachToTarget', { targetId: page.targetId, flatten: true })
  await send('Performance.enable', { timeDomain: 'timeTicks' }, sessionId)
  return {
    send: <T>(method: string, params: object = {}) => send<T>(method, params, sessionId),
    close: () => socket.close(),
  }
}

type Metrics = Record<string, number>
type CPUProfile = { nodes: { id: number, callFrame: { functionName: string, url: string, lineNumber: number } }[], samples: number[], timeDeltas: number[] }

function printProfile(profile: CPUProfile) {
  const nodes = new Map(profile.nodes.map((node) => [node.id, node]))
  const self = new Map<string, number>()
  profile.samples.forEach((id, index) => {
    const frame = nodes.get(id)!.callFrame
    const name = `${frame.functionName || '(anonymous)'} ${frame.url.split('/').pop()?.split('?')[0]}:${frame.lineNumber + 1}`
    self.set(name, (self.get(name) ?? 0) + (profile.timeDeltas[index] ?? 0) / 1000)
  })
  for (const [name, ms] of [...self].filter(([name]) => !/^\(idle\)|^\(program\)/.test(name)).sort((a, b) => b[1] - a[1]).slice(0, 25)) console.log(`${ms.toFixed(1).padStart(8)} ms  ${name}`)
}
type Sample = { task: number, script: number, renders: number | null }

const cleanups: (() => unknown)[] = []
const context = { after: (cleanup: () => unknown) => { cleanups.push(cleanup) } } as unknown as TestContext
try {
  const api = fixtureAPI(['alpha'])
  const term = await herdrTermFixture(context)
  const { url, browser, evaluate, waitFor } = await startBrowser(context, 'terminal-idle', [api.plugin, term.plugin])
  await browser(['open', url])
  await browser(['set', 'viewport', '1200', '800'])
  const session = await cdp(await browser(['get', 'cdp-url']), url)
  cleanups.push(() => session.close())
  // DPR=1.25 (say) runs it at that devicePixelRatio, as a scaled display or a browser zoom does.
  if (process.env.DPR) await session.send('Emulation.setDeviceMetricsOverride', { width: 1200, height: 800, deviceScaleFactor: Number(process.env.DPR), mobile: false })
  const metrics = async () => Object.fromEntries((await session.send<{ metrics: { name: string, value: number }[] }>('Performance.getMetrics')).metrics.map((metric) => [metric.name, metric.value])) as Metrics
  const renders = async () => await evaluate(`window.__surface()?.renders?.() ?? null`) as number | null
  const pause = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms))
  // measure answers the busy seconds (and renders) over windowMs while during runs.
  // PROFILE=kind:state (say ghostty:(c)) prints that window's top self times from a CPU profile.
  const profile = process.env.PROFILE
  const measure = async (during: () => Promise<void> = () => pause(windowMs), label = ''): Promise<Sample> => {
    const profiled = profile !== undefined && profile === label
    if (profiled) { await session.send('Profiler.enable'); await session.send('Profiler.start') }
    const [before, rendersBefore] = [await metrics(), await renders()]
    await during()
    const [after, rendersAfter] = [await metrics(), await renders()]
    if (profiled) printProfile((await session.send<{ profile: CPUProfile }>('Profiler.stop')).profile)
    return {
      task: after.TaskDuration - before.TaskDuration,
      script: after.ScriptDuration - before.ScriptDuration,
      renders: rendersBefore === null || rendersAfter === null ? null : rendersAfter - rendersBefore,
    }
  }
  const viewOpen = `Boolean(document.querySelector('.herdr-view:not([hidden])'))`
  const results: Record<string, Record<string, Sample>> = {}

  for (const kind of kinds) {
    await evaluate('localStorage.clear(), sessionStorage.clear(), true')
    await browser(['reload'])
    await waitFor(`document.querySelectorAll('.lens-card').length === 1`)
    await evaluate(herdrSurfaceProbe)
    if (kind === 'xterm') await evaluate(`localStorage.setItem('herder.web.herdrTerminal.v1', 'xterm'), true`)
    const row: Record<string, Sample> = {}
    results[kind] = row
    await pause(1_000)
    row['never opened'] = await measure(undefined, `${kind}:never`)

    await browser(['press', 'Control+`'])
    await waitFor(`${viewOpen} && document.querySelector('.herdr-view-state')?.textContent === 'live' && window.__surface()?.kind === '${kind}'`)
    await waitFor(`window.__surface().screen().rows.join('\\n').includes('hello from herdr')`)
    await pause(1_000)
    row['(a) open, idle'] = await measure(undefined, `${kind}:(a)`)

    // Ten keys a second, as real key events; the client echoes each.
    row['(c) typing 10/s'] = await measure(async () => {
      for (let key = 0; key < windowMs / 100; key++) {
        const letter = String.fromCharCode(97 + (key % 26))
        await session.send('Input.dispatchKeyEvent', { type: 'keyDown', key: letter, code: `Key${letter.toUpperCase()}`, text: letter, windowsVirtualKeyCode: letter.toUpperCase().charCodeAt(0) })
        await session.send('Input.dispatchKeyEvent', { type: 'keyUp', key: letter, code: `Key${letter.toUpperCase()}`, windowsVirtualKeyCode: letter.toUpperCase().charCodeAt(0) })
        await pause(100)
      }
    }, `${kind}:(c)`)
    await pause(1_000)

    // 5 MiB of mixed ANSI, written as the socket delivers it (32 KiB at a time, a task each).
    row['(d) 5 MiB burst'] = await measure(async () => {
      await evaluate(`(async () => {
        const { burst } = await import('/test/bench/burstPage.ts')
        const data = burst(5 * 1024 * 1024)
        const surface = window.__surface()
        for (let at = 0; at < data.length; at += 32768) {
          surface.write(data.subarray(at, at + 32768))
          await new Promise((resolve) => setTimeout(resolve, 0))
        }
        return true
      })()`)
      await pause(windowMs)
    }, `${kind}:(d)`)

    await browser(['press', 'Control+`'])
    await waitFor(`!${viewOpen}`)
    await pause(1_000)
    row['(b) hidden'] = await measure(undefined, `${kind}:(b)`)
  }

  console.log(`main-thread busy over ${windowMs / 1000} s, headless Chrome, PTY fixture (TaskDuration ms / ScriptDuration ms, renders)`)
  const states = Object.keys(results[kinds[0]])
  for (const state of states) {
    const cells = kinds.map((kind) => {
      const sample = results[kind][state]
      return `${kind} ${(sample.task * 1000).toFixed(0).padStart(5)} / ${(sample.script * 1000).toFixed(0).padStart(5)}${sample.renders === null ? '' : ` (${sample.renders} renders)`}`
    })
    console.log(`${state.padEnd(16)} ${cells.join('   ')}`)
  }
} finally {
  for (const cleanup of cleanups.reverse()) await cleanup()
}
