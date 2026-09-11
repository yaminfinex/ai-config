export type InteractiveTreeItem = {
  getId: () => string
  setFocused: () => void
  primaryAction: () => void
  isExpanded: () => boolean
  expand: () => void
  collapse: () => void
}

export function primaryTreeRowClick(item: InteractiveTreeItem, select: (items: string[]) => void) {
  item.setFocused()
  select([item.getId()])
  item.primaryAction()
}

export function toggleTreeRow(item: Pick<InteractiveTreeItem, 'isExpanded' | 'expand' | 'collapse'>) {
  if (item.isExpanded()) item.collapse()
  else item.expand()
}
