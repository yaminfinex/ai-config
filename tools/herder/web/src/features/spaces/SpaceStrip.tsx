import { useEffect, useRef, useState } from 'react'
import type { SpaceDefinition, SpaceResult } from './spacesModel.ts'
import type { SpacesStatus } from './spacesStore.ts'

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
  close: (id: string) => SpaceResult<unknown>
  reopen: (id: string) => SpaceResult<unknown>
}

export function SpaceStrip(props: Props) {
  const [editing, setEditing] = useState<string | null>(null)
  const [name, setName] = useState('')
  const input = useRef<HTMLInputElement | null>(null)
  const cancelRename = useRef(false)

  useEffect(() => { if (editing) input.current?.select() }, [editing])

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

  return <div className="space-strip" role="group" aria-label={`${props.items.length} spaces`}>
    <div className="space-chips">
      {props.items.map((space) => <div className={`space-chip${space.id === props.activeID ? ' active' : ''}`} key={space.id}>
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
            onClick={() => props.switch(space.id)} onDoubleClick={() => beginRename(space)}>{space.name}</button>}
        {space.id === props.activeID && editing !== space.id && <button type="button" className="space-close"
          aria-label={`Close space ${space.name}`} title={`Close ${space.name}`} onClick={() => props.close(space.id)}>×</button>}
      </div>)}
      <button type="button" className="space-create" aria-label="Create space" title="Create space" onClick={() => props.create()}>+</button>
      {props.recent.map((space) => <button type="button" className="space-reopen" key={space.id}
        title={`Reopen ${space.name}`} onClick={() => props.reopen(space.id)}>reopen {space.name}</button>)}
    </div>
  </div>
}
