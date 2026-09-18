import { useEffect, useRef } from 'react'
import { useQuery } from '@tanstack/react-query'
import { queryKeys } from '../../api/client'
import type { ScreenFrame } from '../../types'
import { LiveTailRegion, liveTailNotice, liveTailRowCount, screenTailRows } from './liveTail'

// Mounted only while liveTailShown holds (AgentPanel decides). Mounting
// registers the pane so the fleet stream subscribes to its screen frames;
// unmounting withdraws it. Read-only: no xterm, no input, frames come from the
// mirror the stream already delivers into the screen query.
export function TranscriptLiveTail({ paneID, status, onTailPane }: { paneID: string, status: string, onTailPane: (paneID?: string) => void }) {
  const onTailPaneRef = useRef(onTailPane)
  onTailPaneRef.current = onTailPane
  useEffect(() => {
    onTailPaneRef.current(paneID)
    return () => onTailPaneRef.current(undefined)
  }, [paneID])
  const frame = useQuery<ScreenFrame>({
    queryKey: queryKeys.screen(paneID),
    queryFn: async () => new Promise<ScreenFrame>(() => undefined),
    enabled: false,
  }).data
  return <LiveTailRegion rows={screenTailRows(frame, liveTailRowCount)} status={status} notice={liveTailNotice(frame)} />
}
