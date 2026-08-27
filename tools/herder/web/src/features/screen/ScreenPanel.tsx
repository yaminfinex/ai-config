import { useEffect } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { queryKeys } from '../../api/client'
import { Banner } from '../../shared/presentation'
import { screenNotice } from '../../shared/loadingPresentation'
import type { Pane, ScreenFrame } from '../../types'

export function ScreenPanel({ pane }: { pane: Pane }) {
  const queryClient = useQueryClient()
  const frame = useQuery<ScreenFrame>({
    queryKey: queryKeys.screen(pane.pane_id),
    queryFn: async () => new Promise<ScreenFrame>(() => undefined),
    enabled: false,
  }).data
  useEffect(() => {
    return () => queryClient.removeQueries({ queryKey: queryKeys.screen(pane.pane_id), exact: true })
  }, [pane.pane_id, queryClient])
  const notice = screenNotice(frame)
  return <main className="screen-page" aria-busy={!frame}>
    <header className="screen-header"><strong>{pane.label || 'Terminal pane'}</strong><span className="pane-chip">{pane.pane_id}</span><span>read-only · ANSI-stripped</span></header>
    <div className="screen-notice">{notice && <Banner source="screen" detail={notice.detail} tone={notice.tone} />}</div>
    <pre className={`terminal-screen${frame?.status === 'unavailable' ? ' unavailable' : ''}`} aria-label="Live terminal screen">{frame?.status === 'available' ? frame.text : ''}</pre>
  </main>
}
