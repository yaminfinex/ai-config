import { useEffect, useRef, useState } from 'react'
import type { SpaceDefinition, SpaceResult } from './spacesModel.ts'
import type { SpacesStatus } from './spacesStore.ts'
import { visibleSpaceIDs } from './spaceOverflowModel.ts'

type Props = {
  enabled: boolean
  items: SpaceDefinition[]
  recent: SpaceDefinition[]
  activeID: string | null
  status: SpacesStatus
  problem: string
  switch: (id: string) => boolean
  create: () => SpaceResult<SpaceDefinition>
  rename: (id: string, name: string) => SpaceResult<SpaceDefinition>
  reorder: (id: string, targetIndex: number) => SpaceResult<SpaceDefinition>
  close: (id: string) => SpaceResult<unknown>
  reopen: (id: string) => SpaceResult<unknown>
  announcement: string
}

export function SpaceStrip(props: Props) {
  const [editing, setEditing] = useState<string | null>(null)
  const [name, setName] = useState('')
  const input = useRef<HTMLInputElement | null>(null)
  const cancelRename = useRef(false)
  const strip = useRef<HTMLDivElement | null>(null)
  const measurements = useRef<HTMLDivElement | null>(null)
  const overflowMenu = useRef<HTMLDivElement | null>(null)
  const [available, setAvailable] = useState(Number.POSITIVE_INFINITY)
  const [widths, setWidths] = useState<Record<string, number>>({})
  const [moreOpen, setMoreOpen] = useState(false)
  const [dragging, setDragging] = useState<string | null>(null)
  const draggingID = useRef<string | null>(null)
  const [dropTarget, setDropTarget] = useState<{ id: string, after: boolean } | null>(null)

  useEffect(() => { if (editing) input.current?.select() }, [editing])
  useEffect(() => {
    const update = () => {
      setAvailable(strip.current?.clientWidth ?? Number.POSITIVE_INFINITY)
      const next = Object.fromEntries([...measurements.current?.querySelectorAll<HTMLElement>('[data-space-measure]') ?? []]
        .map((element) => [element.dataset.spaceMeasure as string, Math.ceil(element.getBoundingClientRect().width)]))
      setWidths((current) => JSON.stringify(current) === JSON.stringify(next) ? current : next)
    }
    const frame = window.requestAnimationFrame(update)
    const observer = typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(update)
    if (strip.current) observer?.observe(strip.current)
    return () => { window.cancelAnimationFrame(frame); observer?.disconnect() }
  }, [props.activeID, props.items])
  useEffect(() => {
    if (!moreOpen) return
    const dismiss = (event: PointerEvent) => { if (!overflowMenu.current?.contains(event.target as Node)) setMoreOpen(false) }
    const escape = (event: KeyboardEvent) => { if (event.key === 'Escape') setMoreOpen(false) }
    document.addEventListener('pointerdown', dismiss, true)
    document.addEventListener('keydown', escape, true)
    return () => { document.removeEventListener('pointerdown', dismiss, true); document.removeEventListener('keydown', escape, true) }
  }, [moreOpen])

  if (!props.enabled) return <div className="space-strip degraded" role="status" title={props.problem || props.status.problem}>
    <span>spaces unavailable · layout still saved</span>
  </div>

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
  const overflow = visibleSpaceIDs(props.items.map((space) => space.id), props.activeID, widths, available, 27, 58)
  const visible = new Set(overflow.visible)

  return <div ref={strip} className="space-strip" role="group" aria-label={`${props.items.length} spaces`}>
    <div className="space-chips">
      {props.items.filter((space) => visible.has(space.id)).map((space) => <div
        className={`space-chip${space.id === props.activeID ? ' active' : ''}${dragging === space.id ? ' dragging' : ''}${dropTarget?.id === space.id ? dropTarget.after ? ' drop-after' : ' drop-before' : ''}`}
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
          setDropTarget({ id: space.id, after: event.clientX >= rect.left + rect.width / 2 })
        }}
        onDrop={(event) => {
          event.preventDefault()
          const sourceID = event.dataTransfer.getData('text/plain') || draggingID.current
          if (!sourceID || sourceID === space.id) return
          const sourceIndex = props.items.findIndex((item) => item.id === sourceID)
          const targetIndex = props.items.findIndex((item) => item.id === space.id)
          const rect = event.currentTarget.getBoundingClientRect()
          const after = event.clientX >= rect.left + rect.width / 2
          const destination = targetIndex - (sourceIndex < targetIndex ? 1 : 0) + (after ? 1 : 0)
          props.reorder(sourceID, destination)
          draggingID.current = null
          setDragging(null)
          setDropTarget(null)
        }}
        onDragEnd={() => { draggingID.current = null; setDragging(null); setDropTarget(null) }}>
        {editing === space.id
          ? <input ref={input} aria-label={`Rename ${space.name}`} value={name} maxLength={80}
            onChange={(event) => setName(event.target.value)} onBlur={finishRename}
            onKeyDown={(event) => {
              if (event.key === 'Enter') event.currentTarget.blur()
              if (event.key === 'Escape') {
                cancelRename.current = true
                event.currentTarget.blur()
              }
            }} />
          : <button type="button" className="space-name" aria-pressed={space.id === props.activeID}
            aria-keyshortcuts="Meta+ArrowLeft Meta+ArrowRight Control+ArrowLeft Control+ArrowRight"
            onClick={() => props.switch(space.id)} onDoubleClick={() => beginRename(space)} onKeyDown={(event) => {
              if (event.key === 'Enter' || event.key === 'F2') {
                if (space.id === props.activeID) beginRename(space)
                if (event.key === 'F2') event.preventDefault()
              }
            }}>{space.name}</button>}
        {space.id === props.activeID && editing !== space.id && <button type="button" className="space-close"
          aria-label={`Close space ${space.name}`} title={`Close ${space.name}`} onClick={() => props.close(space.id)}>×</button>}
      </div>)}
      {overflow.hidden.length > 0 && <div ref={overflowMenu} className="space-overflow">
        <button type="button" className="space-more" aria-haspopup="menu" aria-expanded={moreOpen} onClick={() => setMoreOpen((open) => !open)}>{overflow.hidden.length} more</button>
        {moreOpen && <div className="space-overflow-menu" role="menu" aria-label="More spaces">
          {props.items.filter((space) => overflow.hidden.includes(space.id)).map((space) => <button type="button" role="menuitem" key={space.id}
            onClick={() => { props.switch(space.id); setMoreOpen(false) }}>{space.name}</button>)}
        </div>}
      </div>}
      <button type="button" className="space-create" aria-label="Create space" title="Create space" onClick={() => props.create()}>+</button>
      {props.recent.map((space) => <button type="button" className="space-reopen" key={space.id}
        title={`Reopen ${space.name}`} onClick={() => props.reopen(space.id)}>reopen {space.name}</button>)}
    </div>
    <div ref={measurements} className="space-measure-rack" aria-hidden="true">
      {props.items.map((space) => <span className={`space-chip${space.id === props.activeID ? ' active' : ''}`} data-space-measure={space.id} key={space.id}>
        <span className="space-name">{space.name}</span>{space.id === props.activeID && <span className="space-close">×</span>}
      </span>)}
    </div>
    <span className="visually-hidden" role="status" aria-live="polite">{props.announcement}</span>
  </div>
}
