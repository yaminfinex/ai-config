import type { CSSProperties, HTMLAttributes, ReactNode } from 'react'
import { PanelState } from './PanelState'
import { treeIndent } from './treeModel'

export function TreeRow({ depth, name, expandable = false, expanded = false, selected = false, focused = false, className = '', icon, label, trailing, title, onToggle, itemProps }: {
  depth: number
  name: string
  expandable?: boolean
  expanded?: boolean
  selected?: boolean
  focused?: boolean
  className?: string
  icon: ReactNode
  label: ReactNode
  trailing?: ReactNode
  title?: string
  onToggle?: () => void
  itemProps?: HTMLAttributes<HTMLDivElement>
}) {
  const style: CSSProperties = { ...itemProps?.style, paddingLeft: treeIndent(depth) }
  const classes = `panel-tree-row${focused ? ' tree-focused' : ''}${selected ? ' selected' : ''}${className ? ` ${className}` : ''}`
  return <div {...itemProps} className={classes} style={style} title={title}
    aria-expanded={expandable ? expanded : itemProps?.['aria-expanded']}
    aria-selected={selected ? true : itemProps?.['aria-selected']}>
    {expandable ? <button
      className={`tree-disclosure${expanded ? ' expanded' : ''}`}
      type="button"
      aria-label={`${expanded ? 'Collapse' : 'Expand'} ${name}`}
      title={`${expanded ? 'Collapse' : 'Expand'} ${name}`}
      onClick={(event) => { event.stopPropagation(); onToggle?.() }}
    ><span aria-hidden="true">›</span></button> : <span className="tree-disclosure-spacer" />}
    <span className="tree-icon" aria-hidden="true">{icon}</span>
    {label}
    {trailing}
  </div>
}

export function TreeState({ depth, title, detail, role = 'status' }: {
  depth: number
  title: ReactNode
  detail?: ReactNode
  role?: 'alert' | 'status'
}) {
  return <div className="tree-state-shell" role="none" style={{ paddingLeft: treeIndent(depth) }}>
    <PanelState as="div" className="tree-state" role={role} title={title} detail={detail} />
  </div>
}
