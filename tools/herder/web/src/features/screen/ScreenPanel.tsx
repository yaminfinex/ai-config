import { useQuery } from '@tanstack/react-query'
import { queryKeys } from '../../api/client'
import { Banner } from '../../shared/presentation'
import { screenNotice } from '../../shared/loadingPresentation'
import { ScrollJumpButtons, useFollowScroll } from '../../shared/useFollowScroll'
import type { Pane, ScreenFrame } from '../../types'
import { screenPanePresentation } from './screenPresentation'

export function ScreenViewport({ paneID, active = true }: { paneID: string, active?: boolean }) {
  const frame = useQuery<ScreenFrame>({
    queryKey: queryKeys.screen(paneID),
    queryFn: async () => new Promise<ScreenFrame>(() => undefined),
    enabled: false,
  }).data
  const notice = screenNotice(frame)
  const screenFollow = useFollowScroll<HTMLPreElement>(frame, undefined, active)
  return <section className="screen-viewport" aria-busy={!frame}>
    <div className="screen-notice">{notice && <Banner source="screen" detail={notice.detail} tone={notice.tone} />}</div>
    <pre className={`terminal-screen${frame?.status === 'unavailable' ? ' unavailable' : ''}`} data-follow-scroll aria-label="Live terminal screen" ref={screenFollow.viewportRef} onScroll={screenFollow.onScroll}>{frame?.status === 'available' ? frame.text : ''}</pre>
    <ScrollJumpButtons bottomVisible={!screenFollow.following} onBottom={screenFollow.jumpToBottom} />
  </section>
}

export function ScreenPanel({ pane, active = true }: { pane: Pane, active?: boolean }) {
  const presentation = screenPanePresentation(pane)
  return <main className="screen-page">
    <header className="screen-header"><strong>{presentation.label}</strong><span className="pane-chip">{pane.pane_id}</span><span>read-only · ANSI-stripped</span></header>
    {presentation.warning && <Banner source="identity" detail={presentation.warning} tone="info" />}
    <ScreenViewport paneID={pane.pane_id} active={active} />
  </main>
}
