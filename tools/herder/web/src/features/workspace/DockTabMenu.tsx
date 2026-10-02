import { useCallback, useEffect, useState, type MouseEvent, type RefObject } from 'react'
import { createPortal } from 'react-dom'
import type { DockPanelParams } from '../layout/dockLayout.ts'
import { usePositionedMenu } from '../../shared/usePositionedMenu.tsx'
import { findAgentRow } from '../../shared/agentStatus.ts'
import { agentUnread, useReadMarkers } from '../spaces/index.ts'
import { dockTabMenuItems, isDockTabMenuKey, readMenuItem } from './dockTabMenuModel.ts'
import { useAgentUnread, useWorkspaceActionsContext, useWorkspaceData } from './workspaceContext.tsx'

export function useDockTabMenu(tabRef: RefObject<HTMLDivElement | null>, sourceID: string, params: DockPanelParams) {
  const actions = useWorkspaceActionsContext()
  const data = useWorkspaceData()
  const { position, menuRef, open, close } = usePositionedMenu()
  const subject = params.kind === 'agent' ? params.name : undefined
  const unread = useAgentUnread(subject ?? '')

  useEffect(() => {
    const tab = tabRef.current?.closest<HTMLElement>('.dv-tab')
    if (!tab) return
    const onKeyDown = (event: KeyboardEvent) => {
      if (!isDockTabMenuKey(event)) return
      event.preventDefault()
      event.stopPropagation()
      const rect = tab.getBoundingClientRect()
      open({ x: rect.left, y: rect.bottom }, tab)
    }
    tab.addEventListener('keydown', onKeyDown)
    return () => tab.removeEventListener('keydown', onKeyDown)
  }, [open, tabRef])

  const onContextMenu = useCallback((event: MouseEvent<HTMLDivElement>) => {
    event.preventDefault()
    event.stopPropagation()
    open({ x: event.clientX, y: event.clientY }, tabRef.current?.closest<HTMLElement>('.dv-tab') ?? null)
  }, [open, tabRef])

  const menu = position ? createPortal(<div ref={menuRef} className="dock-tab-menu" role="menu"
    aria-label="Pane actions" style={{ left: position.x, top: position.y }}>
    {dockTabMenuItems(data.spaces, data.activeSpaceID, subject, unread).map((item) => <button type="button" role="menuitem" key={`${item.kind}:${item.id}`}
      onClick={() => {
        if (item.kind === 'unread' || item.kind === 'read') {
          close()
          if (item.kind === 'read') actions.markRead([item.subject])
          else actions.markUnread(item.subject)
          return
        }
        if (item.kind === 'reassign') {
          close(false)
          actions.showQuickOpen(undefined, { kind: 'reassign', subject: item.subject })
          return
        }
        const sent = item.kind === 'space' ? actions.sendPanelToSpace(sourceID, params, item.id) : actions.sendPanelToNewSpace(sourceID, params)
        close(!sent)
      }}>{item.label}</button>)}
  </div>, document.body) : null

  return { onContextMenu, menu }
}

export function useAgentRowMenu() {
  const actions = useWorkspaceActionsContext()
  const data = useWorkspaceData()
  const markers = useReadMarkers()
  const [subject, setSubject] = useState('')
  const positioned = usePositionedMenu()
  const open = useCallback((event: MouseEvent<HTMLElement>, nextSubject: string) => {
    event.preventDefault()
    event.stopPropagation()
    setSubject(nextSubject)
    positioned.open({ x: event.clientX, y: event.clientY }, event.currentTarget)
  }, [positioned.open])

  const readItem = readMenuItem(subject, agentUnread(findAgentRow(data.board, subject), markers[subject]))
  const menu = positioned.position ? createPortal(<div ref={positioned.menuRef} className="dock-tab-menu" role="menu" aria-label={`Actions for ${subject}`}
    style={{ left: positioned.position.x, top: positioned.position.y }}>
    <button type="button" role="menuitem" onClick={() => {
      positioned.close(false)
      actions.showQuickOpen(undefined, { kind: 'reassign', subject })
    }}>Reassign…</button>
    <button type="button" role="menuitem" onClick={() => {
      positioned.close()
      if (readItem.kind === 'read') actions.markRead([subject])
      else actions.markUnread(subject)
    }}>{readItem.label}</button>
  </div>, document.body) : null
  return { open, menu }
}
