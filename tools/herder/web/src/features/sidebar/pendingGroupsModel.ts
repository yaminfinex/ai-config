import type { SidebarNode } from './sidebarNodes.ts'

// Pending groups: a group created on the web exists only as a local
// placeholder header (shell preferences) until the first drop into it writes
// the real assign event and a fleet frame shows the label with members.
// The name rules mirror the server's Validate (agentstore/event.go): trimmed,
// non-empty, at most 80 runes, no control characters.
export const maxGroupNameLength = 80

export type GroupNameCheck = { ok: true, name: string } | { ok: false, reason: string }

export function validateGroupName(raw: string, existing: Iterable<string>): GroupNameCheck {
  const name = raw.trim()
  if (!name) return { ok: false, reason: 'group name is empty' }
  // eslint-disable-next-line no-control-regex
  if (/[\x00-\x1f\x7f]/.test(name)) return { ok: false, reason: 'group name must not contain control characters' }
  if ([...name].length > maxGroupNameLength) return { ok: false, reason: `group name must not exceed ${maxGroupNameLength} characters` }
  for (const label of existing) if (label === name) return { ok: false, reason: `group ${name} already exists` }
  return { ok: true, name }
}

export function addPendingGroup(pending: readonly string[], name: string) {
  return pending.includes(name) ? [...pending] : [...pending, name]
}

export function removePendingGroup(pending: readonly string[], name: string) {
  return pending.filter((label) => label !== name)
}

// realGroupLabels are the header labels a frame actually shows with members
// (placeholders excluded): what a new name must not collide with, and what
// settles a placeholder.
export function realGroupLabels(nodes: Map<string, SidebarNode>) {
  return [...nodes.values()].filter((node) => node.kind === 'group' && !node.placeholder).map((node) => node.name)
}

// settledPendingGroups lists the pending labels this frame shows as real
// headers: the placeholder did its job and leaves preferences.
export function settledPendingGroups(pending: readonly string[], nodes: Map<string, SidebarNode>) {
  const real = new Set(realGroupLabels(nodes))
  return pending.filter((label) => real.has(label))
}
