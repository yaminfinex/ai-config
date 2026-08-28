import { useQuery } from '@tanstack/react-query'
import { queryKeys } from '../../api/client'
import { Banner } from '../../shared/presentation'
import { screenNotice } from '../../shared/loadingPresentation'
import type { Pane, ScreenFrame } from '../../types'
import { screenPanePresentation } from './screenPresentation'

export function ScreenViewport({ paneID }: { paneID: string }) {
  const frame = useQuery<ScreenFrame>({
    queryKey: queryKeys.screen(paneID),
    queryFn: async () => new Promise<ScreenFrame>(() => undefined),
    enabled: false,
  }).data
  const notice = screenNotice(frame)
  return <section className="screen-viewport" aria-busy={!frame}>
    <div className="screen-notice">{notice && <Banner source="screen" detail={notice.detail} tone={notice.tone} />}</div>
    <pre className={`terminal-screen${frame?.status === 'unavailable' ? ' unavailable' : ''}`} aria-label="Live terminal screen">{frame?.status === 'available' ? frame.text : ''}</pre>
  </section>
}

export function ScreenPanel({ pane }: { pane: Pane }) {
  const presentation = screenPanePresentation(pane)
  return <main className="screen-page">
    <header className="screen-header"><strong>{presentation.label}</strong><span className="pane-chip">{pane.pane_id}</span><span>read-only · ANSI-stripped</span></header>
    {presentation.warning && <Banner source="identity" detail={presentation.warning} tone="info" />}
    <ScreenViewport paneID={pane.pane_id} />
  </main>
}
