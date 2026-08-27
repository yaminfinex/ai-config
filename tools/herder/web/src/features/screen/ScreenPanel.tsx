import { useQuery } from '@tanstack/react-query'
import { queryKeys } from '../../api/client'
import { Banner } from '../../shared/presentation'
import type { Pane, ScreenFrame } from '../../types'

export function ScreenPanel({ pane }: { pane: Pane }) {
  const frame = useQuery<ScreenFrame>({
    queryKey: queryKeys.screen(pane.pane_id),
    queryFn: async () => new Promise<ScreenFrame>(() => undefined),
    enabled: false,
  }).data
  return <main className="screen-page">
    <header className="screen-header"><strong>{pane.label || 'Terminal pane'}</strong><span className="pane-chip">{pane.pane_id}</span><span>read-only · ANSI-stripped</span></header>
    {!frame && <Banner source="screen" detail="Connecting to live pane…" />}
    {frame?.status === 'unavailable' && <Banner source="screen" detail={frame.detail || 'Pane screen is unavailable'} />}
    {frame?.truncated && <Banner source="screen" detail="Screen exceeds the 16 KiB live-frame budget; this snapshot is truncated." />}
    <pre className={`terminal-screen${frame?.status === 'unavailable' ? ' unavailable' : ''}`} aria-label="Live terminal screen">{frame?.status === 'available' ? frame.text : ''}</pre>
  </main>
}
