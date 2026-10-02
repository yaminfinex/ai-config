import { useEffect, useRef, useState, type SyntheticEvent } from 'react'
import { createPortal } from 'react-dom'
import { usePositionedMenu } from '../../shared/usePositionedMenu.tsx'
import type { SpaceDefinition, SpaceResult } from './spacesModel.ts'
import type { SpacesStatus } from './spacesStore.ts'
import { attentionLabel, quietAttention, spaceMenuItems, totalAttention, type SpaceAttention } from './spaceAttentionModel.ts'

type Props = {
  enabled: boolean
  items: SpaceDefinition[]
  recent: SpaceDefinition[]
  activeID: string | null
  status: SpacesStatus
  problem: string
  attention: Record<string, SpaceAttention>
  collapsed: boolean
  onCollapsed: (collapsed: boolean) => void
  switch: (id: string) => boolean
  create: () => SpaceResult<SpaceDefinition>
  rename: (id: string, name: string) => SpaceResult<SpaceDefinition>
  reorder: (id: string, targetIndex: number) => SpaceResult<SpaceDefinition>
  close: (id: string) => SpaceResult<unknown>
  reopen: (id: string) => SpaceResult<unknown>
  markAllRead: (id: string) => void
  announcement: string
}

// AttentionMarks shows a space's waiting (quiet) and blocked (loud) agent
// counts; the text alternative lives on the owning row.
export function AttentionMarks({ attention }: { attention: SpaceAttention }) {
  return <span className="space-marks" aria-hidden="true">
    {attention.blocked.length > 0 && <span className="space-mark blocked">{attention.blocked.length}</span>}
    {attention.unread.length > 0 && <span className="space-mark unread">{attention.unread.length}</span>}
  </span>
}

export function SpacesSection(props: Props) {
  const [editing, setEditing] = useState<string | null>(null)
  const [name, setName] = useState('')
  const input = useRef<HTMLInputElement | null>(null)
  const cancelRename = useRef(false)
  const historyMenu = useRef<HTMLDivElement | null>(null)
  const [historyOpen, setHistoryOpen] = useState(false)
  const [dragging, setDragging] = useState<string | null>(null)
  const draggingID = useRef<string | null>(null)
  const [dropTarget, setDropTarget] = useState<{ id: string, after: boolean } | null>(null)
  const spaceMenu = usePositionedMenu()
  const [menuSpace, setMenuSpace] = useState('')

  useEffect(() => { if (editing) input.current?.select() }, [editing])
  useEffect(() => {
    if (!historyOpen) return
    const dismiss = (event: PointerEvent) => { if (!historyMenu.current?.contains(event.target as Node)) setHistoryOpen(false) }
    const escape = (event: KeyboardEvent) => { if (event.key === 'Escape') setHistoryOpen(false) }
    document.addEventListener('pointerdown', dismiss, true)
    document.addEventListener('keydown', escape, true)
    return () => { document.removeEventListener('pointerdown', dismiss, true); document.removeEventListener('keydown', escape, true) }
  }, [historyOpen])

  if (!props.enabled) return <section className="spaces-section degraded" aria-label="Spaces">
    <div className="spaces-degraded" role="status" title={props.problem || props.status.problem}>spaces unavailable · layout still saved</div>
  </section>

  const beginRename = (space: SpaceDefinition) => {
    if (space.id !== props.activeID) return
    cancelRename.current = false
    setEditing(space.id)
    setName(space.name)
  }
  const finishRename = () => {
    if (!editing) return
    if (cancelRename.current) {
      cancelRename.current = false
      setEditing(null)
      return
    }
    const result = props.rename(editing, name)
    if (result.ok) setEditing(null)
  }
  const attentionOf = (id: string) => props.attention[id] ?? quietAttention
  // A space's menu opens only while it has something to offer.
  const openSpaceMenu = (space: SpaceDefinition, at: { x: number, y: number }, source: HTMLElement, event: SyntheticEvent) => {
    if (spaceMenuItems(attentionOf(space.id)).length === 0) return
    event.preventDefault()
    setMenuSpace(space.id)
    spaceMenu.open(at, source)
  }
  const menuItems = spaceMenuItems(attentionOf(menuSpace))
  const total = totalAttention(props.items.map((space) => attentionOf(space.id)))

  return <section className={`spaces-section${props.collapsed ? ' collapsed' : ''}`} aria-label="Spaces">
    <header className="spaces-heading">
      <button type="button" className="spaces-collapse" aria-expanded={!props.collapsed} aria-controls="spaces-list"
        onClick={() => props.onCollapsed(!props.collapsed)}>
        <span className="spaces-caret" aria-hidden="true">{props.collapsed ? '▸' : '▾'}</span>
        <span>Spaces</span>
        {props.collapsed && <><AttentionMarks attention={total} /><span className="visually-hidden">, {attentionLabel(total)}</span></>}
      </button>
      <button type="button" className="space-create" aria-label="Create space" title="Create space" onClick={() => props.create()}>+</button>
      {props.recent.length > 0 && <div ref={historyMenu} className="space-history-wrap">
        <button type="button" className="space-history" aria-label="Recently closed spaces" title="Recently closed spaces"
          aria-haspopup="menu" aria-expanded={historyOpen} onClick={() => setHistoryOpen((open) => !open)}>↺</button>
        {historyOpen && <div className="space-history-menu" role="menu" aria-label="Recently closed spaces">
          {props.recent.slice(0, 8).map((space) => <button type="button" role="menuitem" key={space.id} title={`Reopen ${space.name}`}
            onClick={() => { props.reopen(space.id); setHistoryOpen(false) }}>{space.name}</button>)}
        </div>}
      </div>}
    </header>
    {!props.collapsed && <ul id="spaces-list" className="spaces-list" aria-label={`${props.items.length} spaces`}>
      {props.items.map((space) => {
        const attention = attentionOf(space.id)
        const active = space.id === props.activeID
        return <li
          className={`space-row${active ? ' active' : ''}${dragging === space.id ? ' dragging' : ''}${dropTarget?.id === space.id ? dropTarget.after ? ' drop-after' : ' drop-before' : ''}`}
          data-space-id={space.id} draggable={editing !== space.id} key={space.id}
          onDragStart={(event) => {
            if ((event.target as HTMLElement).closest('.space-close')) { event.preventDefault(); return }
            event.dataTransfer.effectAllowed = 'move'
            event.dataTransfer.setData('text/plain', space.id)
            draggingID.current = space.id
            setDragging(space.id)
          }}
          onDragOver={(event) => {
            event.preventDefault()
            if (!draggingID.current || draggingID.current === space.id) return
            const rect = event.currentTarget.getBoundingClientRect()
            setDropTarget({ id: space.id, after: event.clientY >= rect.top + rect.height / 2 })
          }}
          onDrop={(event) => {
            event.preventDefault()
            const sourceID = event.dataTransfer.getData('text/plain') || draggingID.current
            if (!sourceID || sourceID === space.id) return
            const sourceIndex = props.items.findIndex((item) => item.id === sourceID)
            const targetIndex = props.items.findIndex((item) => item.id === space.id)
            const rect = event.currentTarget.getBoundingClientRect()
            const after = event.clientY >= rect.top + rect.height / 2
            const destination = targetIndex - (sourceIndex < targetIndex ? 1 : 0) + (after ? 1 : 0)
            props.reorder(sourceID, destination)
            draggingID.current = null
            setDragging(null)
            setDropTarget(null)
          }}
          onDragEnd={() => { draggingID.current = null; setDragging(null); setDropTarget(null) }}>
          {editing === space.id
            ? <input ref={input} className="space-rename" aria-label={`Rename ${space.name}`} value={name} maxLength={80}
              onChange={(event) => setName(event.target.value)} onBlur={finishRename}
              onKeyDown={(event) => {
                if (event.key === 'Enter') event.currentTarget.blur()
                if (event.key === 'Escape') {
                  cancelRename.current = true
                  event.currentTarget.blur()
                }
              }} />
            : <button type="button" className="space-name" aria-current={active ? 'true' : undefined}
              aria-label={`${space.name}, ${attentionLabel(attention)}`} title={space.name}
              onClick={() => props.switch(space.id)} onDoubleClick={() => beginRename(space)}
              onContextMenu={(event) => openSpaceMenu(space, { x: event.clientX, y: event.clientY }, event.currentTarget, event)} onKeyDown={(event) => {
                if (event.key === 'ContextMenu' || event.key === 'F10' && event.shiftKey) {
                  const rect = event.currentTarget.getBoundingClientRect()
                  openSpaceMenu(space, { x: rect.left, y: rect.bottom }, event.currentTarget, event)
                  return
                }
                if (event.key === 'Enter' || event.key === 'F2') {
                  if (space.id === props.activeID) beginRename(space)
                  if (event.key === 'F2') event.preventDefault()
                }
                if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
                  const row = event.currentTarget.closest('li')
                  const next = event.key === 'ArrowDown' ? row?.nextElementSibling : row?.previousElementSibling
                  next?.querySelector<HTMLElement>('.space-name')?.focus()
                  event.preventDefault()
                }
              }}><span className="space-label">{space.name}</span><AttentionMarks attention={attention} /></button>}
          {active && editing !== space.id && <button type="button" className="space-close"
            aria-label={`Close space ${space.name}`} title={`Close ${space.name}`} onClick={() => props.close(space.id)}>×</button>}
        </li>
      })}
    </ul>}
    {spaceMenu.position && menuItems.length > 0 && createPortal(<div ref={spaceMenu.menuRef} className="dock-tab-menu" role="menu" aria-label="Space actions"
      style={{ left: spaceMenu.position.x, top: spaceMenu.position.y }}>
      {menuItems.map((item) => <button type="button" role="menuitem" key={item.id} onClick={() => {
        spaceMenu.close()
        props.markAllRead(menuSpace)
      }}>{item.label}</button>)}
    </div>, document.body)}
    <span className="visually-hidden" role="status" aria-live="polite">{props.announcement}</span>
  </section>
}
