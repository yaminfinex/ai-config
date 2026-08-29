import { useCallback, useEffect, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import { WebglAddon } from '@xterm/addon-webgl'
import '@xterm/xterm/css/xterm.css'
import { getPaneHistory, queryKeys, sendPaneInput } from '../../api/client'
import { Banner } from '../../shared/presentation'
import { screenNotice } from '../../shared/loadingPresentation'
import { ScrollJumpButtons } from '../../shared/useFollowScroll'
import type { Pane, ScreenFrame } from '../../types'
import { screenPanePresentation } from './screenPresentation'
import { SnapshotPainter } from './terminalPainter'
import { terminalTheme } from './terminalTheme'
import { PaneInputQueue } from './paneInput'

type TerminalSurfaceProps = {
  frame?: ScreenFrame
  text: string
  live: boolean
  active: boolean
  onFocus?: () => void
  onBlur?: () => void
  onData?: (data: string) => void
}

function XtermSurface({ frame, text, live, active, onFocus, onBlur, onData }: TerminalSurfaceProps) {
  const hostRef = useRef<HTMLDivElement>(null)
  const terminalRef = useRef<Terminal | null>(null)
  const painterRef = useRef<SnapshotPainter | null>(null)
  const latestFrame = useRef(frame)
  latestFrame.current = frame

  useEffect(() => {
    const host = hostRef.current
    if (!host) return
    const terminal = new Terminal({
      allowTransparency: false,
      convertEol: true,
      cursorBlink: false,
      cursorInactiveStyle: 'none',
      disableStdin: !onData,
      fontFamily: '"SFMono-Regular", Consolas, "Liberation Mono", monospace',
      fontSize: 12,
      lineHeight: 1.15,
      scrollback: live ? 0 : 2000,
      theme: terminalTheme,
    })
    const fitAddon = new FitAddon()
    terminal.loadAddon(fitAddon)
    terminal.open(host)
    terminalRef.current = terminal
    painterRef.current = new SnapshotPainter((data, done) => terminal.write(data, done))

    let webgl: WebglAddon | null = null
    try {
      webgl = new WebglAddon()
      webgl.onContextLoss(() => {
        webgl?.dispose()
        webgl = null
      })
      terminal.loadAddon(webgl)
    } catch {
      webgl?.dispose()
      webgl = null
    }

    const measureAndResize = () => {
      const current = latestFrame.current
      if (!current?.cols || !current.rows) return
      for (let fontSize = 14; fontSize >= 7; fontSize -= 1) {
        terminal.options.fontSize = fontSize
        const proposal = fitAddon.proposeDimensions()
        if (proposal && proposal.cols >= current.cols && proposal.rows >= current.rows) break
      }
      terminal.resize(current.cols, current.rows)
    }
    const observer = new ResizeObserver(measureAndResize)
    observer.observe(host)
    measureAndResize()
    const input = onData ? terminal.onData(onData) : null

    return () => {
      observer.disconnect()
      input?.dispose()
      painterRef.current = null
      terminalRef.current = null
      webgl?.dispose()
      terminal.dispose()
    }
  }, [live, onData])

  useEffect(() => {
    const terminal = terminalRef.current
    if (terminal && frame?.cols && frame.rows && (terminal.cols !== frame.cols || terminal.rows !== frame.rows)) {
      terminal.resize(frame.cols, frame.rows)
    }
  }, [frame])

  useEffect(() => {
    painterRef.current?.paint(text)
  }, [text])

  useEffect(() => {
    if (!active) terminalRef.current?.blur()
  }, [active])

  return <div className="terminal-host" ref={hostRef} tabIndex={0} onFocus={onFocus} onBlur={onBlur} />
}

export function ScreenViewport({ paneID, active = true, onFocus, onBlur }: { paneID: string, active?: boolean, onFocus?: () => void, onBlur?: () => void }) {
  const [inputProblem, setInputProblem] = useState('')
  const onBlurRef = useRef(onBlur)
  onBlurRef.current = onBlur
  useEffect(() => () => onBlurRef.current?.(), [])
  const inputQueue = useRef<{ paneID: string, queue: PaneInputQueue } | undefined>(undefined)
  if (inputQueue.current?.paneID !== paneID) {
    inputQueue.current = { paneID, queue: new PaneInputQueue(async (input) => { await sendPaneInput(paneID, input) }) }
  }
  const sendData = useCallback((data: string) => {
    void inputQueue.current?.queue.send(data).then(() => setInputProblem(''), (error: unknown) => setInputProblem(error instanceof Error ? error.message : String(error)))
  }, [])
  const frame = useQuery<ScreenFrame>({
    queryKey: queryKeys.screen(paneID),
    queryFn: async () => new Promise<ScreenFrame>(() => undefined),
    enabled: false,
  }).data
  const notice = screenNotice(frame)
  return <section className="screen-viewport" aria-busy={!frame}>
    <div className="screen-notice">{notice && <Banner source="screen" detail={notice.detail} tone={notice.tone} />}</div>
    {inputProblem && <div className="screen-input-notice"><Banner source="terminal input" detail={inputProblem} /></div>}
    {frame?.cols && frame.rows ? <div className="terminal-size">{frame.cols}×{frame.rows} — pane's real size</div> : null}
    <XtermSurface frame={frame} text={frame?.status === 'available' ? frame.text : ''} live active={active} onFocus={onFocus} onBlur={onBlur} onData={sendData} />
    <ScrollJumpButtons bottomVisible={false} onBottom={() => undefined} />
  </section>
}

export function ScreenPanel({ pane, active = true, onFocus, onBlur }: { pane: Pane, active?: boolean, onFocus?: (paneID: string) => void, onBlur?: (paneID: string) => void }) {
  const presentation = screenPanePresentation(pane)
  const [historyMode, setHistoryMode] = useState(false)
  const history = useQuery({
    queryKey: queryKeys.paneHistory(pane.pane_id),
    queryFn: () => getPaneHistory(pane.pane_id),
    enabled: historyMode,
    staleTime: Infinity,
  })
  const focus = useCallback(() => onFocus?.(pane.pane_id), [onFocus, pane.pane_id])
  const blur = useCallback(() => onBlur?.(pane.pane_id), [onBlur, pane.pane_id])
  return <main className="screen-page">
    <header className="screen-header">
      <strong>{presentation.label}</strong><span className="pane-chip">{pane.pane_id}</span>
      <span>{historyMode ? 'history snapshot' : 'live — keystrokes go to the real pane'}</span>
      <div className="screen-actions">
        {historyMode ? <><button type="button" onClick={() => void history.refetch()}>Refresh history</button><button type="button" onClick={() => setHistoryMode(false)}>Live</button></>
          : <button type="button" onClick={() => setHistoryMode(true)}>History</button>}
      </div>
    </header>
    {presentation.warning && <Banner source="identity" detail={presentation.warning} tone="info" />}
    {historyMode ? <section className="screen-history" aria-busy={history.isPending}>
      <div className="history-meta">{history.data ? `Fetched ${history.data.fetched_at}${history.data.truncated ? ' · bounded to recent history' : ''}` : 'Fetching bounded pane history…'}</div>
      {history.error && <Banner source="history" detail={history.error.message} />}
      <XtermSurface text={history.data?.text ?? ''} live={false} active={active} />
    </section> : <ScreenViewport paneID={pane.pane_id} active={active} onFocus={focus} onBlur={blur} />}
  </main>
}
