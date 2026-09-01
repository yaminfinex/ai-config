export function visibleSpaceIDs(
  ids: string[],
  activeID: string | null,
  widths: Record<string, number>,
  available: number,
  fixedWidth: number,
  moreWidth: number,
) {
  const gap = 3
  const total = ids.reduce((sum, id) => sum + (widths[id] ?? 72), 0) + Math.max(0, ids.length - 1) * gap + fixedWidth
  if (total <= available) return { visible: ids, hidden: [] as string[] }
  const active = ids.includes(activeID ?? '') ? activeID as string : ids[0]
  if (!active) return { visible: [] as string[], hidden: [] as string[] }
  const budget = Math.max(0, available - fixedWidth - moreWidth)
  const chosen = new Set([active])
  let used = widths[active] ?? 72
  for (const id of ids) {
    if (chosen.has(id)) continue
    const width = widths[id] ?? 72
    if (used + gap + width > budget) continue
    chosen.add(id)
    used += gap + width
  }
  return { visible: ids.filter((id) => chosen.has(id)), hidden: ids.filter((id) => !chosen.has(id)) }
}
