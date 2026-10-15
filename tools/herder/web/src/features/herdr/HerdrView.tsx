import { useCallback, useEffect, useRef, useState } from 'react'
import { isHerdrViewChord } from '../layout/shellShortcuts.ts'
import { useSizeObserver } from '../../shared/lifecycle.ts'
import { HerdrSocket } from './herdrSocket.ts'
import { connectionLabel, herdrTermURL, resizeDebounceMs, shouldFollow, type HerdrConnection } from './herdrViewModel.ts'
import { openTerminalSurface, preferredSurface, type TerminalSurface, type TerminalSurfaceFactory } from './terminalSurface.ts'
import { terminalTheme } from '../screen/terminalTheme.ts'
import './herdrView.css'

// HerdrView is herdr itself over the whole app: one herdr client for this
// browser, following the agent the web is on. It stays mounted once opened
// (hidden while away) so the screen and, for leaveGraceMs, the client are
// still there when the owner flips back.
export function HerdrView({ open, followPane, leaveLabel, onLeave, surface = preferredSurface }: {
  open: boolean
  followPane: string | null
  leaveLabel: string
  onLeave: () => void
  surface?: TerminalSurfaceFactory
}) {
  const hostRef = useRef<HTMLDivElement>(null)
  const terminal = useRef<TerminalSurface | null>(null)
  const socket = useRef<HerdrSocket | null>(null)
  const [connection, setConnection] = useState<HerdrConnection>('idle')
  const [retryIn, setRetryIn] = useState<number | undefined>(undefined)
  const [follow, setFollow] = useState(true)
  const [followProblem, setFollowProblem] = useState('')
  const [problem, setProblem] = useState('')
  // The pane herdr was last asked to show on this client; null after a reconnect.
  const sent = useRef<string | null>(null)

  // The terminal loads (ghostty-web's WASM the first time) before the client connects.
  const [loaded, setLoaded] = useState<{ fallback?: string } | null>(null)
  useEffect(() => {
    const host = hostRef.current
    if (!host) return
    let stop: (() => void) | undefined
    const unmounted = new AbortController()
    void openTerminalSurface(host, { ownKey: isHerdrViewChord }, surface, unmounted.signal).then((opened) => {
      if (!opened) return
      const { surface: term, fallback } = opened
      terminal.current = term
      const client = new HerdrSocket({
        url: (size) => herdrTermURL(window.location, size),
        onOutput: (bytes) => term.write(bytes),
        onNotice: (notice) => {
          if (notice.type === 'focus_failed') setFollowProblem(`could not show ${notice.pane_id}: ${notice.detail}`)
          else if (notice.type === 'focused') setFollowProblem('')
          else if (notice.type === 'error') setProblem(notice.detail)
        },
        onConnection: (next, retryInMs) => {
          // A new client repaints everything; drop the old screen first.
          if (next === 'connecting' || next === 'reconnecting') sent.current = null
          if (next === 'connecting') term.reset()
          setConnection(next)
          setRetryIn(retryInMs)
        },
      })
      socket.current = client
      const stopInput = term.onInput((data) => { client.input(data) })
      stop = () => {
        stopInput()
        client.dispose()
        term.dispose()
        socket.current = null
        terminal.current = null
      }
      setLoaded({ fallback })
    })
    return () => {
      unmounted.abort()
      stop?.()
      setLoaded(null)
    }
  }, [surface])

  // Open attaches (or keeps) the client and takes the keys; leaving starts the grace.
  const ready = loaded !== null
  useEffect(() => {
    if (!open || !ready) return
    // Size first, so a new client starts at the view's grid.
    const size = terminal.current?.fit()
    if (size) socket.current?.resize(size.cols, size.rows)
    socket.current?.attach()
    terminal.current?.focus()
    return () => socket.current?.detach()
  }, [open, ready])

  const resizeTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  const fit = useCallback(() => {
    clearTimeout(resizeTimer.current)
    resizeTimer.current = setTimeout(() => {
      const size = terminal.current?.fit()
      if (size) socket.current?.resize(size.cols, size.rows)
    }, resizeDebounceMs)
  }, [])
  useEffect(() => () => clearTimeout(resizeTimer.current), [])
  useSizeObserver(hostRef, fit, open)

  useEffect(() => {
    if (!shouldFollow({ open, follow, connection, sent: sent.current }, followPane)) return
    if (socket.current?.focus(followPane)) sent.current = followPane
  }, [connection, follow, followPane, open])

  const status = ready ? connectionLabel(connection, retryIn) : 'loading terminal…'
  return <section className="herdr-view" hidden={!open} aria-label="herdr" onMouseDown={(event) => {
    if (!(event.target as Element).closest('button, label')) terminal.current?.focus()
  }}>
    <header className="herdr-view-header">
      <strong>herdr</strong>
      <span className={`herdr-view-state herdr-${connection}`} role="status">{status}</span>
      {loaded?.fallback !== undefined && <span className="herdr-view-note" title={loaded.fallback}>ghostty did not load ({loaded.fallback}); this is xterm.js</span>}
      <label className="herdr-view-follow" title="Show the agent you are on in herdr">
        <input type="checkbox" checked={follow} onChange={(event) => { setFollow(event.target.checked); sent.current = null }} />follow
      </label>
      {followPane && follow && <span className="herdr-view-pane">{followPane}</span>}
      {followProblem && follow && <span className="herdr-view-problem" role="alert">{followProblem}</span>}
      {problem && <span className="herdr-view-problem" role="alert">{problem}</span>}
      {connection === 'ended' && <button type="button" onClick={() => { setProblem(''); socket.current?.reconnect() }}>Reconnect</button>}
      <span className="herdr-view-keys">every key goes to herdr · <kbd>{leaveLabel}</kbd> leaves</span>
      <button type="button" className="herdr-view-leave" aria-label={`Leave herdr (${leaveLabel})`} title={`Leave herdr (${leaveLabel})`} onClick={onLeave}>×</button>
    </header>
    <div className="herdr-view-terminal" ref={hostRef} style={{ background: terminalTheme.background }} />
  </section>
}
